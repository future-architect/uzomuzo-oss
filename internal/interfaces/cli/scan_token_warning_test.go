package cli

import (
	"bytes"
	"strings"
	"testing"
)

func Test_warnIfNoToken(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantWarn bool
	}{
		{name: "no token warns", token: "", wantWarn: true},
		{name: "token is silent", token: "ghp_example", wantWarn: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			warnIfNoToken(&buf, tt.token)
			got := buf.String()
			if !tt.wantWarn {
				if got != "" {
					t.Fatalf("output = %q, want none", got)
				}
				return
			}
			for _, want := range []string{"GITHUB_TOKEN is not set", "archived", "gh auth token"} {
				if !strings.Contains(got, want) {
					t.Errorf("output = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}
