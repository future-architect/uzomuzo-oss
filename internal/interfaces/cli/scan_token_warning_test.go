package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
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

// TestRunScanTokenWarningStream pins that RunScan itself warns, and on stderr, because
// stdout carries JSON, CSV or SARIF. The detector fails so the scan stops before any
// network call.
func TestRunScanTokenWarningStream(t *testing.T) {
	const warning = "GITHUB_TOKEN is not set"
	tests := []struct {
		name       string
		token      string
		wantStderr bool
	}{
		{name: "no token", token: "", wantStderr: true},
		{name: "token", token: "ghp_example", wantStderr: false},
	}
	detect := func(string, map[string]depparser.DependencyParser) (depparser.DependencyParser, []byte, error) {
		return nil, nil, errors.New("stop before the network")
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &domaincfg.Config{}
			cfg.GitHub.Token = tt.token
			var opts ScanOptions
			opts.Filename = "go.mod"
			var runErr error
			stdout, stderr := captureStdoutStderr(t, func() {
				runErr = RunScan(context.Background(), cfg, nil, opts, nil, detect, nil, nil, nil)
			})
			if runErr == nil || !strings.Contains(runErr.Error(), "stop before the network") {
				t.Fatalf("RunScan err = %v, want the detector's error", runErr)
			}
			if strings.Contains(stdout, warning) {
				t.Errorf("stdout = %q, must not contain the warning", stdout)
			}
			if got := strings.Contains(stderr, warning); got != tt.wantStderr {
				t.Errorf("stderr = %q, contains warning = %v, want %v", stderr, got, tt.wantStderr)
			}
		})
	}
}

func captureStdoutStderr(t *testing.T, run func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	t.Cleanup(func() {
		os.Stdout, os.Stderr = origOut, origErr
		_ = outR.Close() // test cleanup
		_ = errR.Close() // test cleanup
	})

	read := func(r io.Reader) <-chan string {
		ch := make(chan string, 1)
		go func() {
			b, _ := io.ReadAll(r) // a read error shows up as a failed assertion
			ch <- string(b)
		}()
		return ch
	}
	outCh, errCh := read(outR), read(errR)

	run()

	os.Stdout, os.Stderr = origOut, origErr
	_ = outW.Close() // ends the reader goroutine
	_ = errW.Close() // ends the reader goroutine
	return <-outCh, <-errCh
}
