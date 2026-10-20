package maildomain

import "testing"

// FuzzHost checks host parsing and public-suffix handling on arbitrary strings.
func FuzzHost(f *testing.F) {
	f.Add("a@mail.example.co.uk")
	f.Add("me.github.io")
	f.Add("..")
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 {
			return
		}
		_ = Host(s)
		_, _ = Registrable(s)
	})
}
