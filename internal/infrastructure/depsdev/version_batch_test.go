package depsdev

import "testing"

func TestBuildPURLFromVersionKey(t *testing.T) {
	tests := []struct {
		name string
		vk   DependencyVersionKey
		want string
	}{
		{
			name: "npm package",
			vk:   DependencyVersionKey{System: "NPM", Name: "qs", Version: "6.5.5"},
			want: "pkg:npm/qs@6.5.5",
		},
		{
			name: "npm scoped package",
			vk:   DependencyVersionKey{System: "NPM", Name: "@types/node", Version: "18.0.0"},
			want: "pkg:npm/@types/node@18.0.0",
		},
		{
			name: "cargo package",
			vk:   DependencyVersionKey{System: "CARGO", Name: "serde", Version: "1.0.200"},
			want: "pkg:cargo/serde@1.0.200",
		},
		{
			name: "pypi package",
			vk:   DependencyVersionKey{System: "PYPI", Name: "requests", Version: "2.28.0"},
			want: "pkg:pypi/requests@2.28.0",
		},
		{
			name: "maven package with colon separator",
			vk:   DependencyVersionKey{System: "MAVEN", Name: "org.slf4j:slf4j-api", Version: "2.0.16"},
			want: "pkg:maven/org.slf4j/slf4j-api@2.0.16",
		},
		{
			name: "maven package without colon",
			vk:   DependencyVersionKey{System: "MAVEN", Name: "commons-io", Version: "2.11.0"},
			want: "pkg:maven/commons-io@2.11.0",
		},
		{
			name: "unsupported system returns empty",
			vk:   DependencyVersionKey{System: "GO", Name: "github.com/gin-gonic/gin", Version: "v1.10.0"},
			want: "",
		},
		{
			name: "lowercase system also works",
			vk:   DependencyVersionKey{System: "npm", Name: "lodash", Version: "4.17.21"},
			want: "pkg:npm/lodash@4.17.21",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPURLFromVersionKey(tt.vk)
			if got != tt.want {
				t.Errorf("buildPURLFromVersionKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
