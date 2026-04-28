package eolevaluator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
)

func TestEvaluator_PyPI_InfoYanked(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		// Distinguish project (classifier) endpoint vs version endpoint.
		if !strings.HasSuffix(r.URL.Path, "/2.30.0/json") {
			// /pypi/{name}/json — return non-inactive classifier so applyPyPIClassifier doesn't fire.
			_, _ = w.Write([]byte(`{"info":{"name":"requests","summary":"","description":"","classifiers":["Development Status :: 5 - Production/Stable"]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"info":{"name":"requests","version":"2.30.0","yanked":true,"yanked_reason":"CVE-2024-XXXX"},"urls":[{"yanked":true}]}`))
	}))
	defer srv.Close()

	pc := pypi.NewClient()
	pc.SetBaseURL(srv.URL)
	pc.SetCacheTTL(0)

	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetPyPIClient(pc)

	analysis := &domain.Analysis{
		Package: &domain.Package{PURL: "pkg:pypi/requests@2.30.0", Ecosystem: "pypi"},
		ReleaseInfo: &domain.ReleaseInfo{
			StableVersion: &domain.VersionDetail{Version: "2.30.0"},
		},
	}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if err != nil {
		t.Fatalf("EvaluateBatch failed: %v", err)
	}
	st := out["k"]
	if st.State != domain.EOLEndOfLife {
		t.Fatalf("expected EOLEndOfLife on yanked PyPI version, got %v", st.State)
	}
	if capturedPath != "/pypi/requests/2.30.0/json" {
		t.Errorf("unexpected last path: got %q", capturedPath)
	}
	found := false
	for _, evd := range st.Evidences {
		if evd.Source == "PyPI" && evd.Confidence == 0.95 && strings.Contains(evd.Summary, "CVE-2024-XXXX") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected pypi yanked evidence with reason, got %#v", st.Evidences)
	}
}

func TestEvaluator_PyPI_AllUrlsYanked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/1.0.0/json") {
			_, _ = w.Write([]byte(`{"info":{"name":"pkg","summary":"","description":"","classifiers":["Development Status :: 5 - Production/Stable"]}}`))
			return
		}
		// info.yanked=false, but every distribution URL is yanked.
		_, _ = w.Write([]byte(`{"info":{"name":"pkg","version":"1.0.0","yanked":false},"urls":[{"yanked":true},{"yanked":true}]}`))
	}))
	defer srv.Close()

	pc := pypi.NewClient()
	pc.SetBaseURL(srv.URL)
	pc.SetCacheTTL(0)

	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetPyPIClient(pc)

	analysis := &domain.Analysis{
		Package:     &domain.Package{PURL: "pkg:pypi/pkg@1.0.0", Ecosystem: "pypi"},
		ReleaseInfo: &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: "1.0.0"}},
	}
	out, _ := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if out["k"].State != domain.EOLEndOfLife {
		t.Fatalf("expected EOLEndOfLife when all urls yanked, got %v", out["k"].State)
	}
}

func TestEvaluator_PyPI_NotYanked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/1.0.0/json") {
			_, _ = w.Write([]byte(`{"info":{"name":"pkg","summary":"","description":"","classifiers":["Development Status :: 5 - Production/Stable"]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"info":{"name":"pkg","version":"1.0.0","yanked":false},"urls":[{"yanked":false}]}`))
	}))
	defer srv.Close()

	pc := pypi.NewClient()
	pc.SetBaseURL(srv.URL)
	pc.SetCacheTTL(0)

	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetPyPIClient(pc)

	analysis := &domain.Analysis{
		Package:     &domain.Package{PURL: "pkg:pypi/pkg@1.0.0", Ecosystem: "pypi"},
		ReleaseInfo: &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: "1.0.0"}},
	}
	out, _ := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if out["k"].State == domain.EOLEndOfLife {
		t.Fatalf("expected non-EOL on healthy PyPI version, got EOL")
	}
}
