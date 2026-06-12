package ttlcache

import (
	"testing"
	"time"
)

// TestCache_ZeroValueUsable verifies that the zero value of Cache is ready to
// use (no initialisation required) and that it behaves as if caching is
// disabled (TTL = 0).
func TestCache_ZeroValueUsable(t *testing.T) {
	var c Cache[string]

	c.Set("k", "v")     // must not panic
	_, ok := c.Get("k") // must miss
	if ok {
		t.Error("Get returned hit on zero-value cache (TTL=0); want miss")
	}
}

// TestCache table-driven cases.
func TestCache(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ttl      time.Duration
		key      string
		setValue string
		getKey   string
		wantHit  bool
		wantVal  string
	}{
		{
			name:     "hit",
			ttl:      1 * time.Minute,
			key:      "k",
			setValue: "v",
			getKey:   "k",
			wantHit:  true,
			wantVal:  "v",
		},
		{
			name:     "miss_different_key",
			ttl:      1 * time.Minute,
			key:      "k1",
			setValue: "v1",
			getKey:   "k2",
			wantHit:  false,
		},
		{
			name:     "ttl_zero_disabled_set",
			ttl:      0,
			key:      "k",
			setValue: "v",
			getKey:   "k",
			wantHit:  false,
		},
		{
			name:     "ttl_negative_disabled_set",
			ttl:      -1 * time.Second,
			key:      "k",
			setValue: "v",
			getKey:   "k",
			wantHit:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var c Cache[string]
			c.SetTTL(tt.ttl)
			c.Set(tt.key, tt.setValue)
			got, ok := c.Get(tt.getKey)
			if ok != tt.wantHit {
				t.Errorf("Get(%q) hit = %v, want %v", tt.getKey, ok, tt.wantHit)
			}
			if tt.wantHit && got != tt.wantVal {
				t.Errorf("Get(%q) = %q, want %q", tt.getKey, got, tt.wantVal)
			}
		})
	}
}

// TestCache_Expiry verifies that a cached entry is considered a miss after
// its TTL has elapsed.  We use a very small TTL and sleep past it.
func TestCache_Expiry(t *testing.T) {
	t.Parallel()
	var c Cache[int]
	c.SetTTL(10 * time.Millisecond)
	c.Set("x", 42)

	// Confirm hit immediately.
	if _, ok := c.Get("x"); !ok {
		t.Fatal("Get returned miss immediately after Set; want hit")
	}

	// Sleep past TTL.
	time.Sleep(20 * time.Millisecond)

	if _, ok := c.Get("x"); ok {
		t.Error("Get returned hit after TTL expired; want miss")
	}
}

// TestCache_SetTTLToggle verifies that disabling the TTL after storing entries
// makes subsequent Gets miss.
func TestCache_SetTTLToggle(t *testing.T) {
	t.Parallel()
	var c Cache[string]
	c.SetTTL(1 * time.Minute)
	c.Set("k", "v")
	if _, ok := c.Get("k"); !ok {
		t.Fatal("Get returned miss before TTL disabled; want hit")
	}

	// Disable caching.
	c.SetTTL(0)
	if _, ok := c.Get("k"); ok {
		t.Error("Get returned hit after TTL set to 0; want miss")
	}
}
