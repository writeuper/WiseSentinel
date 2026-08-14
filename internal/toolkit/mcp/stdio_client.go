package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

var defaultEnvironmentAllowlist = []string{
	"PATH", "HOME", "LANG", "LC_ALL", "TZ", "SSL_CERT_FILE", "SSL_CERT_DIR", "UV_CACHE_DIR",
}

// StartOptions restricts the executable and environment inherited by an MCP
// subprocess. MCP servers are untrusted integration boundaries and must not
// receive the platform's full credential-bearing environment.
type StartOptions struct {
	AllowedCommands      []string
	AllowedArgs          []string
	EnvironmentAllowlist []string
	CapabilityPolicy     *CapabilityPolicy
}

// CapabilityPolicy is the fail-closed contract for an MCP server. Empty
// fields are intentionally permissive for backwards-compatible local use;
// production profiles should pin the server identity and complete tool set.
type CapabilityPolicy struct {
	ExpectedServerName    string
	ExpectedServerVersion string
	ExpectedToolNames     []string
	ExpectedFingerprint   string
	MaxTools              int
}

// CapabilityManifest is a bounded, non-sensitive snapshot of the MCP
// handshake and tools/list result. It is suitable for audit and deployment
// comparison; it deliberately excludes descriptions, arguments and secrets.
type CapabilityManifest struct {
	ProtocolVersion   string
	ServerName        string
	ServerVersion     string
	ToolNames         []string
	ToolCount         int
	SchemaFingerprint string
}

const maxCapabilityTools = 512

// Client is the subset of MCP operations used by the platform adapters.
type Client interface {
	ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

// StdioClient owns one MCP server subprocess and its MCP protocol session.
type StdioClient struct {
	mu       sync.Mutex
	client   *client.Client
	manifest CapabilityManifest
}

// NewStdioClient starts command, initializes the MCP session, and verifies tools/list.
func NewStdioClient(ctx context.Context, command string, args ...string) (*StdioClient, error) {
	return NewStdioClientWithOptions(ctx, command, args, StartOptions{})
}

// NewStdioClientWithOptions starts an MCP process after validating its command
// against the configured allowlist and stripping unapproved environment keys.
func NewStdioClientWithOptions(ctx context.Context, command string, args []string, options StartOptions) (*StdioClient, error) {
	if err := validateCommand(command, options.AllowedCommands); err != nil {
		return nil, err
	}
	if err := validateArgs(args, options.AllowedArgs); err != nil {
		return nil, err
	}
	allowlist := options.EnvironmentAllowlist
	if len(allowlist) == 0 {
		allowlist = defaultEnvironmentAllowlist
	}
	environment := sanitizedEnvironment(os.Environ(), allowlist)
	c, err := client.NewStdioMCPClientWithOptions(command, nil, args,
		transport.WithCommandFunc(restrictedCommandFactory(environment)),
	)
	if err != nil {
		return nil, fmt.Errorf("start MCP stdio server: %w", err)
	}
	wrapped := &StdioClient{client: c}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = wrapped.Close()
		}
	}()
	initialized, err := c.Initialize(ctx, mcp.InitializeRequest{
		Request: mcp.Request{Method: "initialize"},
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "wisesentinel-platform", Version: "dev"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize MCP session: %w", err)
	}
	tools, err := listAllTools(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("list MCP tools: %w", err)
	}
	manifest, err := buildCapabilityManifest(initialized, tools)
	if err != nil {
		return nil, fmt.Errorf("build MCP capability manifest: %w", err)
	}
	if err := validateCapabilityManifest(manifest, options.CapabilityPolicy); err != nil {
		return nil, err
	}
	wrapped.manifest = manifest
	closeOnError = false
	return wrapped, nil
}

func listAllTools(ctx context.Context, c interface {
	ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
}) (*mcp.ListToolsResult, error) {
	all := &mcp.ListToolsResult{}
	var cursor mcp.Cursor
	seenCursors := map[mcp.Cursor]struct{}{}
	for {
		result, err := c.ListTools(ctx, mcp.ListToolsRequest{PaginatedRequest: mcp.PaginatedRequest{Params: mcp.PaginatedParams{Cursor: cursor}}})
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, fmt.Errorf("MCP tools/list response is empty")
		}
		all.Tools = append(all.Tools, result.Tools...)
		if len(all.Tools) > maxCapabilityTools {
			return nil, fmt.Errorf("MCP tool count exceeds hard capability limit %d", maxCapabilityTools)
		}
		if result.NextCursor == "" {
			return all, nil
		}
		if _, ok := seenCursors[result.NextCursor]; ok {
			return nil, fmt.Errorf("MCP tools/list pagination cursor repeated")
		}
		seenCursors[result.NextCursor] = struct{}{}
		cursor = result.NextCursor
	}
}

func buildCapabilityManifest(initialized *mcp.InitializeResult, tools *mcp.ListToolsResult) (CapabilityManifest, error) {
	if initialized == nil || tools == nil {
		return CapabilityManifest{}, fmt.Errorf("MCP capability response is empty")
	}
	fingerprint, err := toolSchemaFingerprint(tools.Tools)
	if err != nil {
		return CapabilityManifest{}, err
	}
	names := make([]string, 0, len(tools.Tools))
	seenNames := make(map[string]struct{}, len(tools.Tools))
	for _, tool := range tools.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return CapabilityManifest{}, fmt.Errorf("MCP tool name is empty")
		}
		if _, exists := seenNames[name]; exists {
			return CapabilityManifest{}, fmt.Errorf("MCP tool name %q is duplicated", name)
		}
		seenNames[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return CapabilityManifest{
		ProtocolVersion:   initialized.ProtocolVersion,
		ServerName:        initialized.ServerInfo.Name,
		ServerVersion:     initialized.ServerInfo.Version,
		ToolNames:         names,
		ToolCount:         len(names),
		SchemaFingerprint: fingerprint,
	}, nil
}

func toolSchemaFingerprint(tools []mcp.Tool) (string, error) {
	entries := make([]string, 0, len(tools))
	for _, tool := range tools {
		payload, err := json.Marshal(struct {
			Name         string               `json:"name"`
			InputSchema  mcp.ToolInputSchema  `json:"inputSchema"`
			OutputSchema mcp.ToolOutputSchema `json:"outputSchema,omitempty"`
			RawInput     json.RawMessage      `json:"rawInput,omitempty"`
			RawOutput    json.RawMessage      `json:"rawOutput,omitempty"`
		}{tool.Name, tool.InputSchema, tool.OutputSchema, tool.RawInputSchema, tool.RawOutputSchema})
		if err != nil {
			return "", fmt.Errorf("marshal tool schema %q: %w", tool.Name, err)
		}
		entries = append(entries, string(payload))
	}
	sort.Strings(entries)
	hash := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return hex.EncodeToString(hash[:]), nil
}

func validateCapabilityManifest(manifest CapabilityManifest, policy *CapabilityPolicy) error {
	if policy == nil {
		return nil
	}
	if policy.MaxTools > 0 && manifest.ToolCount > policy.MaxTools {
		return fmt.Errorf("MCP tool count %d exceeds configured maximum %d", manifest.ToolCount, policy.MaxTools)
	}
	if want := strings.TrimSpace(policy.ExpectedServerName); want != "" && manifest.ServerName != want {
		return fmt.Errorf("MCP server name %q does not match configured identity", manifest.ServerName)
	}
	if want := strings.TrimSpace(policy.ExpectedServerVersion); want != "" && manifest.ServerVersion != want {
		return fmt.Errorf("MCP server version %q does not match configured identity", manifest.ServerVersion)
	}
	if want := strings.TrimSpace(policy.ExpectedFingerprint); want != "" && !strings.EqualFold(manifest.SchemaFingerprint, want) {
		return fmt.Errorf("MCP tool schema fingerprint mismatch")
	}
	if len(policy.ExpectedToolNames) > 0 {
		want := append([]string(nil), policy.ExpectedToolNames...)
		for i := range want {
			want[i] = strings.TrimSpace(want[i])
		}
		sort.Strings(want)
		if !sameStrings(manifest.ToolNames, want) {
			return fmt.Errorf("MCP tool set does not match configured capability policy")
		}
	}
	return nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func validateArgs(args, allowedArgs []string) error {
	if len(allowedArgs) == 0 {
		return nil
	}
	if len(args) != len(allowedArgs) {
		return fmt.Errorf("MCP arguments do not match the configured allowlist")
	}
	for index := range args {
		if args[index] != allowedArgs[index] {
			return fmt.Errorf("MCP arguments do not match the configured allowlist")
		}
	}
	return nil
}

func restrictedCommandFactory(environment []string) transport.CommandFunc {
	return func(ctx context.Context, command string, _ []string, args []string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, command, args...)
		// Setting Env replaces (rather than appends to) the parent process
		// environment, defeating mcp-go's default os.Environ() inheritance.
		cmd.Env = append([]string(nil), environment...)
		return cmd, nil
	}
}

func validateCommand(command string, allowedCommands []string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("MCP command is empty")
	}
	if !filepath.IsAbs(command) {
		return fmt.Errorf("MCP command must be an absolute path")
	}
	if len(allowedCommands) == 0 {
		return nil
	}
	cleanCommand := filepath.Clean(command)
	for _, allowed := range allowedCommands {
		if cleanCommand == filepath.Clean(strings.TrimSpace(allowed)) {
			return nil
		}
	}
	return fmt.Errorf("MCP command is not in the configured allowlist")
}

func sanitizedEnvironment(environment, allowlist []string) []string {
	allowed := make(map[string]struct{}, len(allowlist))
	for _, key := range allowlist {
		key = strings.TrimSpace(key)
		if key != "" {
			allowed[key] = struct{}{}
		}
	}
	result := make([]string, 0, len(allowed))
	for _, item := range environment {
		key, _, found := strings.Cut(item, "=")
		if found {
			if _, ok := allowed[key]; ok {
				result = append(result, item)
			}
		}
	}
	return result
}

func (c *StdioClient) ListTools(ctx context.Context, req mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return nil, fmt.Errorf("MCP client is closed")
	}
	return c.client.ListTools(ctx, req)
}

func (c *StdioClient) CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return nil, fmt.Errorf("MCP client is closed")
	}
	return c.client.CallTool(ctx, req)
}

func (c *StdioClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return nil
	}
	err := c.client.Close()
	c.client = nil
	return err
}

// Manifest returns the sanitized capability snapshot captured at startup.
func (c *StdioClient) Manifest() CapabilityManifest {
	c.mu.Lock()
	defer c.mu.Unlock()
	manifest := c.manifest
	manifest.ToolNames = append([]string(nil), c.manifest.ToolNames...)
	return manifest
}

var _ Client = (*StdioClient)(nil)
