package repository

import "testing"

func TestNormalizeEvidenceDocIDsBoundsAndDeduplicates(t *testing.T) {
	ids := normalizeEvidenceDocIDs([]string{" doc-a ", "doc-a", "", "bad\nvalue", string(make([]byte, 129)), "doc-b"})
	if len(ids) != 2 || ids[0] != "doc-a" || ids[1] != "doc-b" {
		t.Fatalf("normalized evidence ids = %#v", ids)
	}
}

func TestNormalizeEvidenceDocIDsCapsEvidenceSet(t *testing.T) {
	input := make([]string, 40)
	for i := range input {
		input[i] = "doc-" + string(rune('a'+i))
	}
	if got := normalizeEvidenceDocIDs(input); len(got) != 32 {
		t.Fatalf("evidence ids length = %d, want 32", len(got))
	}
}
