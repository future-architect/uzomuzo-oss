package osv

import (
	"encoding/json"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

func TestOSVPackageFor(t *testing.T) {
	for _, tt := range []struct {
		purl, eco, name string
		ok              bool
	}{
		{"pkg:npm/%40solana/web3.js@1.95.6", "npm", "@solana/web3.js", true},
		{"pkg:npm/%40other/web3.js@1.95.6", "npm", "@other/web3.js", true},
		{"pkg:pypi/Friendly_Bard@1", "PyPI", "friendly-bard", true},
		{"pkg:pypi/Friendly..Bard@1", "PyPI", "friendly-bard", true},
		{"pkg:maven/org.example/demo@1", "Maven", "org.example:demo", true},
		{"pkg:golang/github.com/example/mod/v2@v2.1.0", "Go", "github.com/example/mod/v2", true},
		{"pkg:cargo/atty@0.2.14", "crates.io", "atty", true},
		{"pkg:gem/rails@7.0", "RubyGems", "rails", true},
		{"pkg:composer/acme/widget@1.0", "Packagist", "acme/widget", true},
		{"pkg:github/acme/widget", "", "", false},
	} {
		t.Run(tt.purl, func(t *testing.T) {
			parsed, err := purl.NewParser().Parse(tt.purl)
			if err != nil {
				t.Fatal(err)
			}
			eco, name, ok := OSVPackageFor(parsed)
			if eco != tt.eco || name != tt.name || ok != tt.ok {
				t.Fatalf("got %q %q %v", eco, name, ok)
			}
		})
	}
}

func TestTopLevelCWEIDs(t *testing.T) {
	var v wireVuln
	if err := json.Unmarshal([]byte(`{"id":"GHSA-abc","database_specific":{"cwe_ids":["CWE-506","CWE-94","CWE-no","CWE-506\n"]}}`), &v); err != nil {
		t.Fatal(err)
	}
	rec, ok := toRecord(&v)
	if !ok || len(rec.CWEIDs) != 2 || rec.CWEIDs[0] != "CWE-506" || rec.CWEIDs[1] != "CWE-94" {
		t.Fatalf("record: %+v", rec)
	}
}
