package i18n

import (
	"regexp"
	"testing"
)

// The Save to Vault texts are said of an upload too, so none of them says
// torrent in any language: what is saved is "this", "the files".
func TestVaultPledgeTextsSayNoTorrent(t *testing.T) {
	keys := []string{"vault.hasPoints", "vault.noPoints", "vault.unlimitedPoints", "vault.torrentSize",
		"vault.alreadyStored", "vault.pledgeSuccess", "vault.error", "vault.removeError"}
	torrent := regexp.MustCompile(`(?i)torrent|торрент|种子`)
	locales := localeFiles(t)
	if len(locales) != 12 {
		t.Fatalf("%d locales, want 12", len(locales))
	}
	for lang, msgs := range locales {
		for _, k := range keys {
			v, ok := msgs[k]
			if !ok {
				t.Fatalf("%s: no %s", lang, k)
			}
			if torrent.MatchString(v) {
				t.Errorf("%s: %s says torrent: %q", lang, k, v)
			}
		}
	}
}
