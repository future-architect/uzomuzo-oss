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
			name: "fork with parent",
			info: &RepositoryInfo{
				IsFork: true,
				Parent: &ParentInfo{NameWithOwner: "original-owner/original-repo"},
			},
			want: "original-owner/original-repo",
		},
		{
			name: "fork without parent data (private parent)",
			info: &RepositoryInfo{
				IsFork: true,
				Parent: nil,
			},
			want: "",
		},
		{
			name: "fork with empty parent name",
			info: &RepositoryInfo{
				IsFork: true,
				Parent: &ParentInfo{NameWithOwner: ""},
			},
			want: "",
		},
		{
			name: "not a fork but parent present (should not happen, but guard)",
			info: &RepositoryInfo{
				IsFork: false,
				Parent: &ParentInfo{NameWithOwner: "some-owner/some-repo"},
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
