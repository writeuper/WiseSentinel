package rag

import (
	"reflect"
	"testing"

	"wisesentinel-platform/internal/domain"
)

func TestBuildRetrievalQueriesKeepsOriginalAndBoundsVariants(t *testing.T) {
	req := &domain.RetrieveRequest{
		Query:                "服务下线告警怎么处理？",
		QueryVariants:        []string{"服务下线告警排查", "服务下线告警排查", "升级条件", "额外变体"},
		EnableQueryExpansion: true,
	}
	got, expanded := buildRetrievalQueries(req)
	want := []string{"服务下线告警怎么处理？", "服务下线告警排查", "升级条件", "额外变体"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("queries = %#v, want %#v", got, want)
	}
	if !expanded {
		t.Fatal("multiple queries must be marked expanded")
	}
}

func TestBuildRetrievalQueriesUsesConservativeDeterministicVariant(t *testing.T) {
	got, expanded := buildRetrievalQueries(&domain.RetrieveRequest{
		Query:                "服务下线告警怎么处理？",
		EnableQueryExpansion: true,
	})
	want := []string{"服务下线告警怎么处理？", "服务下线告警排查步骤 处置流程"}
	if !reflect.DeepEqual(got, want) || !expanded {
		t.Fatalf("queries/expanded = %#v/%t, want %#v/true", got, expanded, want)
	}
}

func TestBuildRetrievalQueriesDoesNotInventVariantForUnclearQuestion(t *testing.T) {
	got, expanded := buildRetrievalQueries(&domain.RetrieveRequest{Query: "那个呢？", EnableQueryExpansion: true})
	if want := []string{"那个呢？"}; !reflect.DeepEqual(got, want) || expanded {
		t.Fatalf("queries/expanded = %#v/%t, want %#v/false", got, expanded, want)
	}
}
