package actions_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/actions"
)

func TestLookup_KnownDeprecations(t *testing.T) {
	tests := []struct {
		name  string
		owner string
		repo  string
		pin   string
		want  bool
	}{
		{"upload-artifact v3 is EOL", "actions", "upload-artifact", "v3", true},
		{"upload-artifact v3.0.0 matches major", "actions", "upload-artifact", "v3.0.0", true},
		{"upload-artifact v4 is current", "actions", "upload-artifact", "v4", false},
		{"download-artifact v2 is EOL", "actions", "download-artifact", "v2", true},
		{"checkout v2 is EOL", "actions", "checkout", "v2", true},
		{"checkout v3 not in initial seed", "actions", "checkout", "v3", false},
		{"checkout v4 is current", "actions", "checkout", "v4", false},
		{"case-insensitive owner", "Actions", "checkout", "v2", true},
		{"case-insensitive repo", "actions", "CHECKOUT", "v2", true},
		{"unknown repo", "actions", "unknown-action", "v1", false},
		{"SHA pin never matches", "actions", "checkout", "de0fac2e4500dabe0009e67214ff5f5447ce83dd", false},
		{"branch pin never matches", "actions", "checkout", "main", false},
		{"empty pin never matches", "actions", "checkout", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := actions.Lookup(tt.owner, tt.repo, tt.pin)
			if got != tt.want {
				t.Errorf("Lookup(%q, %q, %q) = %v, want %v", tt.owner, tt.repo, tt.pin, got, tt.want)
			}
		})
	}
}

func TestLookup_ReturnsCorrectEntry(t *testing.T) {
	entry, ok := actions.Lookup("actions", "upload-artifact", "v3")
	if !ok {
		t.Fatal("expected match for upload-artifact@v3")
	}
	if entry.SuggestedVersion != "v4" {
		t.Errorf("SuggestedVersion = %q, want v4", entry.SuggestedVersion)
	}
	if entry.Reason == "" {
		t.Error("Reason must be non-empty")
	}
	if entry.EOLDate == "" {
		t.Error("EOLDate must be non-empty for this entry")
	}
	if entry.ReferenceURL == "" {
		t.Error("ReferenceURL must be non-empty")
	}
}

// TestCatalogInvariants guards against common authoring errors in future entries:
// the suggested upgrade must not itself be listed as deprecated, every reference
// URL must be a valid HTTP(S) URL, and each reason string should mention a date
// so the PR body carries the authority the design intends.
func TestCatalogInvariants(t *testing.T) {
	for _, e := range actions.AllEntries() {
		name := e.Owner + "/" + e.Repo
		t.Run(name, func(t *testing.T) {
			if e.Owner == "" || e.Repo == "" {
				t.Errorf("%s: Owner and Repo must be set", name)
			}
			if len(e.DeprecatedMajors) == 0 {
				t.Errorf("%s: DeprecatedMajors must not be empty", name)
			}
			if e.SuggestedVersion == "" {
				t.Errorf("%s: SuggestedVersion must be set", name)
			}
			for _, m := range e.DeprecatedMajors {
				if m == e.SuggestedVersion {
					t.Errorf("%s: SuggestedVersion %q is itself listed as deprecated", name, m)
				}
			}
			if e.ReferenceURL == "" {
				t.Errorf("%s: ReferenceURL must be set (reputational guarantee)", name)
			} else {
				u, err := url.Parse(e.ReferenceURL)
				if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
					t.Errorf("%s: ReferenceURL %q is not a valid http(s) URL", name, e.ReferenceURL)
				}
			}
			if e.Reason == "" {
				t.Errorf("%s: Reason must be set (shown in PR bodies)", name)
			}
			// Reason should contain the EOL date when one is known, so PR
			// bodies carry the "EOL since YYYY-MM-DD" authority the design intends.
			if e.EOLDate != "" && !strings.Contains(e.Reason, e.EOLDate) {
				t.Errorf("%s: Reason should mention EOLDate %q, got %q", name, e.EOLDate, e.Reason)
			}
		})
	}
}
