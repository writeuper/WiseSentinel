package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"wisesentinel-platform/internal/repository"
)

type fakeVectorGCStore struct {
	finishedStatus string
	finishedError  string
	retried        bool
	retryAt        time.Time
}

func (s *fakeVectorGCStore) ListRunnable(context.Context, int, time.Time) ([]*repository.VectorGCTask, error) {
	return nil, nil
}
func (s *fakeVectorGCStore) Get(context.Context, string, string, string) (*repository.VectorGCTask, error) {
	return nil, nil
}
func (s *fakeVectorGCStore) Claim(context.Context, string, string, string, string, time.Time) (bool, error) {
	return false, nil
}
func (s *fakeVectorGCStore) FinishIfOwned(_ context.Context, _, _, _, _, status, lastError string) (bool, error) {
	s.finishedStatus, s.finishedError = status, lastError
	return true, nil
}
func (s *fakeVectorGCStore) RetryIfOwned(_ context.Context, _ *repository.VectorGCTask, _ string, retryAt time.Time, _ string) (bool, error) {
	s.retried, s.retryAt = true, retryAt
	return true, nil
}

type fakeVectorGCSafety struct{ generation, legacy, documentAll bool }

func (s fakeVectorGCSafety) CanDeleteGeneration(context.Context, string, string, uint64) (bool, error) {
	return s.generation, nil
}
func (s fakeVectorGCSafety) CanDeleteLegacy(context.Context, string, string) (bool, error) {
	return s.legacy, nil
}
func (s fakeVectorGCSafety) CanDeleteDocumentAll(context.Context, string, string) (bool, error) {
	return s.documentAll, nil
}

type fakeVectorGCDeleter struct {
	generationCalls  int
	legacyCalls      int
	documentAllCalls int
	err              error
}

func (d *fakeVectorGCDeleter) DeleteByGeneration(context.Context, string, string, uint64) error {
	d.generationCalls++
	return d.err
}
func (d *fakeVectorGCDeleter) DeleteLegacyByDocID(context.Context, string, string) error {
	d.legacyCalls++
	return d.err
}
func (d *fakeVectorGCDeleter) DeleteByDocID(context.Context, string, string) error {
	d.documentAllCalls++
	return d.err
}

func TestVectorGCWorkerSkipsProtectedActiveGeneration(t *testing.T) {
	store, deleter := &fakeVectorGCStore{}, &fakeVectorGCDeleter{}
	worker := newVectorGCWorker(store, fakeVectorGCSafety{}, deleter)
	worker.run(&repository.VectorGCTask{TenantID: "t", DocID: "d", TargetKey: "generation:2", TargetKind: repository.VectorGCTargetGeneration, TargetGeneration: 2, AttemptCount: 1, MaxAttempts: 8}, "token")
	if store.finishedStatus != "skipped" || deleter.generationCalls != 0 || store.retried {
		t.Fatalf("protected active generation outcome: status=%q calls=%d retried=%v", store.finishedStatus, deleter.generationCalls, store.retried)
	}
}

func TestVectorGCWorkerRetriesDeleteFailure(t *testing.T) {
	store, deleter := &fakeVectorGCStore{}, &fakeVectorGCDeleter{err: errors.New("milvus unavailable")}
	worker := newVectorGCWorker(store, fakeVectorGCSafety{generation: true}, deleter)
	before := time.Now()
	worker.run(&repository.VectorGCTask{TenantID: "t", DocID: "d", TargetKey: "generation:1", TargetKind: repository.VectorGCTargetGeneration, TargetGeneration: 1, AttemptCount: 3, MaxAttempts: 8}, "token")
	if !store.retried || deleter.generationCalls != 1 || store.finishedStatus != "" {
		t.Fatalf("failure outcome: retried=%v calls=%d status=%q", store.retried, deleter.generationCalls, store.finishedStatus)
	}
	if store.retryAt.Before(before.Add(4*time.Second)) || store.retryAt.After(before.Add(6*time.Second)) {
		t.Fatalf("retry delay = %s, want about 4s", store.retryAt.Sub(before))
	}
}

func TestVectorGCWorkerDeletesOnlyDocumentAllTarget(t *testing.T) {
	store, deleter := &fakeVectorGCStore{}, &fakeVectorGCDeleter{}
	worker := newVectorGCWorker(store, fakeVectorGCSafety{documentAll: true}, deleter)
	worker.run(&repository.VectorGCTask{TenantID: "tenant-a", DocID: "doc-a", TargetKey: "document:all", TargetKind: repository.VectorGCTargetDocumentAll, AttemptCount: 1, MaxAttempts: 8}, "token")
	if store.finishedStatus != "succeeded" || deleter.documentAllCalls != 1 || deleter.generationCalls != 0 || deleter.legacyCalls != 0 {
		t.Fatalf("document_all outcome: status=%q calls=%#v", store.finishedStatus, deleter)
	}
}

func TestVectorGCRetryDelayCaps(t *testing.T) {
	if got := vectorGCRetryDelay(1); got != time.Second {
		t.Fatalf("attempt 1 delay = %s", got)
	}
	if got := vectorGCRetryDelay(20); got != 5*time.Minute {
		t.Fatalf("capped delay = %s", got)
	}
}
