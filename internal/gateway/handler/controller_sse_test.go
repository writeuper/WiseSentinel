package handler

import (
	"encoding/json"
	"testing"

	"wisesentinel-platform/internal/domain"
)

type fixedStreamReader struct {
	events []streamEvent
	next   int
}

type streamEvent struct {
	event string
	data  string
}

func (r *fixedStreamReader) Next() (string, string, bool) {
	if r.next >= len(r.events) {
		return "", "", false
	}
	event := r.events[r.next]
	r.next++
	return event.event, event.data, true
}

func (r *fixedStreamReader) Close() error { return nil }

func TestFormatSSEPrefixesEveryMultilinePayloadLine(t *testing.T) {
	got := formatSSE("message", "first\r\nsecond\nthird")
	want := "event: message\ndata: first\ndata: second\ndata: third\n\n"
	if got != want {
		t.Fatalf("formatSSE() = %q, want %q", got, want)
	}
}

func TestSafeSSECitationProjectsExpectedFields(t *testing.T) {
	raw, err := json.Marshal(domain.Citation{DocID: "doc-1", ChunkID: "chunk-1", Source: "handbook.md", Snippet: "verified evidence", Version: "generation-7"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(safeSSECitation(string(raw))), &got); err != nil {
		t.Fatalf("safeSSECitation JSON: %v", err)
	}
	if got["doc_id"] != "doc-1" || got["chunk_id"] != "chunk-1" || got["source"] != "handbook.md" || got["snippet"] != "verified evidence" || got["version"] != "generation-7" {
		t.Fatalf("citation projection = %#v", got)
	}
	if got := safeSSECitation("not-json"); got != `{}` {
		t.Fatalf("invalid citation projection = %q, want {}", got)
	}
}

func TestConsumeChatStreamDefersDoneUntilSessionPersistence(t *testing.T) {
	reader := &fixedStreamReader{events: []streamEvent{
		{event: "connected", data: `{"status":"connected"}`},
		{event: "message", data: "first"},
		{event: "message", data: " second"},
		{event: "done", data: `{"trace_id":"trace-1"}`},
	}}
	var emitted []streamEvent

	completion := consumeChatStream(reader, func(event, data string) {
		emitted = append(emitted, streamEvent{event: event, data: data})
	})

	if completion.failed {
		t.Fatal("completion unexpectedly marked failed")
	}
	if completion.answer != "first second" {
		t.Fatalf("answer = %q, want full response", completion.answer)
	}
	if completion.doneData != `{"trace_id":"trace-1"}` {
		t.Fatalf("done data = %q", completion.doneData)
	}
	if !completion.finished {
		t.Fatal("done marker must be recorded as a clean completion")
	}
	for _, event := range emitted {
		if event.event == "done" {
			t.Fatal("done must be withheld until persistence succeeds")
		}
	}
}

func TestConsumeChatStreamRequiresDoneForCleanCompletion(t *testing.T) {
	reader := &fixedStreamReader{events: []streamEvent{{event: "message", data: "partial"}}}
	completion := consumeChatStream(reader, func(string, string) {})
	if completion.finished {
		t.Fatal("unexpected EOF must not be treated as a completed stream")
	}
}

func TestConsumeChatStreamDoesNotMarkErrorAsComplete(t *testing.T) {
	reader := &fixedStreamReader{events: []streamEvent{
		{event: "message", data: "partial"},
		{event: "error", data: "provider unavailable"},
		{event: "done", data: `{"trace_id":"trace-2"}`},
	}}
	var emitted []string

	completion := consumeChatStream(reader, func(event, _ string) {
		emitted = append(emitted, event)
	})

	if !completion.failed {
		t.Fatal("stream error must make completion fail")
	}
	if completion.answer != "partial" {
		t.Fatalf("answer = %q, want partial response retained only for display", completion.answer)
	}
	if len(emitted) != 2 || emitted[0] != "message" || emitted[1] != "error" {
		t.Fatalf("emitted events = %#v, want message then error without done", emitted)
	}
}

func TestValidChatIdempotencyKey(t *testing.T) {
	if !validChatIdempotencyKey("550e8400-e29b-41d4-a716-446655440000") {
		t.Fatal("UUID must be accepted")
	}
	for _, key := range []string{"short", "550e8400-e29b-41d4-a716-446655440000/unsafe", "550e8400-e29b-41d4-a716-446655440000 "} {
		if validChatIdempotencyKey(key) {
			t.Fatalf("invalid key accepted: %q", key)
		}
	}
}
