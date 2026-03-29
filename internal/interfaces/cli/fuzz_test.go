package cli

import "testing"

// FuzzParseLineRange fuzzes line range parsing for panics and integer edge cases.
func FuzzParseLineRange(f *testing.F) {
	seeds := []string{
		"1:10",
		"1:",
		"100:200",
		"",
		"abc",
		"1:abc",
		"-1:10",
		"0:10",
		"10:5",
		":",
		":10",
		"1:2:3",
		"999999999999:1",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_, _, _ = ParseLineRange(input)
	})
}
