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

// TestEvaluator_PyPI_Yanked_RequestedVersionOnly pins the version-resolution
// contract of the PyPI yanked rule: only the version in OriginalPURL decides.
// Neither ReleaseInfo.StableVersion nor a version uzomuzo itself wrote into
// Package.PURL / EffectivePURL may promote to EOL. See ADR-0021.
//
// Fixture mirrors pkg:pypi/pydantic-extra-types as observed on 2026-08-31: 2.11.0
// and 2.11.1 are not yanked, 2.11.2 is fully yanked, and deps.dev reports the
// yanked 2.11.2 as its default release — so StableVersion carries 2.11.2.
func TestEvaluator_PyPI_Yanked_RequestedVersionOnly(t *testing.T) {
	const projectPath = "/pypi/pydantic-extra-types/json"

	tests := []struct {
		name string
		// originalPURL is what the caller asked about (Analysis.OriginalPURL).
		originalPURL string
		// analyzedPURL is the coordinate uzomuzo analyzed (Package.PURL and
		// EffectivePURL). On the GitHub URL entry path it carries a version
		// uzomuzo selected, not one the caller pinned.
		analyzedPURL string
		// stableVersion is what deps.dev selected; "" means ReleaseInfo is nil.
		stableVersion string
		wantState     domain.EOLState
		// wantNoVersionQuery asserts the rule never hit a version endpoint at all.
		wantNoVersionQuery bool
	}{
		{
			// The GitHub URL entry path: `uzomuzo scan
			// https://github.com/pydantic/pydantic-extra-types` resolves the base
			// PURL, then deps.dev hands back the yanked 2.11.2 as the default
			// release. The caller pinned nothing, so the yank must not fire.
			name:               "GitHub URL path: synthesized yanked version is not a caller pin",
			originalPURL:       "pkg:pypi/pydantic-extra-types",
			analyzedPURL:       "pkg:pypi/pydantic-extra-types@2.11.2",
			stableVersion:      "2.11.2",
			wantState:          domain.EOLNotEOL,
			wantNoVersionQuery: true,
		},
		{
			name:               "unversioned, stable is yanked 2.11.2 — no EOL, no version query",
			originalPURL:       "pkg:pypi/pydantic-extra-types",
			analyzedPURL:       "pkg:pypi/pydantic-extra-types",
			stableVersion:      "2.11.2",
			wantState:          domain.EOLNotEOL,
			wantNoVersionQuery: true,
		},
		{
			name:               "unversioned, no ReleaseInfo — no EOL, no version query",
			originalPURL:       "pkg:pypi/pydantic-extra-types",
			analyzedPURL:       "pkg:pypi/pydantic-extra-types",
			stableVersion:      "",
			wantState:          domain.EOLNotEOL,
			wantNoVersionQuery: true,
		},
		{
			name:               "empty OriginalPURL is a no-op even when Package.PURL is yanked",
			originalPURL:       "",
			analyzedPURL:       "pkg:pypi/pydantic-extra-types@2.11.2",
			stableVersion:      "2.11.2",
			wantState:          domain.EOLNotEOL,
			wantNoVersionQuery: true,
		},
		{
			name:          "pinned to non-yanked 2.11.0 while stable is yanked — no EOL",
			originalPURL:  "pkg:pypi/pydantic-extra-types@2.11.0",
			analyzedPURL:  "pkg:pypi/pydantic-extra-types@2.11.0",
			stableVersion: "2.11.2",
			wantState:     domain.EOLNotEOL,
		},
		{
			name:          "pinned to yanked 2.11.2 — still EOL",
			originalPURL:  "pkg:pypi/pydantic-extra-types@2.11.2",
			analyzedPURL:  "pkg:pypi/pydantic-extra-types@2.11.2",
			stableVersion: "2.11.2",
			wantState:     domain.EOLEndOfLife,
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
				switch r.URL.Path {
				case "/pypi/pydantic-extra-types/2.11.2/json":
					_, _ = w.Write([]byte(`{"info":{"name":"pydantic-extra-types","version":"2.11.2","yanked":true,` +
						`"yanked_reason":"multiple feature mistakenly merged before release. no known security risks."},` +
						`"urls":[{"yanked":true},{"yanked":true}]}`))
				case "/pypi/pydantic-extra-types/2.11.0/json":
					_, _ = w.Write([]byte(`{"info":{"name":"pydantic-extra-types","version":"2.11.0","yanked":false},"urls":[{"yanked":false}]}`))
				case projectPath:
					// Non-inactive classifier so applyPyPIClassifier does not fire.
					_, _ = w.Write([]byte(`{"info":{"name":"pydantic-extra-types","summary":"","description":"",` +
						`"classifiers":["Development Status :: 5 - Production/Stable"]}}`))
				default:
					// Any other path is a request this test did not intend to serve;
					// 404 so an unexpected fetch cannot silently look like a success.
					w.WriteHeader(http.StatusNotFound)
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
				OriginalPURL:  tt.originalPURL,
				EffectivePURL: tt.analyzedPURL,
				Package:       &domain.Package{PURL: tt.analyzedPURL, Ecosystem: "pypi"},
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
			if st.State != tt.wantState {
				t.Fatalf("EOL state: got %v, want %v; evidences=%#v", st.State, tt.wantState, st.Evidences)
			}

			mu.Lock()
			paths := append([]string(nil), requestedPaths...)
			mu.Unlock()

			if tt.wantNoVersionQuery {
				// The project endpoint is expected (the inactive-classifier rule uses it).
				// Any other path means a version endpoint was hit, whichever version it names.
				for _, p := range paths {
					if p != projectPath {
						t.Errorf("no caller-pinned version: must query only %s; got path %q", projectPath, p)
					}
				}
			}
			if tt.wantState != domain.EOLEndOfLife {
				for _, evd := range st.Evidences {
					if strings.Contains(evd.Summary, "yanked") {
						t.Errorf("unexpected yanked evidence on non-EOL case: %#v", evd)
					}
				}
			}
		})
	}
}

// TestEvaluator_Cargo_Yanked_RequestedVersionOnly is the crates.io half of the same
// contract — both ecosystems share applyRegistryYanked. See ADR-0021.
func TestEvaluator_Cargo_Yanked_RequestedVersionOnly(t *testing.T) {
	const versionPath = "/api/v1/crates/sha-1/0.10.1"

	tests := []struct {
		name               string
		originalPURL       string
		analyzedPURL       string
		stableVersion      string
		wantState          domain.EOLState
		wantNoVersionQuery bool
	}{
		{
			name:               "synthesized yanked version is not a caller pin",
			originalPURL:       "pkg:cargo/sha-1",
			analyzedPURL:       "pkg:cargo/sha-1@0.10.1",
			stableVersion:      "0.10.1",
			wantState:          domain.EOLNotEOL,
			wantNoVersionQuery: true,
		},
		{
			name:               "unversioned, stable is yanked — no EOL, no version query",
			originalPURL:       "pkg:cargo/sha-1",
			analyzedPURL:       "pkg:cargo/sha-1",
			stableVersion:      "0.10.1",
			wantState:          domain.EOLNotEOL,
			wantNoVersionQuery: true,
		},
		{
			name:          "pinned to yanked version — still EOL",
			originalPURL:  "pkg:cargo/sha-1@0.10.1",
			analyzedPURL:  "pkg:cargo/sha-1@0.10.1",
			stableVersion: "0.10.1",
			wantState:     domain.EOLEndOfLife,
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
				if r.URL.Path != versionPath {
					w.WriteHeader(http.StatusNotFound)
					return
				}
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
				OriginalPURL:  tt.originalPURL,
				EffectivePURL: tt.analyzedPURL,
				Package:       &domain.Package{PURL: tt.analyzedPURL, Ecosystem: "cargo"},
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
			if st.State != tt.wantState {
				t.Fatalf("EOL state: got %v, want %v; evidences=%#v", st.State, tt.wantState, st.Evidences)
			}

			mu.Lock()
			paths := append([]string(nil), requestedPaths...)
			mu.Unlock()

			if tt.wantNoVersionQuery && len(paths) > 0 {
				t.Errorf("no caller-pinned version: must not query crates.io at all; got paths=%v", paths)
			}
		})
	}
}

// Test_applyRegistryYanked_NoRequestedVersion pins the helper's own return value
// (not just the resulting state) for every input shape that carries no
// caller-requested version: the rule must report "did not fire" and must never
// invoke fetch. See ADR-0021.
func Test_applyRegistryYanked_NoRequestedVersion(t *testing.T) {
	tests := []struct {
		name         string
		originalPURL string
	}{
		{name: "unversioned OriginalPURL", originalPURL: "pkg:pypi/pydantic-extra-types"},
		{name: "empty OriginalPURL", originalPURL: ""},
		{name: "OriginalPURL is a GitHub URL", originalPURL: "https://github.com/pydantic/pydantic-extra-types"},
		{name: "OriginalPURL of another ecosystem", originalPURL: "pkg:cargo/sha-1@0.10.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ev := NewEvaluator(nil)
			analysis := &domain.Analysis{
				OriginalPURL:  tt.originalPURL,
				EffectivePURL: "pkg:pypi/pydantic-extra-types@2.11.2",
				Package:       &domain.Package{PURL: "pkg:pypi/pydantic-extra-types@2.11.2", Ecosystem: "pypi"},
				ReleaseInfo: &domain.ReleaseInfo{
					StableVersion: &domain.VersionDetail{Version: "2.11.2"},
				},
			}
			status := &domain.EOLStatus{State: domain.EOLUnknown}

			fetch := func(ctx context.Context, name, version string) (bool, string, string, bool, error) {
				t.Errorf("fetch must not be called; got name=%q version=%q", name, version)
				return true, "yanked", "", true, nil
			}

			got := ev.applyRegistryYanked(context.Background(), analysis, status, "pypi", "PyPI", true,
				fetch, 1.0, "test_event")
			if got {
				t.Errorf("applyRegistryYanked returned true; want false")
			}
			if status.State != domain.EOLUnknown {
				t.Errorf("status.State mutated to %v; want %v", status.State, domain.EOLUnknown)
			}
			if len(status.Evidences) != 0 {
				t.Errorf("evidence appended: %#v", status.Evidences)
			}
		})
	}
}
