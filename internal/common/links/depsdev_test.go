package links

import "testing"

func TestBuildDepsDevURL(t *testing.T) {
	tests := []struct {
		eco, name, want string
	}{
		{"npm", "express", "https://deps.dev/npm/express"},
		{"PyPI", "requests", "https://deps.dev/pypi/requests"},
		{"golang", "golang.org/x/sys", "https://deps.dev/go/golang.org/x/sys"},
		{"gem", "rails", "https://deps.dev/rubygems/rails"},
		{"composer", "laravel/framework", "https://deps.dev/packagist/laravel/framework"},
		{"cargo", "serde", "https://deps.dev/cargo/serde"},
		{"", "express", ""},
		{"npm", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.eco+"/"+tt.name, func(t *testing.T) {
			got := BuildDepsDevURL(tt.eco, tt.name)
			if got != tt.want {
				t.Errorf("BuildDepsDevURL(%q, %q) = %q, want %q", tt.eco, tt.name, got, tt.want)
			}
		})
	}
}

func TestBuildDepsDevVersionURL(t *testing.T) {
	tests := []struct {
		eco, name, ver, want string
	}{
		{"npm", "express", "4.18.2", "https://deps.dev/npm/express/4.18.2"},
		{"golang", "golang.org/x/sys", "v0.1.0", "https://deps.dev/go/golang.org/x/sys/v0.1.0"},
		{"gem", "rails", "7.0.0", "https://deps.dev/rubygems/rails/7.0.0"},
		{"composer", "laravel/framework", "10.0.0", "https://deps.dev/packagist/laravel/framework/10.0.0"},
		{"", "express", "1.0.0", ""},
		{"npm", "", "1.0.0", ""},
		{"npm", "express", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.eco+"/"+tt.name, func(t *testing.T) {
			got := BuildDepsDevVersionURL(tt.eco, tt.name, tt.ver)
			if got != tt.want {
				t.Errorf("BuildDepsDevVersionURL(%q, %q, %q) = %q, want %q", tt.eco, tt.name, tt.ver, got, tt.want)
			}
		})
	}
}
