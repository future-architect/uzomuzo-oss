package actions_test

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/actions"
)

func TestIsTagRef(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"v1", true},
		{"v2", true},
		{"v3.1", true},
		{"v4.2.0", true},
		{"1", true},
		{"1.2", true},
		{"1.2.3", true},
		{"  v4  ", true}, // whitespace tolerated
		{"", false},
		{"main", false},
		{"master", false},
		{"develop", false},
		{"de0fac2e4500dabe0009e67214ff5f5447ce83dd", false}, // SHA
		{"v", false},
		{"v1.2.3-rc1", false}, // pre-release rejected (out of scope)
		{"v1.2.3+build", false},
		{"latest", false},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			if got := actions.IsTagRef(tt.ref); got != tt.want {
				t.Errorf("IsTagRef(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

func TestMajorOf(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"v1", "v1"},
		{"v2", "v2"},
		{"v3.1", "v3"},
		{"v4.2.0", "v4"},
		{"1", "v1"},
		{"1.2", "v1"},
		{"1.2.3", "v1"},
		{"main", ""},
		{"", ""},
		{"abc123", ""},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			if got := actions.MajorOf(tt.ref); got != tt.want {
				t.Errorf("MajorOf(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

func TestMatchesMajor(t *testing.T) {
	tests := []struct {
		pin          string
		catalogMajor string
		want         bool
	}{
		{"v2", "v2", true},
		{"v2.3", "v2", true},
		{"v2.3.1", "v2", true},
		{"2", "v2", true},
		{"v3", "v2", false},
		{"main", "v2", false},
		{"", "v2", false},
		{"v11", "v1", false}, // major boundary
		{"v1", "v11", false},
	}
	for _, tt := range tests {
		t.Run(tt.pin+"_"+tt.catalogMajor, func(t *testing.T) {
			if got := actions.MatchesMajor(tt.pin, tt.catalogMajor); got != tt.want {
				t.Errorf("MatchesMajor(%q, %q) = %v, want %v", tt.pin, tt.catalogMajor, got, tt.want)
			}
		})
	}
}
