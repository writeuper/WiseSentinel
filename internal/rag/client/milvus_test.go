package client

import "testing"

func TestParsePhysicalVectorCount(t *testing.T) {
	count, err := parsePhysicalVectorCount(map[string]string{"row_count": "42"})
	if err != nil || count != 42 {
		t.Fatalf("count = %d, err = %v", count, err)
	}
	for _, statistics := range []map[string]string{{}, {"row_count": "not-a-number"}, {"row_count": "-1"}} {
		if _, err := parsePhysicalVectorCount(statistics); err == nil {
			t.Fatalf("invalid statistics accepted: %#v", statistics)
		}
	}
}
