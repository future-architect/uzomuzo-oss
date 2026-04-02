package cyclonedx_test

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/cyclonedx"
)

func TestIsCycloneDXJSON(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "valid CycloneDX header",
			data: []byte(`{"bomFormat": "CycloneDX", "specVersion": "1.4"}`),
			want: true,
		},
		{
			name: "bomFormat in components",
			data: []byte(`{"bomFormat":"CycloneDX","components":[]}`),
			want: true,
		},
		{
			name: "plain JSON without bomFormat",
			data: []byte(`{"name": "example", "version": "1.0"}`),
			want: false,
		},
		{
			name: "JSON with bomFormat but wrong value",
			data: []byte(`{"bomFormat": "SPDX", "specVersion": "2.3"}`),
			want: false,
		},
		{
			name: "empty data",
			data: []byte{},
			want: false,
		},
		{
			name: "short non-JSON data",
			data: []byte("hello"),
			want: false,
		},
		{
			name: "bomFormat beyond sniff prefix is missed",
			data: append(make([]byte, 600), []byte(`"bomFormat"`)...),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cyclonedx.IsCycloneDXJSON(tt.data)
			if got != tt.want {
				t.Errorf("IsCycloneDXJSON() = %v, want %v", got, tt.want)
			}
		})
	}
}
