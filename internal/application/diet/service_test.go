package diet

import (
	"context"
	"reflect"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domaindiet "github.com/future-architect/uzomuzo-oss/internal/domain/diet"
)

// --- Stub implementations ---

type stubGraphAnalyzer struct {
	result *domaindiet.GraphResult
	err    error
}

func (s *stubGraphAnalyzer) AnalyzeGraph(_ context.Context, _ []byte) (*domaindiet.GraphResult, error) {
	return s.result, s.err
}

type stubSourceAnalyzer struct {
	result map[string]*domaindiet.CouplingAnalysis
	err    error
}

func (s *stubSourceAnalyzer) AnalyzeCoupling(_ context.Context, _ string, _ map[string][]string) (map[string]*domaindiet.CouplingAnalysis, error) {
	return s.result, s.err
}

// --- Tests ---

func TestParsePURLParts(t *testing.T) {
	tests := []struct {
		purl     string
		wantName string
		wantEco  string
		wantVer  string
	}{
		{
			purl:     "pkg:golang/github.com/gin-gonic/gin@v1.10.0",
			wantName: "github.com/gin-gonic/gin",
			wantEco:  "golang",
			wantVer:  "v1.10.0",
		},
		{
			purl:     "pkg:npm/%40angular/core@16.0.0",
			wantName: "@angular/core",
			wantEco:  "npm",
			wantVer:  "16.0.0",
		},
		{
			purl:     "pkg:pypi/requests@2.31.0",
			wantName: "requests",
			wantEco:  "pypi",
			wantVer:  "2.31.0",
		},
		{
			purl:     "invalid-purl",
			wantName: "invalid-purl",
			wantEco:  "",
			wantVer:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.purl, func(t *testing.T) {
			name, eco, ver := parsePURLParts(tt.purl)
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if eco != tt.wantEco {
				t.Errorf("ecosystem = %q, want %q", eco, tt.wantEco)
			}
			if ver != tt.wantVer {
				t.Errorf("version = %q, want %q", ver, tt.wantVer)
			}
		})
	}
}

func TestBuildImportPaths(t *testing.T) {
	purls := []string{
		"pkg:golang/github.com/stretchr/testify@v1.9.0",
		"pkg:npm/%40types/node@20.0.0",
		"pkg:pypi/flask@3.0.0",
		"pkg:maven/org.apache.commons/commons-lang3@3.14.0",
	}
	result := buildImportPaths(purls)

	// Non-Maven, non-PyPI ecosystems still return a single import path.
	singleExpectations := map[string]string{
		"pkg:golang/github.com/stretchr/testify@v1.9.0": "github.com/stretchr/testify",
		"pkg:npm/%40types/node@20.0.0":                  "@types/node",
	}
	for purl, wantImport := range singleExpectations {
		got, ok := result[purl]
		if !ok {
			t.Errorf("missing import path for %s", purl)
			continue
		}
		if len(got) != 1 || got[0] != wantImport {
			t.Errorf("import path for %s = %v, want [%s]", purl, got, wantImport)
		}
	}

	// PyPI without hyphens returns a single import path.
	pypiPURL := "pkg:pypi/flask@3.0.0"
	gotPyPI := result[pypiPURL]
	if len(gotPyPI) != 1 || gotPyPI[0] != "flask" {
		t.Errorf("import path for %s = %v, want [flask]", pypiPURL, gotPyPI)
	}

	// Maven returns groupId only when artifactId contains hyphens (invalid in Java package names).
	mavenPURL := "pkg:maven/org.apache.commons/commons-lang3@3.14.0"
	got := result[mavenPURL]
	want := []string{"org.apache.commons"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("import paths for %s = %v, want %v", mavenPURL, got, want)
	}
}

func TestBuildMavenImportPaths(t *testing.T) {
	tests := []struct {
		name string
		purl string
		want []string
	}{
		{
			name: "standard groupId matches package",
			purl: "pkg:maven/org.apache.commons/commons-lang3@3.14.0",
			want: []string{"org.apache.commons"},
		},
		{
			name: "override: cglib groupId differs from package",
			purl: "pkg:maven/cglib/cglib@3.3.0",
			want: []string{"net.sf.cglib", "cglib"},
		},
		{
			name: "override: gson groupId differs from package",
			purl: "pkg:maven/com.google.code.gson/gson@2.10",
			want: []string{"com.google.gson", "com.google.code.gson", "com.google.code.gson.gson"},
		},
		{
			name: "groupId.artifactId emitted when artifactId is Java-safe",
			purl: "pkg:maven/com.example/utils@1.0.0",
			want: []string{"com.example", "com.example.utils"},
		},
		{
			name: "override: junit has two package prefixes",
			purl: "pkg:maven/junit/junit@4.13.2",
			want: []string{"junit", "org.junit"},
		},
		{
			name: "digit-starting artifactId is not Java-safe",
			purl: "pkg:maven/com.example/3scale@1.0.0",
			want: []string{"com.example"},
		},
		{
			name: "hyphenated namespace skipped, override used",
			purl: "pkg:maven/commons-io/commons-io@2.15.0",
			want: []string{"org.apache.commons.io"},
		},
		{
			name: "no namespace falls back to artifactId",
			purl: "pkg:maven/somelib@1.0.0",
			want: []string{"somelib"},
		},
		{
			name: "case-insensitive override lookup",
			purl: "pkg:maven/Cglib/Cglib@3.3.0",
			want: []string{"net.sf.cglib", "Cglib"},
		},
		{
			name: "mixed-case namespace/name equality skips groupId.artifactId",
			purl: "pkg:maven/Cglib/cglib@3.3.0",
			want: []string{"net.sf.cglib", "Cglib"},
		},
		{
			name: "invalid fallback artifactId is skipped",
			purl: "pkg:maven/3scale-client@1.0.0",
			want: nil,
		},
		{
			name: "hyphenated namespace without override skips groupId.artifactId",
			purl: "pkg:maven/my-company/mylib@1.0.0",
			want: []string{"mylib"},
		},
		{
			name: "override: jackson-annotations groupId differs from package",
			purl: "pkg:maven/com.fasterxml.jackson.core/jackson-annotations@2.17.0",
			want: []string{"com.fasterxml.jackson.annotation", "com.fasterxml.jackson.core"},
		},
		{
			name: "override: jackson-databind groupId differs from package",
			purl: "pkg:maven/com.fasterxml.jackson.core/jackson-databind@2.17.0",
			want: []string{"com.fasterxml.jackson.databind", "com.fasterxml.jackson.core"},
		},
		{
			name: "override: jackson-dataformat-yaml",
			purl: "pkg:maven/com.fasterxml.jackson.dataformat/jackson-dataformat-yaml@2.17.0",
			want: []string{"com.fasterxml.jackson.dataformat.yaml", "com.fasterxml.jackson.dataformat"},
		},
		{
			name: "override: jackson-dataformat-xml",
			purl: "pkg:maven/com.fasterxml.jackson.dataformat/jackson-dataformat-xml@2.17.0",
			want: []string{"com.fasterxml.jackson.dataformat.xml", "com.fasterxml.jackson.dataformat"},
		},
		{
			name: "override: jackson-dataformat-csv",
			purl: "pkg:maven/com.fasterxml.jackson.dataformat/jackson-dataformat-csv@2.17.0",
			want: []string{"com.fasterxml.jackson.dataformat.csv", "com.fasterxml.jackson.dataformat"},
		},
		{
			name: "override: jackson-datatype-jsr310",
			purl: "pkg:maven/com.fasterxml.jackson.datatype/jackson-datatype-jsr310@2.17.0",
			want: []string{"com.fasterxml.jackson.datatype.jsr310", "com.fasterxml.jackson.datatype"},
		},
		{
			name: "override: jackson-module-kotlin",
			purl: "pkg:maven/com.fasterxml.jackson.module/jackson-module-kotlin@2.17.0",
			want: []string{"com.fasterxml.jackson.module.kotlin", "com.fasterxml.jackson.module"},
		},
		{
			name: "override: javax.inject groupId matches package",
			purl: "pkg:maven/javax.inject/javax.inject@1",
			want: []string{"javax.inject"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildImportPaths([]string{tt.purl})
			got := result[tt.purl]
			if tt.want == nil {
				if got != nil {
					t.Errorf("buildImportPaths(%s) = %v, want no entry", tt.purl, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("missing import paths for %s", tt.purl)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildImportPaths(%s) = %v, want %v", tt.purl, got, tt.want)
			}
		})
	}
}

func TestBuildPyPIImportPaths(t *testing.T) {
	tests := []struct {
		name string
		purl string
		want []string
	}{
		{
			name: "python-prefix stripped: python-multipart",
			purl: "pkg:pypi/python-multipart@0.0.6",
			want: []string{"python_multipart", "multipart"},
		},
		{
			name: "simple hyphenated name: email-validator",
			purl: "pkg:pypi/email-validator@2.0.0",
			want: []string{"email_validator"},
		},
		{
			name: "case normalization: PyYAML",
			purl: "pkg:pypi/PyYAML@6.0.1",
			want: []string{"pyyaml"},
		},
		{
			name: "no hyphens: requests",
			purl: "pkg:pypi/requests@2.31.0",
			want: []string{"requests"},
		},
		{
			name: "py-prefix stripped: py-cpuinfo",
			purl: "pkg:pypi/py-cpuinfo@9.0.0",
			want: []string{"py_cpuinfo", "cpuinfo"},
		},
		{
			name: "multiple hyphens: python-jose-cryptodome",
			purl: "pkg:pypi/python-jose-cryptodome@1.3.2",
			want: []string{"python_jose_cryptodome", "jose_cryptodome"},
		},
		{
			name: "simple name: flask",
			purl: "pkg:pypi/flask@3.0.0",
			want: []string{"flask"},
		},
		{
			name: "dotted name: zope.interface",
			purl: "pkg:pypi/zope.interface@6.0",
			want: []string{"zope.interface"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildImportPaths([]string{tt.purl})
			got := result[tt.purl]
			if tt.want == nil {
				if got != nil {
					t.Errorf("buildImportPaths(%s) = %v, want no entry", tt.purl, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("missing import paths for %s", tt.purl)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildImportPaths(%s) = %v, want %v", tt.purl, got, tt.want)
			}
		})
	}
}

func TestComputeHealthSignals_EOL(t *testing.T) {
	a := &domain.Analysis{
		OverallScore: 5.0,
		EOL:          domain.EOLStatus{State: domain.EOLEndOfLife},
	}
	h := computeHealthSignals(a)
	if !h.IsEOL {
		t.Error("expected IsEOL = true for EOLEndOfLife state")
	}
	if h.HealthRisk < 0.85 {
		t.Errorf("expected HealthRisk >= 0.85 for EOL, got %f", h.HealthRisk)
	}
}

func TestComputeHealthSignals_Active(t *testing.T) {
	a := &domain.Analysis{
		OverallScore: 8.0,
		EOL:          domain.EOLStatus{State: domain.EOLNotEOL},
		// Without AxisResults, FinalMaintenanceStatus() returns "Review Needed"
		// which maps to base risk 0.5, plus Scorecard adjustment (1-0.8)*0.1 = 0.02
		// Total: 0.52
	}
	h := computeHealthSignals(a)
	if h.IsEOL {
		t.Error("expected IsEOL = false for NotEOL state")
	}
	if h.HealthRisk > 0.6 {
		t.Errorf("expected HealthRisk <= 0.6 for non-EOL dep, got %f", h.HealthRisk)
	}
}

func TestComputeHealthSignals_Archived(t *testing.T) {
	a := &domain.Analysis{
		OverallScore: 5.0,
		EOL:          domain.EOLStatus{State: domain.EOLNotEOL},
		RepoState: &domain.RepoState{
			IsArchived:          true,
			DaysSinceLastCommit: 500,
		},
	}
	h := computeHealthSignals(a)
	if h.MaintenanceStatus != domaindiet.MaintenanceStatusArchived {
		t.Errorf("expected MaintenanceStatus = Archived, got %s", h.MaintenanceStatus)
	}
	if h.HealthRisk < 0.85 {
		t.Errorf("expected HealthRisk >= 0.85 for archived, got %f", h.HealthRisk)
	}
	if !h.IsStalled {
		t.Error("expected IsStalled = true for 500 days since last commit")
	}
}

func TestComputeHealthSignals_Vulnerabilities(t *testing.T) {
	a := &domain.Analysis{
		OverallScore: 7.0,
		EOL:          domain.EOLStatus{State: domain.EOLNotEOL},
		ReleaseInfo: &domain.ReleaseInfo{
			StableVersion: &domain.VersionDetail{
				Version: "v1.0.0",
				Advisories: []domain.Advisory{
					{ID: "GHSA-1", CVSS3Score: 9.8, Severity: "CRITICAL"},
					{ID: "GHSA-2", CVSS3Score: 5.0, Severity: "MEDIUM"},
				},
			},
		},
	}
	h := computeHealthSignals(a)
	if !h.HasVulnerabilities {
		t.Error("expected HasVulnerabilities = true")
	}
	if h.VulnerabilityCount != 2 {
		t.Errorf("expected VulnerabilityCount = 2, got %d", h.VulnerabilityCount)
	}
	if h.MaxCVSSScore != 9.8 {
		t.Errorf("expected MaxCVSSScore = 9.8, got %f", h.MaxCVSSScore)
	}
}

func TestRun_BasicPipeline(t *testing.T) {
	graphResult := &domaindiet.GraphResult{
		DirectDeps: []string{
			"pkg:golang/github.com/stretchr/testify@v1.9.0",
			"pkg:golang/github.com/gin-gonic/gin@v1.10.0",
		},
		AllDeps: []string{
			"pkg:golang/github.com/stretchr/testify@v1.9.0",
			"pkg:golang/github.com/gin-gonic/gin@v1.10.0",
			"pkg:golang/github.com/pmezard/go-difflib@v1.0.0",
		},
		Metrics: map[string]*domaindiet.GraphMetrics{
			"pkg:golang/github.com/stretchr/testify@v1.9.0": {
				ExclusiveTransitiveCount: 2,
				TotalTransitiveCount:     3,
				SharedTransitiveCount:    1,
			},
			"pkg:golang/github.com/gin-gonic/gin@v1.10.0": {
				ExclusiveTransitiveCount: 10,
				TotalTransitiveCount:     15,
				SharedTransitiveCount:    5,
			},
		},
		TotalTransitive: 20,
	}

	sourceResults := map[string]*domaindiet.CouplingAnalysis{
		"pkg:golang/github.com/gin-gonic/gin@v1.10.0": {
			ImportFileCount: 5,
			CallSiteCount:   12,
			APIBreadth:      3,
			IsUnused:        false,
		},
		// testify not in source results -> marked as unused
	}

	svc := NewService(
		&stubGraphAnalyzer{result: graphResult},
		&stubSourceAnalyzer{result: sourceResults},
		nil, // no AnalysisService for this test
	)

	plan, err := svc.Run(context.Background(), DietInput{
		SBOMData:   []byte("fake-sbom"),
		SBOMPath:   "test.sbom.json",
		SourceRoot: "/tmp/src",
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(plan.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(plan.Entries))
	}

	// Entries should be ranked (rank 1 has highest priority)
	if plan.Entries[0].Scores.Rank != 1 {
		t.Errorf("first entry should have rank 1, got %d", plan.Entries[0].Scores.Rank)
	}
	if plan.Entries[1].Scores.Rank != 2 {
		t.Errorf("second entry should have rank 2, got %d", plan.Entries[1].Scores.Rank)
	}

	// Check that testify is marked as unused (not in source results)
	for _, e := range plan.Entries {
		if e.Name == "github.com/stretchr/testify" {
			if !e.Coupling.IsUnused {
				t.Error("testify should be marked as unused")
			}
		}
	}

	// Summary checks
	if plan.Summary.TotalDirect != 2 {
		t.Errorf("expected TotalDirect = 2, got %d", plan.Summary.TotalDirect)
	}
	if plan.Summary.UnusedDirect < 1 {
		t.Errorf("expected at least 1 unused direct dep, got %d", plan.Summary.UnusedDirect)
	}
	if plan.SBOMPath != "test.sbom.json" {
		t.Errorf("expected SBOMPath = test.sbom.json, got %s", plan.SBOMPath)
	}
}

func TestIsWorkspaceDep(t *testing.T) {
	tests := []struct {
		purl string
		want bool
	}{
		{"pkg:npm/express@4.18.0", false},
		{"pkg:npm/%40scope/pkg@0.0.0-use.local", true},
		{"pkg:npm/my-lib@workspace:*", true},
		{"pkg:npm/my-lib@workspace:^1.0.0", true},
		{"pkg:npm/my-lib@link:../packages/lib", true},
		{"pkg:npm/my-lib@file:../packages/lib", true},
		{"pkg:npm/%40types/node@20.0.0", false},
		{"pkg:golang/github.com/foo/bar@v0.0.0-use.local", false}, // only npm
		{"invalid-purl", false},
	}
	for _, tt := range tests {
		t.Run(tt.purl, func(t *testing.T) {
			got := isWorkspaceDep(tt.purl)
			if got != tt.want {
				t.Errorf("isWorkspaceDep(%q) = %v, want %v", tt.purl, got, tt.want)
			}
		})
	}
}

func TestFilterWorkspaceDeps(t *testing.T) {
	purls := []string{
		"pkg:npm/express@4.18.0",
		"pkg:npm/%40scope/local-pkg@0.0.0-use.local",
		"pkg:npm/docs@0.0.0-use.local",
		"pkg:npm/%40types/node@20.0.0",
	}
	filtered := filterWorkspaceDeps(purls)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 deps after filtering, got %d: %v", len(filtered), filtered)
	}
	for _, p := range filtered {
		if isWorkspaceDep(p) {
			t.Errorf("workspace dep %q should have been filtered out", p)
		}
	}
}

func TestRun_NoSourceAnalyzer(t *testing.T) {
	graphResult := &domaindiet.GraphResult{
		DirectDeps: []string{"pkg:golang/github.com/foo/bar@v1.0.0"},
		Metrics: map[string]*domaindiet.GraphMetrics{
			"pkg:golang/github.com/foo/bar@v1.0.0": {
				ExclusiveTransitiveCount: 5,
				TotalTransitiveCount:     5,
			},
		},
		TotalTransitive: 10,
	}

	svc := NewService(
		&stubGraphAnalyzer{result: graphResult},
		nil, // no source analyzer
		nil,
	)

	plan, err := svc.Run(context.Background(), DietInput{
		SBOMData: []byte("fake-sbom"),
		SBOMPath: "test.sbom.json",
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(plan.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(plan.Entries))
	}
	// Without source analyzer, coupling should be zero-value (not unused)
	if plan.Entries[0].Coupling.IsUnused {
		t.Error("without source analyzer, coupling should not be marked unused")
	}
}
