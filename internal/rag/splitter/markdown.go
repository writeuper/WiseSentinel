package splitter

import (
	"strings"

	"github.com/google/uuid"
)

// Chunk is a split document segment ready for embedding.
type Chunk struct {
	ID      string
	Content string
	Index   int
	Title   string
}

// SplitMarkdown splits markdown by # and ## headers (Demo-compatible strategy).
func SplitMarkdown(content string) []Chunk {
	lines := strings.Split(content, "\n")
	var chunks []Chunk
	var currentTitle string
	var sectionTitle string
	var builder strings.Builder
	chunkIndex := 0

	flush := func() {
		text := strings.TrimSpace(builder.String())
		builder.Reset()
		if text == "" {
			return
		}
		title := sectionTitle
		if title == "" {
			title = currentTitle
		}
		chunks = append(chunks, Chunk{
			ID:      uuid.NewString(),
			Content: text,
			Index:   chunkIndex,
			Title:   title,
		})
		chunkIndex++
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## "):
			flush()
			currentTitle = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			sectionTitle = ""
			builder.WriteString(line)
			builder.WriteString("\n")
		case strings.HasPrefix(trimmed, "## "):
			flush()
			sectionTitle = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			if currentTitle != "" {
				builder.WriteString("# ")
				builder.WriteString(currentTitle)
				builder.WriteString("\n")
			}
			builder.WriteString(line)
			builder.WriteString("\n")
		default:
			if builder.Len() > 0 || trimmed != "" {
				builder.WriteString(line)
				builder.WriteString("\n")
			}
		}
	}
	flush()

	if len(chunks) == 0 && strings.TrimSpace(content) != "" {
		chunks = append(chunks, Chunk{
			ID:      uuid.NewString(),
			Content: strings.TrimSpace(content),
			Index:   0,
		})
	}
	return chunks
}
