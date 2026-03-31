package github

import (
	"testing"
)

func TestForkSourceFromRepoInfo(t *testing.T) {
	tests := []struct {
		name string
		info *RepositoryInfo
		want string
	}{
		{
			name: "nil input",
			info: nil,
			want: "",
		},
		{
			name: "not a fork",
			info: &RepositoryInfo{IsFork: false},
			want: "",
		},
		{
			name: "fork with source",
			info: &RepositoryInfo{
				IsFork: true,
				Source:  &SourceInfo{NameWithOwner: "original-owner/original-repo"},
			},
			want: "original-owner/original-repo",
		},
		{
			name: "fork without source data (private parent)",
			info: &RepositoryInfo{
				IsFork: true,
				Source:  nil,
			},
			want: "",
		},
		{
			name: "fork with empty source name",
			info: &RepositoryInfo{
				IsFork: true,
				Source:  &SourceInfo{NameWithOwner: ""},
			},
			want: "",
		},
		{
			name: "not a fork but source present (should not happen, but guard)",
			info: &RepositoryInfo{
				IsFork: false,
				Source:  &SourceInfo{NameWithOwner: "some-owner/some-repo"},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := forkSourceFromRepoInfo(tt.info)
			if got != tt.want {
				t.Errorf("forkSourceFromRepoInfo() = %q, want %q", got, tt.want)
			}
		})
	}
}
