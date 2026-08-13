package handler

import (
	"bytes"
	"testing"
)

func TestReadBoundedUploadAcceptsExactLimit(t *testing.T) {
	content, err := readBoundedUpload(bytes.NewReader(bytes.Repeat([]byte("x"), 4)), 4)
	if err != nil || len(content) != 4 {
		t.Fatalf("exact limit read = %d, err=%v", len(content), err)
	}
}

func TestReadBoundedUploadRejectsUntrustedOversizeStream(t *testing.T) {
	if _, err := readBoundedUpload(bytes.NewReader(bytes.Repeat([]byte("x"), 5)), 4); err == nil {
		t.Fatal("stream exceeding declared upload budget must be rejected")
	}
}

func TestReadBoundedUploadRejectsNilReader(t *testing.T) {
	if _, err := readBoundedUpload(nil, 4); err == nil {
		t.Fatal("nil reader must be rejected")
	}
}
