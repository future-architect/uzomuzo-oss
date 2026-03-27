package cli

import "testing"

func TestParseLineRange(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		start   int
		end     int
		wantErr bool
	}{
		{"basic", "1:10", 1, 10, false},
		{"open end", "5:", 5, 0, false},
		{"single line", "10:10", 10, 10, false},
		{"leading zeros", "001:002", 1, 2, false},
		{"missing start", ":10", 0, 0, true},
		{"zero start", "0:10", 0, 0, true},
		{"end lt start", "10:9", 0, 0, true},
		{"non numeric start", "abc:10", 0, 0, true},
		{"non numeric end", "5:xyz", 0, 0, true},
		{"missing colon", "5", 0, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end, err := ParseLineRange(c.in)
			if c.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q", c.in)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", c.in, err)
				return
			}
			if start != c.start || end != c.end {
				t.Errorf("got (%d,%d) want (%d,%d)", start, end, c.start, c.end)
			}
		})
	}
}

func TestValidateLineRange(t *testing.T) {
	cases := []struct {
		name    string
		opts    ProcessingOptions
		wantErr bool
	}{
		{
			name:    "valid range",
			opts:    ProcessingOptions{LineStart: 1, LineEnd: 10},
			wantErr: false,
		},
		{
			name:    "valid open end",
			opts:    ProcessingOptions{LineStart: 5, LineEnd: 0},
			wantErr: false,
		},
		{
			name:    "valid single line",
			opts:    ProcessingOptions{LineStart: 10, LineEnd: 10},
			wantErr: false,
		},
		{
			name:    "no range specified",
			opts:    ProcessingOptions{LineStart: 0, LineEnd: 0},
			wantErr: false,
		},
		{
			name:    "negative start",
			opts:    ProcessingOptions{LineStart: -1, LineEnd: 10},
			wantErr: true,
		},
		{
			name:    "negative end",
			opts:    ProcessingOptions{LineStart: 1, LineEnd: -1},
			wantErr: true,
		},
		{
			name:    "end less than start",
			opts:    ProcessingOptions{LineStart: 10, LineEnd: 5},
			wantErr: true,
		},
		{
			name:    "both negative",
			opts:    ProcessingOptions{LineStart: -5, LineEnd: -1},
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateLineRange(&c.opts)
			if c.wantErr {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
