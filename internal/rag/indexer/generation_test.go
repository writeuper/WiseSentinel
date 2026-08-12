package indexer

import "testing"

func TestDeterministicChunkIDScopesTenantDocumentAndGeneration(t *testing.T) {
	base := DeterministicChunkID("tenant-a", "doc-a", 3, 1, "same content")
	if base == "" {
		t.Fatal("expected deterministic chunk ID")
	}
	if got := DeterministicChunkID("tenant-a", "doc-a", 3, 1, "same content"); got != base {
		t.Fatalf("same input ID = %q, want %q", got, base)
	}
	for _, different := range []string{
		DeterministicChunkID("tenant-b", "doc-a", 3, 1, "same content"),
		DeterministicChunkID("tenant-a", "doc-b", 3, 1, "same content"),
		DeterministicChunkID("tenant-a", "doc-a", 4, 1, "same content"),
		DeterministicChunkID("tenant-a", "doc-a", 3, 2, "same content"),
		DeterministicChunkID("tenant-a", "doc-a", 3, 1, "changed content"),
	} {
		if different == base {
			t.Fatal("chunk ID omitted a staging identity component")
		}
	}
}
