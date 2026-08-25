package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"wisesentinel-platform/internal/domain"
	"wisesentinel-platform/internal/pkg/apperr"
	"wisesentinel-platform/internal/pkg/ctxkeys"
)

func TestIsModelOverloadedErrorRecognizesTypedAndEinoFormattedError(t *testing.T) {
	if !isModelOverloadedError(apperr.ErrModelOverloaded) {
		t.Fatal("typed overload error was not recognized")
	}
	if !isModelOverloadedError(errors.New("[NodeRunError] " + apperr.ErrModelOverloaded.Message)) {
		t.Fatal("Eino-formatted overload error was not recognized")
	}
	if isModelOverloadedError(errors.New("unrelated upstream failure")) {
		t.Fatal("unrelated error was recognized as overload")
	}
}

func TestChatAgentRejectsNilRequests(t *testing.T) {
	agent := NewAgent(nil, nil, nil)
	if _, err := agent.Invoke(context.Background(), nil); !errors.Is(err, apperr.ErrBadRequest) {
		t.Fatalf("Invoke(nil) error = %v, want bad request", err)
	}
	if _, err := agent.Stream(context.Background(), nil); !errors.Is(err, apperr.ErrBadRequest) {
		t.Fatalf("Stream(nil) error = %v, want bad request", err)
	}
}

func TestTracePersistenceContextSurvivesStreamCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), "trace", "trace-1"))
	cancelParent()
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), 2*time.Second)
	defer cancel()
	if persistCtx.Err() != nil {
		t.Fatalf("trace persistence context canceled: %v", persistCtx.Err())
	}
	if got := persistCtx.Value("trace"); got != "trace-1" {
		t.Fatalf("trace context value = %v, want trace-1", got)
	}
}

func TestPlatformRoleQueryUsesAuditableStaticCapabilityAnswer(t *testing.T) {
	if !isPlatformRoleQuery("平台支持哪些 Agent 角色协作") {
		t.Fatal("role collaboration query was not recognized")
	}
	if isPlatformRoleQuery("如何排查 API 5xx") {
		t.Fatal("operational query was recognized as platform role metadata")
	}
	answer := platformRoleAnswer()
	for _, role := range []string{"架构师", "Golang 后端", "前端", "自动化测试"} {
		if !strings.Contains(answer, role) {
			t.Fatalf("answer missing role %q: %s", role, answer)
		}
	}
}

func TestPlatformCapabilityAnswersAvoidOperationalRAGForResilienceAndReports(t *testing.T) {
	tests := []struct {
		query, step string
		keywords    []string
	}{
		{"如何查看 Agent 的平均执行步数", "static_agent_step_metrics", []string{"平均 Trace 步数", "最小值", "最大值"}},
		{"模型服务超时如何降级和重试", "static_platform_model_resilience", []string{"超时", "有限次数重试", "熔断"}},
		{"如何导出不包含模型原文的质量报告", "static_quality_report_projection", []string{"聚合指标", "脱敏", "Token"}},
		{"企业平台如何对模型输出做敏感信息脱敏", "static_model_output_redaction", []string{"SSE", "Trace", "Token"}},
		{"如何配置一个新的 Agent 评测 Case", "static_eval_case_configuration", []string{"case_id", "expected_route", "持久化 Trace Step"}},
	}
	for _, tc := range tests {
		answer, step, ok := platformCapabilityAnswer(tc.query)
		if !ok || step != tc.step {
			t.Fatalf("query %q classified as ok=%t step=%q, want %q", tc.query, ok, step, tc.step)
		}
		for _, keyword := range tc.keywords {
			if !strings.Contains(answer, keyword) {
				t.Fatalf("query %q answer missing %q: %s", tc.query, keyword, answer)
			}
		}
	}
	if _, _, ok := platformCapabilityAnswer("如何排查 Redis timeout"); ok {
		t.Fatal("component operational query must remain on RAG/ops path")
	}
}

func TestExtractConversionTimeAndTarget(t *testing.T) {
	if got := extractConversionTime("请把 2026-08-13 09:00:00 从北京时间转换为 UTC"); got != "2026-08-13 09:00:00" {
		t.Fatalf("explicit time = %q", got)
	}
	if got := extractConversionTime("把当前时间转换为 UTC"); got != "" {
		t.Fatalf("unexpected implicit time = %q", got)
	}
	if got := timeConversionTarget("转换为美国东部时间"); got != "America/New_York" {
		t.Fatalf("target = %q", got)
	}
	if got := timeConversionTarget("转换为 UTC"); got != "UTC" {
		t.Fatalf("target = %q", got)
	}
}

func TestChatRAGTopKExpandsExplicitRunbookQueries(t *testing.T) {
	if got := chatRAGTopK("根据内部 Kubernetes 排查手册说明检查顺序、止血和升级条件"); got != runbookRAGTopK {
		t.Fatalf("runbook topK = %d, want %d", got, runbookRAGTopK)
	}
	if got := chatRAGTopK("什么是 Agent 平台"); got != defaultChatRAGTopK {
		t.Fatalf("ordinary topK = %d, want %d", got, defaultChatRAGTopK)
	}
}

func TestShouldExpandRAGQuery(t *testing.T) {
	if !shouldExpandRAGQuery("服务下线告警怎么处理？") {
		t.Fatal("ambiguous procedural question should enable bounded expansion")
	}
	if shouldExpandRAGQuery("告警规则 CPUHigh") {
		t.Fatal("precise keyword query should keep the single-query path")
	}
}

func TestStreamErrorDataOnlyExposesKnownOverloadAsStructuredData(t *testing.T) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(streamErrorData(apperr.ErrModelOverloaded, "fallback")), &payload); err != nil {
		t.Fatalf("structured overload error: %v", err)
	}
	if payload["code"] != float64(apperr.ErrModelOverloaded.Code) || payload["retry_after_seconds"] != float64(2) {
		t.Fatalf("overload payload = %#v", payload)
	}
	if got := streamErrorData(errors.New("upstream echoed secret=must-not-leak"), "稳定失败提示"); got != "稳定失败提示" {
		t.Fatalf("unknown stream error = %q", got)
	}
}

type currentTimeToolGateway struct {
	output         string
	recordEvidence bool
}

type retrievalTestRAG struct {
	decision *domain.RAGRouteDecision
	err      error
}

func (r *retrievalTestRAG) Retrieve(context.Context, *domain.RetrieveRequest) (*domain.RetrieveResponse, error) {
	if r.decision == nil {
		return nil, r.err
	}
	return r.decision.Response, r.err
}

func (r *retrievalTestRAG) Route(context.Context, *domain.RetrieveRequest) (*domain.RAGRouteDecision, error) {
	return r.decision, r.err
}

func (*retrievalTestRAG) SubmitIndexTask(context.Context, *domain.IndexTaskRequest) (string, error) {
	return "", nil
}

func (*retrievalTestRAG) GetIndexTask(context.Context, string, string) (*domain.IndexTask, error) {
	return nil, nil
}

func (*retrievalTestRAG) DeleteDocumentChunks(context.Context, string, string) error { return nil }

func TestRetrieveDocsRecordsDistinctQualityOutcomes(t *testing.T) {
	req := &domain.ChatAgentRequest{TenantID: "tenant-a", Query: "runbook", Options: domain.ChatOptions{EnableRAG: true}}
	tests := []struct {
		name    string
		rag     domain.RAGService
		want    string
		wantErr bool
	}{
		{name: "disabled dependency", rag: nil, want: ragStepError, wantErr: true},
		{name: "dependency error", rag: &retrievalTestRAG{err: errors.New("milvus timeout")}, want: ragStepError, wantErr: true},
		{name: "empty result", rag: &retrievalTestRAG{decision: &domain.RAGRouteDecision{Confidence: domain.ConfidenceLow, Response: &domain.RetrieveResponse{}}}, want: ragStepEmpty},
		{name: "evidence found", rag: &retrievalTestRAG{decision: &domain.RAGRouteDecision{Confidence: domain.ConfidenceHigh, Response: &domain.RetrieveResponse{Documents: []domain.RetrievedDocument{{DocID: "doc-1", ChunkID: "chunk-1", Source: "runbook", Content: "safe evidence"}}}}}, want: ragStepSuccess},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent := NewAgent(nil, tc.rag, nil)
			_, _, status, errText := agent.retrieveDocs(context.Background(), req)
			if status != tc.want {
				t.Fatalf("status = %q, want %q", status, tc.want)
			}
			if (errText != "") != tc.wantErr {
				t.Fatalf("error text present = %t, want %t", errText != "", tc.wantErr)
			}
		})
	}

	_, _, status, errText := NewAgent(nil, nil, nil).retrieveDocs(context.Background(), &domain.ChatAgentRequest{Options: domain.ChatOptions{EnableRAG: false}})
	if status != ragStepSkipped || errText != "" {
		t.Fatalf("disabled RAG status/error = %q/%q, want skipped/empty", status, errText)
	}
}

func TestChatStreamReaderBackpressurePreservesAllEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &chatStreamReader{events: make(chan streamEventItem, 1), cancel: cancel, done: ctx.Done()}
	const total = 65
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for i := 0; i < total; i++ {
			if !reader.send("message", fmt.Sprintf("chunk-%d", i)) {
				return
			}
		}
		close(reader.events)
	}()

	// Let the producer fill the single-slot queue. The second send must wait
	// until the slow consumer starts reading; a lossy implementation would
	// have returned already and lost the tail.
	time.Sleep(10 * time.Millisecond)
	var received []string
	for {
		event, data, ok := reader.Next()
		if !ok {
			break
		}
		if event != "message" {
			t.Fatalf("event = %q, want message", event)
		}
		received = append(received, data)
	}
	<-finished
	if len(received) != total {
		t.Fatalf("received %d chunks, want %d", len(received), total)
	}
	for i, value := range received {
		if want := fmt.Sprintf("chunk-%d", i); value != want {
			t.Fatalf("chunk %d = %q, want %q", i, value, want)
		}
	}
}

func (g *currentTimeToolGateway) ListTools(context.Context, string, domain.AgentType) ([]domain.ToolMeta, error) {
	return []domain.ToolMeta{{Name: "get_current_time"}}, nil
}

func (g *currentTimeToolGateway) Invoke(ctx context.Context, request *domain.ToolInvokeRequest) (*domain.ToolInvokeResponse, error) {
	if g.recordEvidence {
		if sink := ctxkeys.ToolSinkFrom(ctx); sink != nil {
			*sink = append(*sink, domain.Evidence{ToolName: request.ToolName, Status: "success"})
		}
	}
	return &domain.ToolInvokeResponse{Output: g.output, Status: "success", LatencyMS: 1}, nil
}

func TestFormatCurrentTimeAnswer(t *testing.T) {
	output := `{"time":"2026-07-29 15:00:00","timezone":"Asia/Shanghai","rfc3339":"2026-07-29T07:00:00Z"}`
	answer := formatCurrentTimeAnswer(output)
	want := "当前北京时间（Asia/Shanghai）：2026-07-29 15:00:00。建议排查窗口：最近30分钟，即 2026-07-29 14:30:00 至 2026-07-29 15:00:00（北京时间）。证据来源：local 时间服务。"
	if answer != want {
		t.Fatalf("answer = %q, want %q", answer, want)
	}
}

func TestFormatCurrentTimeAnswerReturnsOriginalOnInvalidJSON(t *testing.T) {
	output := `not-json`
	if answer := formatCurrentTimeAnswer(output); answer != output {
		t.Fatalf("answer = %q, want original output %q", answer, output)
	}
}

func TestInvokeCurrentTimeReturnsNaturalLanguageAnswer(t *testing.T) {
	agent := NewAgent(nil, nil, &currentTimeToolGateway{output: `{"time":"2026-07-29 15:00:00","timezone":"Asia/Shanghai","rfc3339":"2026-07-29T07:00:00Z"}`})
	resp, err := agent.Invoke(context.Background(), &domain.ChatAgentRequest{SessionID: "session-1", Query: "现在几点"})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if !strings.Contains(resp.Answer, "当前北京时间（Asia/Shanghai）：2026-07-29 15:00:00") || !strings.Contains(resp.Answer, "最近30分钟") || !strings.Contains(resp.Answer, "证据来源：local") {
		t.Fatalf("unexpected answer: %q", resp.Answer)
	}
}

func TestInvokeCurrentTimeWithToolsRequiresRecordedEvidenceAndCompletion(t *testing.T) {
	output := `{"time":"2026-07-29 15:00:00","timezone":"Asia/Shanghai","rfc3339":"2026-07-29T07:00:00Z"}`
	request := &domain.ChatAgentRequest{SessionID: "session-1", Query: "现在几点", Options: domain.ChatOptions{EnableTools: true}}

	withoutEvidence := NewAgent(nil, nil, &currentTimeToolGateway{output: output})
	if _, err := withoutEvidence.Invoke(context.Background(), request); err == nil {
		t.Fatal("shortcut must not succeed when the tool gateway did not record evidence")
	}

	withEvidence := NewAgent(nil, nil, &currentTimeToolGateway{output: output, recordEvidence: true})
	response, err := withEvidence.Invoke(context.Background(), request)
	if err != nil {
		t.Fatalf("shortcut with recorded evidence failed: %v", err)
	}
	if response == nil || !strings.Contains(response.Answer, "当前北京时间") {
		t.Fatalf("response = %#v, want formatted completed response", response)
	}
}

func TestInvokeTimeConversionWithToolsUsesDedicatedAllowedTool(t *testing.T) {
	output := `{"source_timezone":"Asia/Shanghai","target_timezone":"UTC","source_time":"2026-07-29 15:00:00","time":"2026-07-29 07:00:00","rfc3339":"2026-07-29T07:00:00Z"}`
	request := &domain.ChatAgentRequest{SessionID: "session-1", Query: "把当前时间转换为 UTC", Options: domain.ChatOptions{EnableTools: true}}

	withoutEvidence := NewAgent(nil, nil, &currentTimeToolGateway{output: output})
	if _, err := withoutEvidence.Invoke(context.Background(), request); err == nil {
		t.Fatal("conversion shortcut must not succeed when the tool gateway did not record evidence")
	}

	withEvidence := NewAgent(nil, nil, &currentTimeToolGateway{output: output, recordEvidence: true})
	response, err := withEvidence.Invoke(context.Background(), request)
	if err != nil {
		t.Fatalf("conversion shortcut with recorded evidence failed: %v", err)
	}
	if response == nil || len(response.ToolCalls) != 1 || response.ToolCalls[0].Tool != "mcp_time_convert_time" {
		t.Fatalf("response = %#v, want conversion tool result", response)
	}
}

func TestStreamCurrentTimeKeepsToolOutputAndFormatsMessage(t *testing.T) {
	toolOutput := `{"time":"2026-07-29 15:00:00","timezone":"Asia/Shanghai","rfc3339":"2026-07-29T07:00:00Z"}`
	agent := NewAgent(nil, nil, &currentTimeToolGateway{output: toolOutput, recordEvidence: true})
	reader, err := agent.Stream(context.Background(), &domain.ChatAgentRequest{SessionID: "session-1", Query: "北京时间", Options: domain.ChatOptions{EnableTools: true}})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	defer reader.Close()

	var toolData, message string
	var done bool
	for {
		event, data, ok := reader.Next()
		if !ok {
			break
		}
		switch event {
		case "tool":
			toolData = data
		case "message":
			message = data
		case "done":
			done = true
		}
	}
	if !strings.Contains(toolData, strings.ReplaceAll(toolOutput, `"`, `\"`)) {
		t.Fatalf("tool event lost original output: %q", toolData)
	}
	if !strings.Contains(message, "当前北京时间（Asia/Shanghai）：2026-07-29 15:00:00") || !strings.Contains(message, "最近30分钟") || !strings.Contains(message, "证据来源：local") {
		t.Fatalf("unexpected message: %q", message)
	}
	if !done {
		t.Fatal("completed shortcut must emit done after completion validation")
	}
}

func TestStreamCurrentTimeWithToolsRejectsMissingEvidence(t *testing.T) {
	toolOutput := `{"time":"2026-07-29 15:00:00","timezone":"Asia/Shanghai","rfc3339":"2026-07-29T07:00:00Z"}`
	agent := NewAgent(nil, nil, &currentTimeToolGateway{output: toolOutput})
	reader, err := agent.Stream(context.Background(), &domain.ChatAgentRequest{SessionID: "session-1", Query: "北京时间", Options: domain.ChatOptions{EnableTools: true}})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	defer reader.Close()

	var errData string
	var done bool
	for {
		event, data, ok := reader.Next()
		if !ok {
			break
		}
		if event == "error" {
			errData = data
		}
		if event == "done" {
			done = true
		}
	}
	if !strings.Contains(errData, domain.CompletionToolMissing) {
		t.Fatalf("error = %q, want completion reason %q", errData, domain.CompletionToolMissing)
	}
	if done {
		t.Fatal("rejected shortcut must not emit a clean done event")
	}
}
