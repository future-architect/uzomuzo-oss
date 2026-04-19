package actions_test

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/actions"
)

func TestIsTagRef(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want bool
	}{
		{"v1", "v1", true},
		{"v2", "v2", true},
		{"v3.1", "v3.1", true},
		{"v4.2.0", "v4.2.0", true},
		{"1", "1", true},
		{"1.2", "1.2", true},
		{"1.2.3", "1.2.3", true},
		{"whitespace tolerated", "  v4  ", true},
		{"empty", "", false},
		{"main", "main", false},
		{"master", "master", false},
		{"develop", "develop", false},
		{"SHA", "de0fac2e4500dabe0009e67214ff5f5447ce83dd", false},
		{"bare v", "v", false},
		{"pre-release -rc1", "v1.2.3-rc1", false},
		{"pre-release +build", "v1.2.3+build", false},
		{"latest", "latest", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := actions.IsTagRef(tt.ref); got != tt.want {
				t.Errorf("IsTagRef(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

func TestMajorOf(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{"v1", "v1", "v1"},
		{"v2", "v2", "v2"},
		{"v3.1 → v3", "v3.1", "v3"},
		{"v4.2.0 → v4", "v4.2.0", "v4"},
		{"bare 1 → v1", "1", "v1"},
		{"bare 1.2 → v1", "1.2", "v1"},
		{"bare 1.2.3 → v1", "1.2.3", "v1"},
		{"branch main → empty", "main", ""},
		{"empty → empty", "", ""},
		{"non-numeric → empty", "abc123", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := actions.MajorOf(tt.ref); got != tt.want {
				t.Errorf("MajorOf(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

func TestMatchesMajor(t *testing.T) {
	tests := []struct {
		name         string
		pin          string
		catalogMajor string
		want         bool
	}{
		{"v2 matches v2", "v2", "v2", true},
		{"v2.3 matches v2", "v2.3", "v2", true},
		{"v2.3.1 matches v2", "v2.3.1", "v2", true},
		{"bare 2 matches v2", "2", "v2", true},
		{"v3 does not match v2", "v3", "v2", false},
		{"branch does not match", "main", "v2", false},
		{"empty does not match", "", "v2", false},
		{"v11 does not match v1", "v11", "v1", false},
		{"v1 does not match v11", "v1", "v11", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := actions.MatchesMajor(tt.pin, tt.catalogMajor); got != tt.want {
				t.Errorf("MatchesMajor(%q, %q) = %v, want %v", tt.pin, tt.catalogMajor, got, tt.want)
			}
		})
	}
}
