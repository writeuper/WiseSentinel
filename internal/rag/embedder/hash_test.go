package embedder

import (
	"context"
	"testing"
)

func TestHashEmbedderPreservesLexicalOverlap(t *testing.T) {
	emb := NewHashEmbedder()
	vectors, err := emb.Embed(context.Background(), []string{
		"payment gateway 503 runbook",
		"payment gateway 503 upstream retry procedure",
		"redis login timeout connection pool",
	})
	if err != nil {
		t.Fatal(err)
	}
	if similarity(vectors[0], vectors[1]) <= similarity(vectors[0], vectors[2]) {
		t.Fatalf("overlapping text should rank above unrelated text: related=%v unrelated=%v", similarity(vectors[0], vectors[1]), similarity(vectors[0], vectors[2]))
	}
}

func TestHashEmbedderHandlesCJKWithoutFullSentenceEquality(t *testing.T) {
	emb := NewHashEmbedder()
	vectors, err := emb.Embed(context.Background(), []string{
		"订单服务数据库死锁排查",
		"订单服务发生死锁时检查数据库锁等待",
		"支付网关503重试",
	})
	if err != nil {
		t.Fatal(err)
	}
	if similarity(vectors[0], vectors[1]) <= similarity(vectors[0], vectors[2]) {
		t.Fatalf("CJK overlap should rank above unrelated text: related=%v unrelated=%v", similarity(vectors[0], vectors[1]), similarity(vectors[0], vectors[2]))
	}
}

func similarity(left, right []float32) float64 {
	var result float64
	for index := range left {
		result += float64(left[index] * right[index])
	}
	return result
}
