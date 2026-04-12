package sbomgraph

import (
	"testing"
)

func TestResolveDirectPURLs_AggregatorPOM(t *testing.T) {
	// Simulates a multi-module Maven aggregator POM where the root
	// depends on sub-modules, and sub-modules depend on external libs.
	//
	// root (com.example/parent) → [module-a, module-b]
	// module-a → [spring-web, guava]
	// module-b → [jackson]
	bom := &BOMEnvelope{
		Metadata: &BOMMetadata{
			Component: &Component{
				BOMRef: "root-ref",
				PURL:   "pkg:maven/com.example/parent@1.0.0",
			},
		},
		Dependencies: []Dependency{
			{Ref: "root-ref", DependsOn: []string{"mod-a-ref", "mod-b-ref"}},
			{Ref: "mod-a-ref", DependsOn: []string{"spring-web-ref", "guava-ref"}},
			{Ref: "mod-b-ref", DependsOn: []string{"jackson-ref"}},
			{Ref: "spring-web-ref"},
			{Ref: "guava-ref"},
			{Ref: "jackson-ref"},
		},
	}
	refMap := map[string]string{
		"root-ref":       "pkg:maven/com.example/parent@1.0.0",
		"mod-a-ref":      "pkg:maven/com.example/module-a@1.0.0",
		"mod-b-ref":      "pkg:maven/com.example/module-b@1.0.0",
		"spring-web-ref": "pkg:maven/org.springframework/spring-web@6.1.0",
		"guava-ref":      "pkg:maven/com.google.guava/guava@33.0.0",
		"jackson-ref":    "pkg:maven/com.fasterxml.jackson.core/jackson-databind@2.17.0",
	}

	direct := ResolveDirectPURLs(bom, refMap)

	// Sub-modules should be flattened; external deps should be direct.
	wantDirect := map[string]bool{
		"pkg:maven/org.springframework/spring-web@6.1.0":             true,
		"pkg:maven/com.google.guava/guava@33.0.0":                    true,
		"pkg:maven/com.fasterxml.jackson.core/jackson-databind@2.17.0": true,
	}
	wantAbsent := []string{
		"pkg:maven/com.example/module-a@1.0.0",
		"pkg:maven/com.example/module-b@1.0.0",
		"pkg:maven/com.example/parent@1.0.0",
	}

	for purl := range wantDirect {
		if _, ok := direct[purl]; !ok {
			t.Errorf("expected %s in direct deps, got absent", purl)
		}
	}
	for _, purl := range wantAbsent {
		if _, ok := direct[purl]; ok {
			t.Errorf("expected %s absent from direct deps (sub-module), got present", purl)
		}
	}
	if len(direct) != len(wantDirect) {
		t.Errorf("direct dep count = %d, want %d", len(direct), len(wantDirect))
	}
}

func TestResolveDirectPURLs_NormalProject(t *testing.T) {
	// Normal single-module project: root depends on external libs with
	// different namespaces. Should NOT be flattened.
	bom := &BOMEnvelope{
		Metadata: &BOMMetadata{
			Component: &Component{
				BOMRef: "root-ref",
				PURL:   "pkg:maven/com.example/myapp@1.0.0",
			},
		},
		Dependencies: []Dependency{
			{Ref: "root-ref", DependsOn: []string{"spring-ref", "guava-ref"}},
			{Ref: "spring-ref"},
			{Ref: "guava-ref"},
		},
	}
	refMap := map[string]string{
		"root-ref":   "pkg:maven/com.example/myapp@1.0.0",
		"spring-ref": "pkg:maven/org.springframework/spring-web@6.1.0",
		"guava-ref":  "pkg:maven/com.google.guava/guava@33.0.0",
	}

	direct := ResolveDirectPURLs(bom, refMap)

	if _, ok := direct["pkg:maven/org.springframework/spring-web@6.1.0"]; !ok {
		t.Error("expected spring-web in direct deps")
	}
	if _, ok := direct["pkg:maven/com.google.guava/guava@33.0.0"]; !ok {
		t.Error("expected guava in direct deps")
	}
	if len(direct) != 2 {
		t.Errorf("direct dep count = %d, want 2", len(direct))
	}
}

func TestResolveDirectPURLs_MixedNamespaces(t *testing.T) {
	// Root has deps with a mix of same-namespace (sub-modules) and different
	// namespaces (external). Should NOT be flattened because not all direct
	// deps share the root's namespace.
	bom := &BOMEnvelope{
		Metadata: &BOMMetadata{
			Component: &Component{
				BOMRef: "root-ref",
				PURL:   "pkg:maven/com.example/parent@1.0.0",
			},
		},
		Dependencies: []Dependency{
			{Ref: "root-ref", DependsOn: []string{"mod-a-ref", "external-ref"}},
			{Ref: "mod-a-ref", DependsOn: []string{"guava-ref"}},
			{Ref: "external-ref"},
			{Ref: "guava-ref"},
		},
	}
	refMap := map[string]string{
		"root-ref":     "pkg:maven/com.example/parent@1.0.0",
		"mod-a-ref":    "pkg:maven/com.example/module-a@1.0.0",
		"external-ref": "pkg:maven/org.external/lib@1.0.0",
		"guava-ref":    "pkg:maven/com.google.guava/guava@33.0.0",
	}

	direct := ResolveDirectPURLs(bom, refMap)

	// Mixed namespaces: no flattening. Both module-a and external-lib are direct.
	if _, ok := direct["pkg:maven/com.example/module-a@1.0.0"]; !ok {
		t.Error("expected module-a in direct deps (mixed namespaces, no flattening)")
	}
	if _, ok := direct["pkg:maven/org.external/lib@1.0.0"]; !ok {
		t.Error("expected external/lib in direct deps")
	}
}
