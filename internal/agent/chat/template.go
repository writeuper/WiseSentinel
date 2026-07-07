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
	documents  string
	once       sync.Once
	systemMsg  *schema.Message
}

// NewChatTemplate creates a ChatTemplate with the given documents.
func NewChatTemplate(documents string) *ChatTemplate {
	return &ChatTemplate{
		documents: documents,
	}
}

// BuildSystemPrompt returns the system prompt string.
// This is public so it can be verified in unit tests.
func (t *ChatTemplate) BuildSystemPrompt() string {
	now := time.Now().Format("2006-01-02 15:04:05 MST")
	return buildSystemPrompt(now, t.documents)
}

// BuildSystemPromptStatic is a test-friendly variant that accepts a fixed time.
func (t *ChatTemplate) BuildSystemPromptStatic(now string) string {
	return buildSystemPrompt(now, t.documents)
}

func buildSystemPrompt(now, documents string) string {
	return "你是智哨(WiseSentinel)智能运维助手，负责处理运维相关的问题。\n\n回答规则：\n- 回答必须基于提供的文档与工具返回结果，不得编造信息\n- 引用文档时标注来源\n- 保持专业、简洁的运维风格\n\n当前时间：" + now + "\n相关文档：\n" + documents
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