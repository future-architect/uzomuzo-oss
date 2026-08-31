package eolevaluator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/crates"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
)

// TestEvaluator_PyPI_Yanked_UnversionedPURL pins the version-resolution contract of
// the PyPI yanked rule: only the PURL version decides, never StableVersion.
// See ADR-0021.
//
// Fixture mirrors pkg:pypi/pydantic-extra-types as observed on 2026-08-31: 2.11.0
// and 2.11.1 are not yanked, 2.11.2 is fully yanked, and deps.dev reports the
// yanked 2.11.2 as its default release — so StableVersion carries 2.11.2.
func TestEvaluator_PyPI_Yanked_UnversionedPURL(t *testing.T) {
	tests := []struct {
		name string
		// purl is the analysis PURL, versioned or not.
		purl string
		// stableVersion is what deps.dev selected; "" means ReleaseInfo is nil.
		stableVersion string
		wantEOL       bool
		// wantNoVersionQuery asserts the rule never hit a version endpoint at all.
		wantNoVersionQuery bool
	}{
		{
			name:               "unversioned PURL, stable is yanked 2.11.2 — no EOL, no version query",
			purl:               "pkg:pypi/pydantic-extra-types",
			stableVersion:      "2.11.2",
			wantEOL:            false,
			wantNoVersionQuery: true,
		},
		{
			name:               "unversioned PURL, no ReleaseInfo — no EOL, no version query",
			purl:               "pkg:pypi/pydantic-extra-types",
			stableVersion:      "",
			wantEOL:            false,
			wantNoVersionQuery: true,
		},
		{
			name:          "PURL pinned to non-yanked 2.11.0 while stable is yanked — no EOL",
			purl:          "pkg:pypi/pydantic-extra-types@2.11.0",
			stableVersion: "2.11.2",
			wantEOL:       false,
		},
		{
			name:          "PURL pinned to non-yanked 2.11.1 while stable is yanked — no EOL",
			purl:          "pkg:pypi/pydantic-extra-types@2.11.1",
			stableVersion: "2.11.2",
			wantEOL:       false,
		},
		{
			name:          "PURL pinned to yanked 2.11.2 — still EOL",
			purl:          "pkg:pypi/pydantic-extra-types@2.11.2",
			stableVersion: "2.11.2",
			wantEOL:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu             sync.Mutex
				requestedPaths []string
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requestedPaths = append(requestedPaths, r.URL.Path)
				mu.Unlock()
				switch {
				case strings.HasSuffix(r.URL.Path, "/2.11.2/json"):
					_, _ = w.Write([]byte(`{"info":{"name":"pydantic-extra-types","version":"2.11.2","yanked":true,` +
						`"yanked_reason":"multiple feature mistakenly merged before release. no known security risks."},` +
						`"urls":[{"yanked":true},{"yanked":true}]}`))
				case strings.HasSuffix(r.URL.Path, "/2.11.1/json"):
					_, _ = w.Write([]byte(`{"info":{"name":"pydantic-extra-types","version":"2.11.1","yanked":false},"urls":[{"yanked":false}]}`))
				case strings.HasSuffix(r.URL.Path, "/2.11.0/json"):
					_, _ = w.Write([]byte(`{"info":{"name":"pydantic-extra-types","version":"2.11.0","yanked":false},"urls":[{"yanked":false}]}`))
				default:
					// Project endpoint — non-inactive classifier so applyPyPIClassifier does not fire.
					_, _ = w.Write([]byte(`{"info":{"name":"pydantic-extra-types","summary":"","description":"",` +
						`"classifiers":["Development Status :: 5 - Production/Stable"]}}`))
				}
			}))
			defer srv.Close()

			pc := pypi.NewClient()
			pc.SetBaseURL(srv.URL)
			pc.SetCacheTTL(0)

			ev := NewEvaluator(nil)
			ev.SetMaxWorkers(1)
			ev.SetPyPIClient(pc)

			analysis := &domain.Analysis{
				Package: &domain.Package{PURL: tt.purl, Ecosystem: "pypi"},
			}
			if tt.stableVersion != "" {
				analysis.ReleaseInfo = &domain.ReleaseInfo{
					StableVersion: &domain.VersionDetail{Version: tt.stableVersion},
				}
			}

			out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
			if err != nil {
				t.Fatalf("EvaluateBatch failed: %v", err)
			}
			st := out["k"]
			gotEOL := st.State == domain.EOLEndOfLife
			if gotEOL != tt.wantEOL {
				t.Fatalf("EOL state: got %v (want EOL=%v); evidences=%#v", st.State, tt.wantEOL, st.Evidences)
			}

			mu.Lock()
			paths := append([]string(nil), requestedPaths...)
			mu.Unlock()

			if tt.wantNoVersionQuery {
				for _, p := range paths {
					if strings.HasSuffix(p, "/2.11.2/json") {
						t.Errorf("unversioned PURL must not query StableVersion 2.11.2; got path %q", p)
					}
				}
			}
			if !tt.wantEOL {
				for _, evd := range st.Evidences {
					if strings.Contains(evd.Summary, "yanked") {
						t.Errorf("unexpected yanked evidence on non-EOL case: %#v", evd)
					}
				}
			}
		})
	}
}

// TestEvaluator_Cargo_Yanked_UnversionedPURL is the crates.io half of the same
// contract — both ecosystems share applyRegistryYanked. See ADR-0021.
func TestEvaluator_Cargo_Yanked_UnversionedPURL(t *testing.T) {
	tests := []struct {
		name               string
		purl               string
		stableVersion      string
		wantEOL            bool
		wantNoVersionQuery bool
	}{
		{
			name:               "unversioned PURL, stable is yanked — no EOL, no version query",
			purl:               "pkg:cargo/sha-1",
			stableVersion:      "0.10.1",
			wantEOL:            false,
			wantNoVersionQuery: true,
		},
		{
			name:          "PURL pinned to yanked version — still EOL",
			purl:          "pkg:cargo/sha-1@0.10.1",
			stableVersion: "0.10.1",
			wantEOL:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu             sync.Mutex
				requestedPaths []string
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requestedPaths = append(requestedPaths, r.URL.Path)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"version":{"crate":"sha-1","num":"0.10.1","yanked":true}}`))
			}))
			defer srv.Close()

			cc := crates.NewClient()
			cc.SetBaseURL(srv.URL)
			cc.SetCacheTTL(0)

			ev := NewEvaluator(nil)
			ev.SetMaxWorkers(1)
			ev.SetCratesClient(cc)

			analysis := &domain.Analysis{
				Package: &domain.Package{PURL: tt.purl, Ecosystem: "cargo"},
			}
			if tt.stableVersion != "" {
				analysis.ReleaseInfo = &domain.ReleaseInfo{
					StableVersion: &domain.VersionDetail{Version: tt.stableVersion},
				}
			}

			out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
			if err != nil {
				t.Fatalf("EvaluateBatch failed: %v", err)
			}
			st := out["k"]
			gotEOL := st.State == domain.EOLEndOfLife
			if gotEOL != tt.wantEOL {
				t.Fatalf("EOL state: got %v (want EOL=%v); evidences=%#v", st.State, tt.wantEOL, st.Evidences)
			}

			mu.Lock()
			paths := append([]string(nil), requestedPaths...)
			mu.Unlock()

			if tt.wantNoVersionQuery && len(paths) > 0 {
				t.Errorf("unversioned PURL must not query crates.io at all; got paths=%v", paths)
			}
		})
	}
}
