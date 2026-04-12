package diet

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
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
		{
			name: "override: guava groupId differs from package",
			purl: "pkg:maven/com.google.guava/guava@33.0.0",
			want: []string{"com.google.common", "com.google.guava", "com.google.guava.guava"},
		},
		{
			name: "override: antlr4-runtime groupId differs from package",
			purl: "pkg:maven/org.antlr/antlr4-runtime@4.13.0",
			want: []string{"org.antlr.v4", "org.antlr"},
		},
		{
			name: "override: ST4 case-insensitive lookup",
			purl: "pkg:maven/org.antlr/ST4@4.3.4",
			want: []string{"org.stringtemplate", "org.antlr", "org.antlr.ST4"},
		},
		{
			name: "override: trove4j groupId differs from package",
			purl: "pkg:maven/net.sf.trove4j/trove4j@3.0.3",
			want: []string{"gnu.trove", "net.sf.trove4j", "net.sf.trove4j.trove4j"},
		},
		{
			name: "override: scala-library hyphenated namespace",
			purl: "pkg:maven/org.scala-lang/scala-library@2.13.12",
			want: []string{"scala"},
		},
		{
			name: "override: scala-reflect hyphenated namespace",
			purl: "pkg:maven/org.scala-lang/scala-reflect@2.13.12",
			want: []string{"scala.reflect"},
		},
		{
			name: "override: spring-boot-starter-web maps to web packages, not bare groupId",
			purl: "pkg:maven/org.springframework.boot/spring-boot-starter-web@3.2.0",
			want: []string{"org.springframework.web", "org.springframework.boot.web", "org.springframework.boot"},
		},
		{
			name: "override: spring-boot-starter-data-jpa maps to JPA packages",
			purl: "pkg:maven/org.springframework.boot/spring-boot-starter-data-jpa@3.2.0",
			want: []string{"org.springframework.data.jpa", "javax.persistence", "jakarta.persistence", "org.springframework.boot"},
		},
		{
			name: "override: spring-boot-starter-security maps to security packages",
			purl: "pkg:maven/org.springframework.boot/spring-boot-starter-security@3.2.0",
			want: []string{"org.springframework.security", "org.springframework.boot"},
		},
		{
			name: "heuristic fallback: unlisted starter derives prefix from suffix",
			purl: "pkg:maven/org.springframework.boot/spring-boot-starter-quartz@3.2.0",
			want: []string{"org.springframework.quartz", "org.springframework.boot"},
		},
		{
			name: "override: spring-core maps to specific sub-packages",
			purl: "pkg:maven/org.springframework/spring-core@6.1.0",
			want: []string{"org.springframework.core", "org.springframework.util", "org.springframework"},
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
		nil, // no PyPI resolver for this test
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

func TestRun_ToolDepsNotFlaggedAsUnused(t *testing.T) {
	graphResult := &domaindiet.GraphResult{
		DirectDeps: []string{
			"pkg:golang/github.com/hashicorp/copywrite@v0.19.0",
			"pkg:golang/github.com/gin-gonic/gin@v1.10.0",
		},
		Metrics: map[string]*domaindiet.GraphMetrics{
			"pkg:golang/github.com/hashicorp/copywrite@v0.19.0": {
				ExclusiveTransitiveCount: 1,
				TotalTransitiveCount:     1,
			},
			"pkg:golang/github.com/gin-gonic/gin@v1.10.0": {
				ExclusiveTransitiveCount: 10,
				TotalTransitiveCount:     15,
			},
		},
		TotalTransitive: 16,
	}

	// Source analysis returns no coupling for either dep — both would
	// normally be flagged as unused.
	sourceResults := map[string]*domaindiet.CouplingAnalysis{}

	toolDeps := map[string]struct{}{
		"github.com/hashicorp/copywrite": {},
	}

	svc := NewService(
		&stubGraphAnalyzer{result: graphResult},
		&stubSourceAnalyzer{result: sourceResults},
		nil, // no PyPI resolver
		nil, // no AnalysisService
	)

	plan, err := svc.Run(context.Background(), DietInput{
		SBOMData:   []byte("fake-sbom"),
		SBOMPath:   "test.sbom.json",
		SourceRoot: "/tmp/src",
		ToolDeps:   toolDeps,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, e := range plan.Entries {
		switch e.Name {
		case "github.com/hashicorp/copywrite":
			if e.Scope != domaindiet.ScopeTool {
				t.Errorf("copywrite Scope = %q, want %q", e.Scope, domaindiet.ScopeTool)
			}
			if e.Coupling.IsUnused {
				t.Error("tool dep copywrite should NOT be flagged as unused")
			}
			if e.Scores.Difficulty == domaindiet.DifficultyTrivial {
				t.Error("tool dep copywrite should NOT be classified as trivial difficulty")
			}
		case "github.com/gin-gonic/gin":
			if e.Scope != "" {
				t.Errorf("gin Scope = %q, want empty", e.Scope)
			}
			if !e.Coupling.IsUnused {
				t.Error("non-tool dep gin with 0 imports should be flagged as unused")
			}
		}
	}

	// Summary: only gin should count as unused, not copywrite
	if plan.Summary.UnusedDirect != 1 {
		t.Errorf("expected UnusedDirect = 1 (gin only), got %d", plan.Summary.UnusedDirect)
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
		nil, // no PyPI resolver
		nil, // no AnalysisService
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

// --- Phase 2.5 wheel fallback tests ---

type stubPyPIResolver struct {
	names map[string][]string // packageName → import names
	err   error
}

func (s *stubPyPIResolver) ResolveImportNames(_ context.Context, name string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.names[name], nil
}

// retrySourceAnalyzer returns different results on the first vs second call.
// First call returns firstResult; second call returns secondResult.
type retrySourceAnalyzer struct {
	firstResult  map[string]*domaindiet.CouplingAnalysis
	secondResult map[string]*domaindiet.CouplingAnalysis
	callCount    atomic.Int32
}

func (s *retrySourceAnalyzer) AnalyzeCoupling(_ context.Context, _ string, _ map[string][]string) (map[string]*domaindiet.CouplingAnalysis, error) {
	n := s.callCount.Add(1)
	if n == 1 {
		return s.firstResult, nil
	}
	return s.secondResult, nil
}

func TestRun_WheelFallback(t *testing.T) {
	t.Parallel()

	pypiPURL := "pkg:pypi/beautifulsoup4@4.12.3"
	goPURL := "pkg:golang/github.com/stretchr/testify@v1.9.0"

	graphResult := &domaindiet.GraphResult{
		DirectDeps: []string{pypiPURL, goPURL},
		Metrics: map[string]*domaindiet.GraphMetrics{
			pypiPURL: {TotalTransitiveCount: 2},
			goPURL:   {TotalTransitiveCount: 5},
		},
		TotalTransitive: 7,
	}

	// Phase 2: heuristic paths find testify but NOT beautifulsoup4 (it needs "bs4").
	// Phase 2.5: wheel resolver returns "bs4", retry finds it.
	sourceAnalyzer := &retrySourceAnalyzer{
		firstResult: map[string]*domaindiet.CouplingAnalysis{
			goPURL: {ImportFileCount: 3, CallSiteCount: 5},
		},
		secondResult: map[string]*domaindiet.CouplingAnalysis{
			pypiPURL: {ImportFileCount: 1, CallSiteCount: 2},
		},
	}

	resolver := &stubPyPIResolver{
		names: map[string][]string{
			"beautifulsoup4": {"bs4"},
		},
	}

	svc := NewService(
		&stubGraphAnalyzer{result: graphResult},
		sourceAnalyzer,
		resolver,
		nil,
	)

	plan, err := svc.Run(context.Background(), DietInput{
		SBOMData:   []byte("fake-sbom"),
		SBOMPath:   "test.sbom.json",
		SourceRoot: "/tmp/src",
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// beautifulsoup4 should NOT be marked unused (resolved via wheel fallback)
	for _, entry := range plan.Entries {
		if entry.PURL == pypiPURL {
			if entry.Coupling.IsUnused {
				t.Errorf("beautifulsoup4 should not be marked unused after wheel fallback")
			}
			if entry.Coupling.ImportFileCount != 1 {
				t.Errorf("expected ImportFileCount=1, got %d", entry.Coupling.ImportFileCount)
			}
			return
		}
	}
	t.Fatal("beautifulsoup4 entry not found in plan")
}

func TestRun_WheelFallback_NilCouplingResults(t *testing.T) {
	t.Parallel()

	pypiPURL := "pkg:pypi/beautifulsoup4@4.12.3"

	graphResult := &domaindiet.GraphResult{
		DirectDeps: []string{pypiPURL},
		Metrics: map[string]*domaindiet.GraphMetrics{
			pypiPURL: {TotalTransitiveCount: 2},
		},
		TotalTransitive: 2,
	}

	// Phase 2 returns (nil, nil) — zero imports matched any dependency.
	// Phase 2.5 should still run and merge retry results without panicking.
	sourceAnalyzer := &retrySourceAnalyzer{
		firstResult: nil, // nil map, not empty map
		secondResult: map[string]*domaindiet.CouplingAnalysis{
			pypiPURL: {ImportFileCount: 1, CallSiteCount: 2},
		},
	}

	resolver := &stubPyPIResolver{
		names: map[string][]string{
			"beautifulsoup4": {"bs4"},
		},
	}

	svc := NewService(
		&stubGraphAnalyzer{result: graphResult},
		sourceAnalyzer,
		resolver,
		nil,
	)

	plan, err := svc.Run(context.Background(), DietInput{
		SBOMData:   []byte("fake-sbom"),
		SBOMPath:   "test.sbom.json",
		SourceRoot: "/tmp/src",
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, entry := range plan.Entries {
		if entry.PURL == pypiPURL {
			if entry.Coupling.IsUnused {
				t.Errorf("beautifulsoup4 should not be marked unused after wheel fallback with nil initial results")
			}
			if entry.Coupling.ImportFileCount != 1 {
				t.Errorf("expected ImportFileCount=1, got %d", entry.Coupling.ImportFileCount)
			}
			return
		}
	}
	t.Fatal("beautifulsoup4 entry not found in plan")
}

func TestRun_WheelFallback_NilResolver(t *testing.T) {
	t.Parallel()

	pypiPURL := "pkg:pypi/beautifulsoup4@4.12.3"

	graphResult := &domaindiet.GraphResult{
		DirectDeps: []string{pypiPURL},
		Metrics: map[string]*domaindiet.GraphMetrics{
			pypiPURL: {TotalTransitiveCount: 2},
		},
		TotalTransitive: 2,
	}

	// Source analyzer finds nothing — beautifulsoup4 unmatched.
	sourceAnalyzer := &stubSourceAnalyzer{
		result: map[string]*domaindiet.CouplingAnalysis{},
	}

	svc := NewService(
		&stubGraphAnalyzer{result: graphResult},
		sourceAnalyzer,
		nil, // no resolver
		nil,
	)

	plan, err := svc.Run(context.Background(), DietInput{
		SBOMData:   []byte("fake-sbom"),
		SBOMPath:   "test.sbom.json",
		SourceRoot: "/tmp/src",
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	found := false
	for _, entry := range plan.Entries {
		if entry.PURL == pypiPURL {
			found = true
			if !entry.Coupling.IsUnused {
				t.Error("without resolver, beautifulsoup4 should stay unused")
			}
		}
	}
	if !found {
		t.Fatal("beautifulsoup4 entry not found in plan")
	}
}

func TestRun_WheelFallback_ResolverError(t *testing.T) {
	t.Parallel()

	pypiPURL := "pkg:pypi/beautifulsoup4@4.12.3"

	graphResult := &domaindiet.GraphResult{
		DirectDeps: []string{pypiPURL},
		Metrics: map[string]*domaindiet.GraphMetrics{
			pypiPURL: {TotalTransitiveCount: 2},
		},
		TotalTransitive: 2,
	}

	sourceAnalyzer := &stubSourceAnalyzer{
		result: map[string]*domaindiet.CouplingAnalysis{},
	}

	resolver := &stubPyPIResolver{
		err: fmt.Errorf("network error"),
	}

	svc := NewService(
		&stubGraphAnalyzer{result: graphResult},
		sourceAnalyzer,
		resolver,
		nil,
	)

	plan, err := svc.Run(context.Background(), DietInput{
		SBOMData:   []byte("fake-sbom"),
		SBOMPath:   "test.sbom.json",
		SourceRoot: "/tmp/src",
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Should gracefully degrade — beautifulsoup4 stays unused
	found := false
	for _, entry := range plan.Entries {
		if entry.PURL == pypiPURL {
			found = true
			if !entry.Coupling.IsUnused {
				t.Error("on resolver error, beautifulsoup4 should stay unused")
			}
		}
	}
	if !found {
		t.Fatal("beautifulsoup4 entry not found in plan")
	}
}

