package pypi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetProject_Yanked pins the project-level yank fields. PyPI orders
// non-yanked releases first when picking the release it reports under info, so
// info.yanked is true only when every release of the project is yanked.
func TestGetProject_Yanked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		body       string
		wantYanked bool
		wantReason string
	}{
		{
			name:       "every release yanked, with a reason",
			body:       `{"info":{"name":"python-apt","yanked":true,"yanked_reason":"Unmaintained"}}`,
			wantYanked: true,
			wantReason: "Unmaintained",
		},
		{
			name:       "every release yanked, reason absent",
			body:       `{"info":{"name":"conda-build","yanked":true,"yanked_reason":null}}`,
			wantYanked: true,
			wantReason: "",
		},
		{
			name:       "a non-yanked release exists",
			body:       `{"info":{"name":"pydantic-extra-types","yanked":false,"yanked_reason":null}}`,
			wantYanked: false,
		},
		{
			name:       "fields absent entirely",
			body:       `{"info":{"name":"requests","summary":"s"}}`,
			wantYanked: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintln(w, tt.body)
			}))
			defer srv.Close()

			c := NewClient()
			c.SetBaseURL(srv.URL)
			c.SetCacheTTL(0)

			info, found, err := c.GetProject(context.Background(), "pkg")
			if err != nil {
				t.Fatalf("GetProject failed: %v", err)
			}
			if !found || info == nil {
				t.Fatalf("expected found project, got found=%v info=%v", found, info)
			}
			if info.Yanked != tt.wantYanked {
				t.Errorf("Yanked: got %v, want %v", info.Yanked, tt.wantYanked)
			}
			if info.YankedReason != tt.wantReason {
				t.Errorf("YankedReason: got %q, want %q", info.YankedReason, tt.wantReason)
			}
		})
	}
}
