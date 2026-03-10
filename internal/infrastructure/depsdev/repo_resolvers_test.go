package depsdev

import "testing"

func TestStripTrailingVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"with_semver", "opentelemetry-sdk-extension-autoconfigure-1.28.0", "opentelemetry-sdk-extension-autoconfigure"},
		{"with_simple_version", "csec-apache-wicket-6.4", "csec-apache-wicket"},
		{"no_version", "spring-security-web", "spring-security-web"},
		{"version_only_digits", "some-lib-2", "some-lib"},
		{"trailing_non_version", "my-lib-beta", "my-lib-beta"},
		{"no_dash", "jsr250api", "jsr250api"},
		{"empty", "", ""},
		{"ends_with_dash", "foo-", "foo-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripTrailingVersion(tt.in); got != tt.want {
				t.Errorf("stripTrailingVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestHasRegistryResolver(t *testing.T) {
	tests := []struct {
		eco  string
		want bool
	}{
		{"npm", true},
		{"nuget", true},
		{"maven", true},
		{"gem", true},
		{"rubygems", true},
		{"composer", true},
		{"packagist", true},
		{"pypi", true},
		{"cargo", false},
		{"golang", false},
		{"", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.eco, func(t *testing.T) {
			if got := hasRegistryResolver(tt.eco); got != tt.want {
				t.Errorf("hasRegistryResolver(%q) = %v, want %v", tt.eco, got, tt.want)
			}
		})
	}
}
