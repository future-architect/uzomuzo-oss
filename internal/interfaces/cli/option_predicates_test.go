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
			t.Errorf("%s: got %v want %v (opts=%+v)", c.name, got, c.want, c.in)
		}
	}
}
