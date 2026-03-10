package purl

import "testing"

func TestVersionlessPreserveCase(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "npm_mixed_case",
			input: "pkg:npm/React@18.3.1",
			want:  "pkg:npm/React",
		},
		{
			name:  "maven_uppercase_group",
			input: "pkg:maven/Org.Example/Lib@1.0.0",
			want:  "pkg:maven/Org.Example/Lib",
		},
		{
			name:  "maven_collapsed_coordinate",
			input: "pkg:maven/artifact@1.0.0",
			want:  "pkg:maven/artifact",
		},
		{
			name:  "with_qualifiers",
			input: "pkg:npm/Package@1.0.0?foo=bar",
			want:  "pkg:npm/Package?foo=bar",
		},
		{
			name:  "with_fragment",
			input: "pkg:gem/Rails@7.0#dev",
			want:  "pkg:gem/Rails#dev",
		},
		{
			name:  "already_versionless",
			input: "pkg:gem/Rails",
			want:  "pkg:gem/Rails",
		},
		{
			name:  "empty_string",
			input: "",
			want:  "",
		},
		{
			name:  "invalid_purl",
			input: "not-a-purl",
			want:  "not-a-purl", // Return original on parse failure
		},
		{
			name:  "pypi_underscore_case",
			input: "pkg:pypi/Django_Rest_Framework@3.14.0",
			want:  "pkg:pypi/Django_Rest_Framework",
		},
		{
			name:  "golang_mixed_case_path",
			input: "pkg:golang/github.com/Sirupsen/logrus@v1.9.0",
			want:  "pkg:golang/github.com/Sirupsen/logrus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VersionlessPreserveCase(tt.input)
			if got != tt.want {
				t.Errorf("VersionlessPreserveCase(%q) = %q, want %q",
					tt.input, got, tt.want)
			}
		})
	}
}
