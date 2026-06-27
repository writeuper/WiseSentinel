package splitter_test

import (
	"strings"
	"testing"

	"wisesentinel-platform/internal/rag/splitter"
)

func TestSplitMarkdown_ByHeaders(t *testing.T) {
	content := `# 服务下线告警

## 现象
服务不可用

## 处理步骤
检查日志`

	chunks := splitter.SplitMarkdown(content)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	found := false
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "服务不可用") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected chunk with section body, got %#v", chunks)
	}
}

func TestSplitMarkdown_PlainText(t *testing.T) {
	chunks := splitter.SplitMarkdown("plain text document")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
}
