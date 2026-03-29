package eolevaluator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/maven"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/nuget"
)

func TestEvaluator_NuGet_CriticalBugs(t *testing.T) {
	// Registration index returns embedded deprecation with CriticalBugs
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/registration5-semver2/test.package/index.json" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"items":[{"items":[{"catalogEntry":{"id":"Test.Package"},"deprecation":{"reasons":["CriticalBugs"],"message":"","alternatePackage":null}}]}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ng := nuget.NewClient()
	ng.SetBaseURL(srv.URL + "/v3/registration5-semver2")

	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetNuGetClient(ng)

	a := &domain.Analysis{Package: &domain.Package{PURL: "pkg:nuget/Test.Package"}}
	m := map[string]*domain.Analysis{"k": a}
	out, err := ev.EvaluateBatch(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	st := out["k"]
	if st.State != domain.EOLEndOfLife {
		t.Fatalf("expected EOL due to CriticalBugs, got %v", st.State)
	}
}

func TestEvaluator_NuGet_LegacyWithSuccessor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/registration5-semver2/old.package/index.json" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"items":[{"items":[{"catalogEntry":{"id":"Old.Package"},"deprecation":{"reasons":["Legacy"],"message":"","alternatePackage":{"id":"New.Package","range":"[1.0,)"}}}]}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ng := nuget.NewClient()
	ng.SetBaseURL(srv.URL + "/v3/registration5-semver2")

	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetNuGetClient(ng)

	a := &domain.Analysis{Package: &domain.Package{PURL: "pkg:nuget/Old.Package"}}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": a})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	st := out["k"]
	if st.State != domain.EOLEndOfLife || st.Successor != "New.Package" {
		t.Fatalf("expected EOL with successor, got state=%v successor=%q", st.State, st.Successor)
	}
}

func TestEvaluator_Maven_Relocation(t *testing.T) {
	// Simulate a Maven repo serving a POM with relocation
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Expect path: /com/old/lib/1.0.0/lib-1.0.0.pom
		if r.URL.Path == "/com/old/lib/1.0.0/lib-1.0.0.pom" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.old</groupId>
  <artifactId>lib</artifactId>
  <version>1.0.0</version>
  <distributionManagement>
	<relocation>
	  <groupId>com.new</groupId>
	  <artifactId>lib2</artifactId>
	  <version>2.0.0</version>
	  <message>Migrated to com.new:lib2</message>
	</relocation>
  </distributionManagement>
</project>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	mv := maven.NewClient()
	mv.SetBaseURL(srv.URL)

	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetMavenClient(mv)

	a := &domain.Analysis{Package: &domain.Package{PURL: "pkg:maven/com.old/lib@1.0.0"}}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": a})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	st := out["k"]
	if st.State != domain.EOLEndOfLife {
		t.Fatalf("expected EOL due to Maven relocation, got %v", st.State)
	}
	if st.Successor != "com.new/lib2" {
		t.Fatalf("successor = %q, want %q", st.Successor, "com.new/lib2")
	}
	if len(st.Evidences) == 0 {
		t.Fatalf("expected at least one evidence for relocation")
	}
}

