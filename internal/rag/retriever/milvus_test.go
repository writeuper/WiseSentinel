package retriever

import (
	"context"
	"math"
	"testing"

	"wisesentinel-platform/internal/domain"
)

func TestRetrieveRejectsNilRequestBeforeDependencyAccess(t *testing.T) {
	var retriever *MilvusRetriever
	if _, err := retriever.Retrieve(context.Background(), nil); err == nil {
		t.Fatal("nil retrieve request must return an error")
	}
}

func TestRetrieveRejectsMissingEmbedder(t *testing.T) {
	retriever := &MilvusRetriever{}
	if _, err := retriever.Retrieve(context.Background(), &domain.RetrieveRequest{Query: "health"}); err == nil {
		t.Fatal("missing embedding provider must return an error")
	}
}

func TestNormalizeL2Score(t *testing.T) {
	cases := []struct {
		distance float64
		want     float64
	}{
		{0, 1.0},    // identical vectors → similarity 1
		{0.5, 0.75}, // 1 - 0.5/2
		{1.0, 0.5},  // 1 - 1/2
		{2.0, 0.0},  // max distance → 0
		{3.0, 0.0},  // beyond ref → clamped to 0
		{-1.0, 1.0}, // negative guard → 1
	}
	for _, c := range cases {
		got := normalizeL2Score(c.distance)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("normalizeL2Score(%v) = %v, want %v", c.distance, got, c.want)
		}
	}
}

func TestConfidenceFromScore(t *testing.T) {
	cases := []struct {
		score float64
		want  domain.ConfidenceLevel
	}{
		{0.90, domain.ConfidenceHigh},
		{0.75, domain.ConfidenceHigh}, // boundary inclusive
		{0.60, domain.ConfidenceMid},
		{0.50, domain.ConfidenceMid}, // boundary inclusive
		{0.49, domain.ConfidenceLow},
		{0.0, domain.ConfidenceLow},
		{math.NaN(), domain.ConfidenceLow},
		{math.Inf(1), domain.ConfidenceLow},
	}
	for _, c := range cases {
		got := confidenceFromScore(c.score)
		if got != c.want {
			t.Errorf("confidenceFromScore(%v) = %v, want %v", c.score, got, c.want)
		}
	}
}

func TestApplyLayerWeight(t *testing.T) {
	// temp knowledge must be downweighted so it cannot alone reach high confidence.
	score := 0.9
	if got := applyLayerWeight(score, domain.KnowledgeLayerTemp); got >= highConfThreshold {
		t.Errorf("temp layer weighted score %v should stay below high-conf threshold %v", got, highConfThreshold)
	}
	// static layer keeps the raw score.
	if got := applyLayerWeight(score, domain.KnowledgeLayerStatic); got != score {
		t.Errorf("static layer weighted score %v, want %v", got, score)
	}
	// fault_case sits between static and temp.
	staticW := applyLayerWeight(score, domain.KnowledgeLayerStatic)
	caseW := applyLayerWeight(score, domain.KnowledgeLayerFaultCase)
	tempW := applyLayerWeight(score, domain.KnowledgeLayerTemp)
	if !(staticW >= caseW && caseW >= tempW) {
		t.Errorf("expected static>=case>=temp, got static=%v case=%v temp=%v", staticW, caseW, tempW)
	}
}

func TestClassifyLayer(t *testing.T) {
	if got := classifyLayer(nil); got != domain.KnowledgeLayerStatic {
		t.Errorf("classifyLayer(nil) = %v, want static", got)
	}
	if got := classifyLayer(map[string]any{"_layer": "temp"}); got != domain.KnowledgeLayerTemp {
		t.Errorf("classifyLayer(_layer=temp) = %v, want temp", got)
	}
	if got := classifyLayer(map[string]any{"layer": "fault_case"}); got != domain.KnowledgeLayerFaultCase {
		t.Errorf("classifyLayer(layer=fault_case) = %v, want fault_case", got)
	}
}

func TestFilterActiveGenerationDocumentsFailsClosed(t *testing.T) {
	docs := []domain.RetrievedDocument{
		{DocID: "legacy", Metadata: map[string]any{}},
		{DocID: "published", Metadata: map[string]any{"generation": float64(3)}},
		{DocID: "published", Metadata: map[string]any{"generation": float64(2)}},
		{DocID: "staged", Metadata: map[string]any{"generation": float64(4)}},
		{DocID: "unknown", Metadata: map[string]any{"generation": float64(1)}},
	}
	states := map[string]domain.DocumentIndexGeneration{
		"legacy":    {ActiveGeneration: 0, LegacyAllowed: true},
		"published": {ActiveGeneration: 3, LegacyAllowed: false},
		"staged":    {ActiveGeneration: 0, LegacyAllowed: true},
	}
	got := filterActiveGenerationDocuments(docs, states)
	if len(got) != 2 || got[0].DocID != "legacy" || got[1].DocID != "published" {
		t.Fatalf("filtered docs = %#v", got)
	}
}
