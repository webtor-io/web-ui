package models

import (
	"strings"
	"testing"
)

// TestAliasCodeIsLongEnoughToNotBeGuessed pins the keyspace.
//
// A /s/<code> is a capability URL: resolving one either proxies the owner's
// WebDAV tree or 301s with their raw access token in the Location header, and
// the resolving route is anonymous with no rate limit. At the original 6
// characters the space was 36^6 ≈ 2^31, and the work to hit *some* live alias
// is that divided by the number of aliases in existence — minutes, not years.
//
// Negative control: set aliasCodeLen back to 6 and this fails.
func TestAliasCodeIsLongEnoughToNotBeGuessed(t *testing.T) {
	const minLen = 16
	if aliasCodeLen < minLen {
		t.Fatalf("aliasCodeLen = %d, want at least %d (~%d bits)", aliasCodeLen, minLen, 82)
	}
	code, err := randomAlphaNum(aliasCodeLen)
	if err != nil {
		t.Fatalf("randomAlphaNum: %v", err)
	}
	if len(code) != aliasCodeLen {
		t.Errorf("code %q has length %d, want %d", code, len(code), aliasCodeLen)
	}
	for _, r := range code {
		if !strings.ContainsRune(string(alphaNum), r) {
			t.Errorf("code %q contains %q, which is outside the alphabet", code, r)
		}
	}
}

// TestAliasCodesDoNotRepeat is a cheap smoke test that the generator is drawing
// fresh randomness rather than returning a constant or a seeded sequence.
func TestAliasCodesDoNotRepeat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		code, err := randomAlphaNum(aliasCodeLen)
		if err != nil {
			t.Fatalf("randomAlphaNum: %v", err)
		}
		if seen[code] {
			t.Fatalf("code %q was generated twice in 1000 draws", code)
		}
		seen[code] = true
	}
}

// TestAliasCodeAlphabetIsNotSkewed guards the rejection sampling. 256 is not a
// multiple of 36, so a plain modulo would make the first four letters roughly
// 11% likelier than the rest — a measurable bias in a value whose only job is
// to be unguessable.
//
// The bound is loose on purpose: this must catch a systematic 11% skew, not
// flag ordinary sampling noise.
func TestAliasCodeAlphabetIsNotSkewed(t *testing.T) {
	const draws = 4000
	counts := map[rune]int{}
	for i := 0; i < draws; i++ {
		code, err := randomAlphaNum(aliasCodeLen)
		if err != nil {
			t.Fatalf("randomAlphaNum: %v", err)
		}
		for _, r := range code {
			counts[r]++
		}
	}
	total := draws * aliasCodeLen
	expected := float64(total) / float64(len(alphaNum))
	for _, r := range string(alphaNum) {
		got := float64(counts[r])
		if got < expected*0.85 || got > expected*1.15 {
			t.Errorf("symbol %q appeared %.0f times, expected about %.0f — the alphabet is skewed", r, got, expected)
		}
	}
}
