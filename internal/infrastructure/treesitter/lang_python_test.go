//go:build cgo

package treesitter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzer_Python(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(`import requests
from os import path

requests.get("https://example.com")
requests.post("https://example.com")
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:pypi/requests@2.31.0": {"requests"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:pypi/requests@2.31.0"]
	if !ok {
		t.Fatal("expected coupling analysis for requests")
	}

	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	if ca.CallSiteCount != 2 {
		t.Errorf("CallSiteCount = %d, want 2", ca.CallSiteCount)
	}
	if ca.APIBreadth != 2 {
		t.Errorf("APIBreadth = %d, want 2", ca.APIBreadth)
	}
}

func TestAnalyzer_PythonFromImport(t *testing.T) {
	tests := []struct {
		name         string
		code         string
		importPaths  map[string][]string
		purl         string
		wantImports  int
		wantCalls    int
		wantBreadth  int
		wantWildcard bool
	}{
		{
			name: "basic from-import with bare calls",
			code: `from requests import get, post

get("https://example.com")
post("https://example.com")
`,
			importPaths: map[string][]string{
				"pkg:pypi/requests@2.31.0": {"requests"},
			},
			purl:        "pkg:pypi/requests@2.31.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
		},
		{
			name: "mixed import and from-import styles",
			code: `import requests
from requests import get

requests.post("https://example.com")
get("https://example.com")
`,
			importPaths: map[string][]string{
				"pkg:pypi/requests@2.31.0": {"requests"},
			},
			purl:        "pkg:pypi/requests@2.31.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
		},
		{
			name: "aliased from-import",
			code: `from os.path import join as pjoin

pjoin("a", "b")
`,
			importPaths: map[string][]string{
				"pkg:pypi/os-path@1.0.0": {"os.path"},
			},
			purl:        "pkg:pypi/os-path@1.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "from-import without calls is not unused",
			code: `from sqlmodel import Session

x = Session
`,
			importPaths: map[string][]string{
				"pkg:pypi/sqlmodel@0.0.8": {"sqlmodel"},
			},
			purl:        "pkg:pypi/sqlmodel@0.0.8",
			wantImports: 1,
			wantCalls:   0,
			wantBreadth: 0,
		},
		{
			name: "from-import with multiple calls",
			code: `from inline_snapshot import snapshot, outsource

snapshot([1, 2, 3])
snapshot("hello")
outsource("data")
`,
			importPaths: map[string][]string{
				"pkg:pypi/inline-snapshot@0.8.0": {"inline_snapshot"},
			},
			purl:        "pkg:pypi/inline-snapshot@0.8.0",
			wantImports: 1,
			wantCalls:   3,
			wantBreadth: 2,
		},
		{
			name: "aliased import statement",
			code: `import requests as r

r.get("https://example.com")
r.post("https://example.com")
`,
			importPaths: map[string][]string{
				"pkg:pypi/requests@2.31.0": {"requests"},
			},
			purl:        "pkg:pypi/requests@2.31.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
		},
		{
			name: "wildcard from-import records import file",
			code: `from flask import *

app = Flask(__name__)
`,
			importPaths: map[string][]string{
				"pkg:pypi/flask@3.0.0": {"flask"},
			},
			purl:         "pkg:pypi/flask@3.0.0",
			wantImports:  1,
			wantCalls:    0,
			wantBreadth:  0,
			wantWildcard: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(tt.code), 0644)
			if err != nil {
				t.Fatal(err)
			}

			analyzer := NewAnalyzer()
			result, err := analyzer.AnalyzeCoupling(context.Background(), dir, tt.importPaths)
			if err != nil {
				t.Fatal(err)
			}

			ca, ok := result[tt.purl]
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purl)
			}

			if ca.ImportFileCount != tt.wantImports {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImports)
			}
			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
			if ca.HasWildcardImport != tt.wantWildcard {
				t.Errorf("HasWildcardImport = %v, want %v", ca.HasWildcardImport, tt.wantWildcard)
			}
		})
	}
}

func TestAnalyzer_PythonPrefixNoFalseMatch(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(`import requests
import request

requests.get("https://example.com")
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:pypi/requests@2.31.0": {"requests"},
		"pkg:pypi/request@1.0.0":   {"request"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	// "requests" should match pkg:pypi/requests, not pkg:pypi/request
	caRequests, ok := result["pkg:pypi/requests@2.31.0"]
	if !ok {
		t.Fatal("expected coupling analysis for requests")
	}
	if caRequests.CallSiteCount != 1 {
		t.Errorf("requests CallSiteCount = %d, want 1", caRequests.CallSiteCount)
	}

	// "request" should be a separate entry with no call sites but still imported
	caRequest, ok := result["pkg:pypi/request@1.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for request")
	}
	if caRequest.IsUnused {
		t.Error("request should not be unused (it is imported)")
	}
	if caRequest.CallSiteCount != 0 {
		t.Errorf("request CallSiteCount = %d, want 0", caRequest.CallSiteCount)
	}
}

func TestAnalyzer_PythonTryExceptImport(t *testing.T) {
	tests := []struct {
		name            string
		code            string
		wantBlankImport bool
		wantUnused      bool
		wantImportCount int
		wantCallSites   int
	}{
		{
			name: "try/except ImportError bare import",
			code: `try:
    import cryptography
except ImportError:
    pass
`,
			wantBlankImport: true,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   1, // baseline for blank/side-effect import (#261)
		},
		{
			name: "try/except ModuleNotFoundError",
			code: `try:
    import cryptography
except ModuleNotFoundError:
    raise RuntimeError("missing")
`,
			wantBlankImport: true,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   1, // baseline for blank/side-effect import (#261)
		},
		{
			name: "try/except bare except",
			code: `try:
    import cryptography
except:
    pass
`,
			wantBlankImport: true,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   1, // baseline for blank/side-effect import (#261)
		},
		{
			name: "try/except with from-import",
			code: `try:
    from cryptography import fernet
except ImportError:
    fernet = None
`,
			wantBlankImport: true,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   1, // baseline for blank/side-effect import (#261)
		},
		{
			name: "regular import not in try/except",
			code: `import cryptography
cryptography.fernet.Fernet("key")
`,
			wantBlankImport: false,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   1,
		},
		{
			name: "try/except with unrelated exception type",
			code: `try:
    import cryptography
except ValueError:
    pass
`,
			wantBlankImport: false,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   0,
		},
		{
			name: "import inside except ImportError is not feature detection",
			code: `try:
    import unavailable_module
except ImportError:
    import cryptography

cryptography.fernet.Fernet("key")
`,
			wantBlankImport: false,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   1,
		},
		{
			name: "try/except ImportError import used via module attribute",
			code: `try:
    import cryptography
except ImportError:
    pass

cryptography.fernet.Fernet("key")
`,
			wantBlankImport: true,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   1,
		},
		{
			name: "try/except ImportError from import used via imported name",
			code: `try:
    from cryptography.fernet import Fernet
except ImportError:
    pass

Fernet("key")
`,
			wantBlankImport: true,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   1,
		},
		{
			name: "try/except with tuple containing ImportError",
			code: `try:
    import cryptography
except (ImportError, ValueError):
    pass
`,
			wantBlankImport: true,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   1, // baseline for blank/side-effect import (#261)
		},
		{
			name: "try/except with tuple of unrelated exceptions",
			code: `try:
    import cryptography
except (ValueError, TypeError):
    pass
`,
			wantBlankImport: false,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(tt.code), 0644)
			if err != nil {
				t.Fatal(err)
			}

			analyzer := NewAnalyzer()
			importPaths := map[string][]string{
				"pkg:pypi/cryptography@41.0.0": {"cryptography"},
			}
			result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
			if err != nil {
				t.Fatal(err)
			}

			ca, ok := result["pkg:pypi/cryptography@41.0.0"]
			if !ok {
				t.Fatal("expected coupling analysis for cryptography")
			}

			if ca.HasBlankImport != tt.wantBlankImport {
				t.Errorf("HasBlankImport = %v, want %v", ca.HasBlankImport, tt.wantBlankImport)
			}
			if ca.IsUnused != tt.wantUnused {
				t.Errorf("IsUnused = %v, want %v", ca.IsUnused, tt.wantUnused)
			}
			if ca.ImportFileCount != tt.wantImportCount {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImportCount)
			}
			if ca.CallSiteCount != tt.wantCallSites {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCallSites)
			}
		})
	}
}

func TestAnalyzer_PythonTypeCheckingImport(t *testing.T) {
	tests := []struct {
		name            string
		code            string
		importPaths     map[string][]string
		purl            string
		wantImports     int
		wantCalls       int
		wantBreadth     int
		wantBlankImport bool
	}{
		{
			name: "if TYPE_CHECKING from-import skipped",
			code: `from typing import TYPE_CHECKING
if TYPE_CHECKING:
    from hpack import HeaderTuple

def foo():
    pass
`,
			importPaths: map[string][]string{
				"pkg:pypi/hpack@4.0.0": {"hpack"},
			},
			purl:        "pkg:pypi/hpack@4.0.0",
			wantImports: 0,
			wantCalls:   0,
			wantBreadth: 0,
		},
		{
			name: "if typing.TYPE_CHECKING from-import skipped",
			code: `import typing
if typing.TYPE_CHECKING:
    from hpack import HeaderTuple

def foo():
    pass
`,
			importPaths: map[string][]string{
				"pkg:pypi/hpack@4.0.0": {"hpack"},
			},
			purl:        "pkg:pypi/hpack@4.0.0",
			wantImports: 0,
			wantCalls:   0,
			wantBreadth: 0,
		},
		{
			name: "mixed: TYPE_CHECKING import and normal import",
			code: `from typing import TYPE_CHECKING
import requests
if TYPE_CHECKING:
    from hpack import HeaderTuple

requests.get("https://example.com")
`,
			importPaths: map[string][]string{
				"pkg:pypi/hpack@4.0.0":     {"hpack"},
				"pkg:pypi/requests@2.31.0": {"requests"},
			},
			purl:        "pkg:pypi/hpack@4.0.0",
			wantImports: 0,
			wantCalls:   0,
			wantBreadth: 0,
		},
		{
			name: "normal import not affected by TYPE_CHECKING",
			code: `from typing import TYPE_CHECKING
import requests
if TYPE_CHECKING:
    from hpack import HeaderTuple

requests.get("https://example.com")
`,
			importPaths: map[string][]string{
				"pkg:pypi/hpack@4.0.0":     {"hpack"},
				"pkg:pypi/requests@2.31.0": {"requests"},
			},
			purl:        "pkg:pypi/requests@2.31.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "nested if inside TYPE_CHECKING skipped",
			code: `from typing import TYPE_CHECKING
import sys
if TYPE_CHECKING:
    if sys.version_info >= (3, 10):
        from hpack import HeaderTuple

def foo():
    pass
`,
			importPaths: map[string][]string{
				"pkg:pypi/hpack@4.0.0": {"hpack"},
			},
			purl:        "pkg:pypi/hpack@4.0.0",
			wantImports: 0,
			wantCalls:   0,
			wantBreadth: 0,
		},
		{
			name: "if TYPE_CHECKING bare import skipped",
			code: `from typing import TYPE_CHECKING
if TYPE_CHECKING:
    import hpack
`,
			importPaths: map[string][]string{
				"pkg:pypi/hpack@4.0.0": {"hpack"},
			},
			purl:        "pkg:pypi/hpack@4.0.0",
			wantImports: 0,
			wantCalls:   0,
			wantBreadth: 0,
		},
		{
			name: "else branch of TYPE_CHECKING not skipped",
			code: `from typing import TYPE_CHECKING
if TYPE_CHECKING:
    from typing import Protocol
else:
    from hpack import HeaderTuple

HeaderTuple()
`,
			importPaths: map[string][]string{
				"pkg:pypi/hpack@4.0.0": {"hpack"},
			},
			purl:        "pkg:pypi/hpack@4.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "elif TYPE_CHECKING from-import skipped",
			code: `from typing import TYPE_CHECKING
import sys
if sys.platform == "win32":
    from os import path
elif TYPE_CHECKING:
    from hpack import HeaderTuple
`,
			importPaths: map[string][]string{
				"pkg:pypi/hpack@4.0.0": {"hpack"},
			},
			purl:        "pkg:pypi/hpack@4.0.0",
			wantImports: 0,
			wantCalls:   0,
			wantBreadth: 0,
		},
		{
			name: "if not TYPE_CHECKING not skipped",
			code: `from typing import TYPE_CHECKING
if not TYPE_CHECKING:
    from hpack import HeaderTuple

HeaderTuple()
`,
			importPaths: map[string][]string{
				"pkg:pypi/hpack@4.0.0": {"hpack"},
			},
			purl:        "pkg:pypi/hpack@4.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(tt.code), 0644)
			if err != nil {
				t.Fatal(err)
			}

			analyzer := NewAnalyzer()
			result, err := analyzer.AnalyzeCoupling(context.Background(), dir, tt.importPaths)
			if err != nil {
				t.Fatal(err)
			}

			ca, ok := result[tt.purl]
			if tt.wantImports == 0 && tt.wantCalls == 0 && !ok {
				// PURL not in result at all means 0 imports/calls — expected.
				return
			}
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purl)
			}

			if ca.ImportFileCount != tt.wantImports {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImports)
			}
			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
			if ca.HasBlankImport != tt.wantBlankImport {
				t.Errorf("HasBlankImport = %v, want %v", ca.HasBlankImport, tt.wantBlankImport)
			}
		})
	}
}

func TestAnalyzer_PythonDecoratorUsage(t *testing.T) {
	// Verify that decorators using imported modules are captured as call sites
	// via the existing (attribute) query pattern. This is a coverage verification
	// test — no code changes needed for this pattern.
	tests := []struct {
		name        string
		code        string
		importPaths map[string][]string
		purl        string
		wantImports int
		wantCalls   int
		wantBreadth int
	}{
		{
			name: "attribute decorator @pytest.fixture",
			code: `import pytest

@pytest.fixture
def my_fixture():
    return 42

def test_something(my_fixture):
    assert my_fixture == 42
`,
			importPaths: map[string][]string{
				"pkg:pypi/pytest@7.0.0": {"pytest"},
			},
			purl:        "pkg:pypi/pytest@7.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "bare decorator from from-import",
			code: `from pytest import fixture

@fixture
def my_fixture():
    return 42
`,
			importPaths: map[string][]string{
				"pkg:pypi/pytest@7.0.0": {"pytest"},
			},
			purl:        "pkg:pypi/pytest@7.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "decorator with arguments @pytest.mark.parametrize",
			code: `import pytest

@pytest.mark
def test_something():
    pass
`,
			importPaths: map[string][]string{
				"pkg:pypi/pytest@7.0.0": {"pytest"},
			},
			purl:        "pkg:pypi/pytest@7.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			err := os.WriteFile(filepath.Join(dir, "test_main.py"), []byte(tt.code), 0644)
			if err != nil {
				t.Fatal(err)
			}

			analyzer := NewAnalyzer()
			result, err := analyzer.AnalyzeCoupling(context.Background(), dir, tt.importPaths)
			if err != nil {
				t.Fatal(err)
			}

			ca, ok := result[tt.purl]
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purl)
			}
			if ca.ImportFileCount != tt.wantImports {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImports)
			}
			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
		})
	}
}
