package main

import (
	"strings"
	"testing"
)

func TestReplaceBlock(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		id        string
		newBlock  string
		fenceLang string
		want      string
		wantErr   string
	}{
		{
			name:      "basic replacement",
			content:   "before\n<!-- begin:output:test-id -->\n```text\nold content\n```\n<!-- end:output:test-id -->\nafter\n",
			id:        "test-id",
			newBlock:  "new content",
			fenceLang: "text",
			want:      "before\n<!-- begin:output:test-id -->\n```text\nnew content\n```\n<!-- end:output:test-id -->\nafter\n",
		},
		{
			name:      "marker not found",
			content:   "no markers here",
			id:        "missing",
			newBlock:  "content",
			fenceLang: "text",
			wantErr:   "not found",
		},
		{
			name:      "json fence language",
			content:   "<!-- begin:output:json-block -->\n```json\n{}\n```\n<!-- end:output:json-block -->",
			id:        "json-block",
			newBlock:  `{"key": "value"}`,
			fenceLang: "json",
			want:      "<!-- begin:output:json-block -->\n```json\n{\"key\": \"value\"}\n```\n<!-- end:output:json-block -->",
		},
		{
			name:      "output containing dollar signs preserved",
			content:   "<!-- begin:output:dollar -->\n```text\nold\n```\n<!-- end:output:dollar -->",
			id:        "dollar",
			newBlock:  "price is $1.00 and $2",
			fenceLang: "text",
			want:      "<!-- begin:output:dollar -->\n```text\nprice is $1.00 and $2\n```\n<!-- end:output:dollar -->",
		},
		{
			name:      "duplicate begin markers",
			content:   "<!-- begin:output:dup -->\n```text\na\n```\n<!-- end:output:dup -->\n<!-- begin:output:dup -->\n```text\nb\n```\n<!-- end:output:dup -->",
			id:        "dup",
			newBlock:  "new",
			fenceLang: "text",
			wantErr:   "duplicate",
		},
		{
			name:      "missing end marker",
			content:   "<!-- begin:output:noend -->\n```text\ncontent\n```\n",
			id:        "noend",
			newBlock:  "new",
			fenceLang: "text",
			wantErr:   "end marker",
		},
		{
			name:      "empty new block",
			content:   "<!-- begin:output:empty -->\n```text\nold\n```\n<!-- end:output:empty -->",
			id:        "empty",
			newBlock:  "",
			fenceLang: "text",
			want:      "<!-- begin:output:empty -->\n```text\n\n```\n<!-- end:output:empty -->",
		},
		{
			name:      "block with nested code fences",
			content:   "<!-- begin:output:nested -->\n```text\nold\n```\n<!-- end:output:nested -->",
			id:        "nested",
			newBlock:  "line1\n```\ninner fence\n```\nline2",
			fenceLang: "text",
			want:      "<!-- begin:output:nested -->\n```text\nline1\n```\ninner fence\n```\nline2\n```\n<!-- end:output:nested -->",
		},
		{
			name:      "id with special regex characters",
			content:   "<!-- begin:output:foo.bar+baz -->\n```text\nold\n```\n<!-- end:output:foo.bar+baz -->",
			id:        "foo.bar+baz",
			newBlock:  "new",
			fenceLang: "text",
			want:      "<!-- begin:output:foo.bar+baz -->\n```text\nnew\n```\n<!-- end:output:foo.bar+baz -->",
		},
		{
			name:      "content unchanged returns same string",
			content:   "<!-- begin:output:same -->\n```text\nsame content\n```\n<!-- end:output:same -->",
			id:        "same",
			newBlock:  "same content",
			fenceLang: "text",
			want:      "<!-- begin:output:same -->\n```text\nsame content\n```\n<!-- end:output:same -->",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := replaceBlock(tt.content, tt.id, tt.newBlock, tt.fenceLang)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("mismatch:\n--- want ---\n%s\n--- got ---\n%s", tt.want, got)
			}
		})
	}
}
