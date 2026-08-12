package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
}

// Client is the subset of MCP operations used by the platform adapters.
type Client interface {
	ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

// StdioClient owns one MCP server subprocess and its MCP protocol session.
type StdioClient struct {
	mu     sync.Mutex
	client *client.Client
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
	if _, err = c.Initialize(ctx, mcp.InitializeRequest{
		Request: mcp.Request{Method: "initialize"},
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "wisesentinel-platform", Version: "dev"},
		},
	}); err != nil {
		return nil, fmt.Errorf("initialize MCP session: %w", err)
	}
	if _, err = c.ListTools(ctx, mcp.ListToolsRequest{}); err != nil {
		return nil, fmt.Errorf("list MCP tools: %w", err)
	}
	closeOnError = false
	return wrapped, nil
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

var _ Client = (*StdioClient)(nil)
