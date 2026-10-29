package main

import (
	"context"
	"testing"

	urfcli "github.com/urfave/cli/v3"
)

func TestSARIFOptionsUseRootVersion(t *testing.T) {
	called := false
	app := &urfcli.Command{Name: "test", Version: "9.8.7", Commands: []*urfcli.Command{{Name: "scan", Flags: scanFlags(), Action: func(_ context.Context, cmd *urfcli.Command) error {
		called = true
		opts, err := buildScanOptions(cmd)
		if err != nil {
			return err
		}
		if opts.ToolVersion != "9.8.7" || opts.Format != "sarif" {
			t.Fatalf("version=%q format=%q", opts.ToolVersion, opts.Format)
		}
		return nil
	}}}}
	if err := app.Run(context.Background(), []string{"test", "scan", "--format", "sarif"}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("scan action did not run")
	}
}
