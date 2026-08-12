package ops

import (
	"encoding/json"
	"regexp"
	"strings"

	"wisesentinel-platform/internal/domain"
)

// taskPayload bundles everything we persist in ws_ops_task.detail_json so the
// Portal can reconstruct both the narration and the structured proof chain.
type taskPayload struct {
	Detail     []string                `json:"detail,omitempty"`
	Evidence   []domain.Evidence       `json:"evidence,omitempty"`
	Conclusion *domain.FaultConclusion `json:"conclusion,omitempty"`
}

// marshalPayload serializes the payload for persistence.
func marshalPayload(detail []string, evidence []domain.Evidence, conclusion *domain.FaultConclusion) string {
	p := taskPayload{Detail: detail, Evidence: evidence, Conclusion: conclusion}
	raw, err := json.Marshal(p)
	if err != nil {
		// Fallback: keep detail only so the task still finishes.
		raw, _ = json.Marshal(taskPayload{Detail: detail})
	}
	return string(raw)
}

// unmarshalPayload reconstructs the payload from detail_json. It tolerates the
// legacy format where detail_json was a bare JSON array of strings.
func unmarshalPayload(detailJSON string) taskPayload {
	var p taskPayload
	if detailJSON == "" {
		return p
	}
	if err := json.Unmarshal([]byte(detailJSON), &p); err == nil {
		return p
	}
	// Legacy []string format.
	var legacy []string
	if err := json.Unmarshal([]byte(detailJSON), &legacy); err == nil {
		return taskPayload{Detail: legacy}
	}
	return p
}

// conclusionHeadings maps each FaultConclusion field to the Chinese heading
// the Ops prompt asks the model to emit.
var conclusionHeadings = map[string]string{
	"Symptom":     "故障现象",
	"Impact":      "影响范围",
	"RootCause":   "根因判断",
	"Workaround":  "临时止血",
	"Remediation": "根治建议",
}

// parseConclusion extracts a structured conclusion from the agent's free-text
// result. The Ops prompt instructs the model to emit the five section headings
// listed above; this parser pulls the text that follows each heading. When a
// section is missing we leave it empty rather than failing, so a partial answer
// still surfaces to the operator.
func parseConclusion(result string) *domain.FaultConclusion {
	if strings.TrimSpace(result) == "" {
		return nil
	}
	c := &domain.FaultConclusion{
		Confidence: "mid",
		Source:     "实时排查结论",
	}
	values := map[*string]string{
		&c.Symptom:     "故障现象",
		&c.Impact:      "影响范围",
		&c.RootCause:   "根因判断",
		&c.Workaround:  "临时止血",
		&c.Remediation: "根治建议",
	}
	for target, heading := range values {
		*target = extractSection(result, heading)
	}
	// If the model emitted a confidence hint, honor it.
	if m := confidencePattern.FindStringSubmatch(result); len(m) == 2 {
		c.Confidence = strings.ToLower(strings.TrimSpace(m[1]))
	}
	return c
}

// extractSection returns the text between `heading` (followed by ：/: or 换行)
// and the next known heading or end of text.
func extractSection(text, heading string) string {
	idx := strings.Index(text, heading)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(heading):]
	// Skip a single colon (full-width or ascii) and whitespace.
	rest = strings.TrimLeft(rest, "：:\r\n ")
	// Find the earliest next known heading.
	end := len(rest)
	for _, h := range conclusionHeadings {
		if h == heading {
			continue
		}
		if i := strings.Index(rest, h); i >= 0 && i < end {
			end = i
		}
	}
	return strings.TrimSpace(rest[:end])
}

var confidencePattern = regexp.MustCompile(`(?i)置信度[：:]\s*(high|mid|low)`)
