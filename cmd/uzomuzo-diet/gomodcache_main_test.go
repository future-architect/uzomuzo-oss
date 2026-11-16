package main

import (
	"os"
	"testing"
)

// TestMain points GOMODCACHE at an empty directory so the real tree-sitter
// analyzer used by these tests never reads the developer's module cache,
// which would make Go call-site counts differ between machines (#551).
func TestMain(m *testing.M) {
	cache, err := os.MkdirTemp("", "gomodcache-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("GOMODCACHE", cache); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(cache) // best-effort cleanup of the empty temp dir
	os.Exit(code)
}
