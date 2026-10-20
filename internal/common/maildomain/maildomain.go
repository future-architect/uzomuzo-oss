// Package maildomain extracts and classifies the domain of an e-mail address.
package maildomain

import (
	"strings"

	"golang.org/x/net/publicsuffix"
)

const (
	maxDNSHostLength  = 253
	maxDNSLabelLength = 63
)

// Host returns the host of an already normalized e-mail address, or "" when
// the address has no local part or the host is not a valid DNS name.
func Host(email string) string {
	at := strings.LastIndexByte(email, '@')
	if at < 1 || at == len(email)-1 {
		return ""
	}
	host := email[at+1:]
	if !validHost(host) {
		return ""
	}
	return host
}

// Registrable returns the registrable domain (eTLD+1) of host and whether its
// public suffix is a private one, such as github.io.
func Registrable(host string) (string, bool) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	suffix, icann := publicsuffix.PublicSuffix(host)
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil || suffix == "" {
		return "", false
	}
	return domain, !icann && suffix != host[strings.LastIndex(host, ".")+1:]
}

func validHost(host string) bool {
	if len(host) > maxDNSHostLength || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > maxDNSLabelLength || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}
