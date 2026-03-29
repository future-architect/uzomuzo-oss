package gomod

import (
	"context"
	"testing"
)

// FuzzGoModParse fuzzes go.mod file parsing for panics and crashes.
func FuzzGoModParse(f *testing.F) {
	seeds := []string{
		"module example.com/foo\n\ngo 1.21\n\nrequire (\n\tgithub.com/pkg/errors v0.9.1\n)\n",
		"module example.com/foo\n\ngo 1.21\n\nrequire github.com/pkg/errors v0.9.1\n",
		"module example.com/foo\n\ngo 1.21\n\nrequire (\n\tgithub.com/pkg/errors v0.9.1 // indirect\n)\n",
		"module example.com/foo\n\ngo 1.21\n\nreplace github.com/old => github.com/new v1.0.0\n\nrequire github.com/old v0.1.0\n",
		"module example.com/foo\n\ngo 1.21\n\nreplace github.com/old => ./local\n\nrequire github.com/old v0.1.0\n",
		"",
		"not a go.mod",
		"module",
		"module \n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	p := &Parser{}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = p.Parse(context.Background(), data)
	})
}
