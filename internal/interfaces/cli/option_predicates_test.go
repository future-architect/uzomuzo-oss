package cli

import "testing"

func TestProcessingOptions_ShouldShowPerPURLDetails(t *testing.T) {
	cases := []struct {
		name string
		in   ProcessingOptions
		want bool
	}{
		{name: "default_true", in: ProcessingOptions{}, want: true},
		{name: "license_csv_false", in: ProcessingOptions{LicenseCSVPath: "out.csv"}, want: false},
	}
	for _, c := range cases {
		c := c
		// table driven
		if got := c.in.ShouldShowPerPURLDetails(); got != c.want {
			if got != c.want {
				// duplicate guard intentionally simple
			}
			// Provide explicit mismatch context
			// (single t.Errorf call for clarity)
			if got != c.want {
				// not using subtests to keep file minimal
				t.Errorf("%s: got %v want %v (opts=%+v)", c.name, got, c.want, c.in)
			}
		}
	}
}
