package uzomuzo_test

import (
	"context"
	"testing"

	uzomuzo "github.com/future-architect/uzomuzo-oss/pkg/uzomuzo"
)

// fakeEvalService is a no-op EvaluationService used to exercise the
// NewEvaluatorFromService nil/typed-nil guard.
type fakeEvalService struct{}

func (f *fakeEvalService) ProcessBatchPURLs(context.Context, []string) (map[string]*uzomuzo.Analysis, error) {
	return nil, nil
}

func (f *fakeEvalService) ProcessBatchGitHubURLs(context.Context, []string) (map[string]*uzomuzo.Analysis, error) {
	return nil, nil
}

func (f *fakeEvalService) WriteScoreCardCSV(map[string]*uzomuzo.Analysis, string) error {
	return nil
}

func TestNewEvaluatorFromService_NilGuard(t *testing.T) {
	t.Run("untyped nil panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for untyped-nil service, got none")
			}
		}()
		_ = uzomuzo.NewEvaluatorFromService(nil)
	})

	t.Run("typed-nil pointer panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for typed-nil service, got none")
			}
		}()
		var svc *fakeEvalService // typed nil: non-nil interface, nil pointer
		_ = uzomuzo.NewEvaluatorFromService(svc)
	})

	t.Run("valid service does not panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("did not expect panic for valid service, got %v", r)
			}
		}()
		ev := uzomuzo.NewEvaluatorFromService(&fakeEvalService{})
		if ev == nil {
			t.Fatal("expected non-nil Evaluator for valid service")
		}
	})
}
