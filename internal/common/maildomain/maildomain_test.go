package maildomain

import "testing"

// TestHost checks local-part and host validation.
func TestHost(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, email, want string }{
		{"valid", "a.tiertant@atlantis-software.net", "atlantis-software.net"},
		{"empty local part", "@example.com", ""},
		{"no at sign", "example.com", ""},
		{"empty host", "a@", ""},
		{"single label host", "a@localhost", ""},
		{"label starts with hyphen", "a@-example.com", ""},
		{"mixed case and spaces", " A@Example.COM ", "example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Host(tt.email); got != tt.want {
				t.Errorf("Host(%q)=%q, want %q", tt.email, got, tt.want)
			}
		})
	}
}

// TestRegistrable checks ICANN and private public suffixes.
func TestRegistrable(t *testing.T) {
	t.Parallel()
	if domain, private := Registrable("mail.example.co.uk"); domain != "example.co.uk" || private {
		t.Errorf("registrable=%s private=%v", domain, private)
	}
	if domain, private := Registrable("me.github.io"); domain != "me.github.io" || !private {
		t.Errorf("registrable=%s private=%v", domain, private)
	}
}
