package chat

import (
	"context"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

// ChatTemplate builds system prompts and manages message injection
// for the Eino ReAct agent. It replaces the inline sync.Once closure
// in buildReActAgent, making the prompt construction testable.
type ChatTemplate struct {
	documents    string
	customPrompt string
	once         sync.Once
	systemMsg    *schema.Message
}

// NewChatTemplate creates a ChatTemplate with the given documents.
func NewChatTemplate(documents string, customPrompt ...string) *ChatTemplate {
	base := ""
	if len(customPrompt) > 0 {
		base = customPrompt[0]
	}
	return &ChatTemplate{
		documents:    documents,
		customPrompt: base,
	}
}

// BuildSystemPrompt returns the system prompt string.
// This is public so it can be verified in unit tests.
func (t *ChatTemplate) BuildSystemPrompt() string {
	now := time.Now().Format("2006-01-02 15:04:05 MST")
	return buildSystemPromptWithBase(t.customPrompt, now, t.documents)
}

// BuildSystemPromptStatic is a test-friendly variant that accepts a fixed time.
func (t *ChatTemplate) BuildSystemPromptStatic(now string) string {
	return buildSystemPromptWithBase(t.customPrompt, now, t.documents)
}

func buildSystemPrompt(now, documents string) string {
	return buildSystemPromptWithBase("你是智哨(WiseSentinel)智能运维助手，负责处理运维相关的问题。", now, documents)
}

func buildSystemPromptWithBase(base, now, documents string) string {
	if base == "" {
		base = "你是智哨(WiseSentinel)智能运维助手，负责处理运维相关的问题。"
	}
	return base + "\n\n" +
		"回答规则：\n" +
		"- 回答必须基于提供的文档与工具返回结果，不得编造信息\n" +
		"- 用户提到知识库、内部手册、内部文档或要求根据文档回答时，必须先调用 query_internal_docs；没有相关检索结果时明确说明未找到，不得把无关文档当作依据\n" +
		"- 用户询问当前时间、北京时间或时区时间时，必须调用 get_current_time，不得仅使用系统提示中的时间\n" +
		"- 只有用户明确要求实时日志、指标或告警时，才调用对应实时工具\n" +
		"- 调用 query_logs 时，input 必须是 JSON 对象，且 query 必须是非空字符串；不要发送空对象或缺少 query 的参数\n" +
		"- 工具调用失败时说明工具失败原因，不得伪造工具证据或 citation\n" +
		"- 引用文档时标注来源\n" +
		"- 保持专业、简洁的运维风格\n\n" +
		"工具调用规则（重要）：\n" +
		"- 当你需要查询数据时，必须通过 function calling 协议调用工具，由系统自动执行。不得在回答中用文字描述\"我会调用 XX 工具\"或\"<工具调用> XX\"，这些描述不会被系统执行\n" +
		"- 调用工具后，你会收到系统返回的工具执行结果，基于该结果继续推理或生成最终回答\n" +
		"- 如果你不需要查询任何数据，直接基于已有知识回答即可，无需调用工具\n\n" +
		"当前时间：" + now + "\n相关文档：\n" + documents
}

// MessageModifier returns a function suitable for react.AgentConfig.MessageModifier.
// It injects the system prompt on the first call only.
func (t *ChatTemplate) MessageModifier() func(context.Context, []*schema.Message) []*schema.Message {
	return func(_ context.Context, input []*schema.Message) []*schema.Message {
		t.once.Do(func() {
			t.systemMsg = schema.SystemMessage(t.BuildSystemPrompt())
		})
		result := make([]*schema.Message, 0, len(input)+1)
		result = append(result, t.systemMsg)
		result = append(result, input...)
		return result
	}
}

// Reset allows re-initializing the template for a new request.
func (t *ChatTemplate) Reset() {
	t.once = sync.Once{}
	t.systemMsg = nil
}
