package i18n

import (
	"regexp"
	"strings"
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

// The private-cloud-media-server page is about one's own files: none of its
// texts says torrent in any language.
func TestPrivateCloudMediaServerSaysNoTorrent(t *testing.T) {
	torrent := regexp.MustCompile(`(?i)torrent|торрент|种子`)
	locales := localeFiles(t)
	if len(locales) != 12 {
		t.Fatalf("%d locales, want 12", len(locales))
	}
	for lang, msgs := range locales {
		n := 0
		for k, v := range msgs {
			if !strings.HasPrefix(k, "tool.privateCloudMediaServer.") {
				continue
			}
			n++
			if torrent.MatchString(v) {
				t.Errorf("%s: %s says torrent: %q", lang, k, v)
			}
		}
		if n == 0 {
			t.Errorf("%s: no tool.privateCloudMediaServer.* keys", lang)
		}
	}
}

// Around that copy the page renders these in place of the home page's torrent
// texts (views/index.html, partials/about.html under Tool.Uploads): the drop
// zone, its badge, the folder link under it, the heading over the related
// pages, the closing CTA, and the home texts it shares. None of them says
// torrent either.
func TestOwnFilesFrameSaysNoTorrent(t *testing.T) {
	keys := []string{"home.dropzoneOwnFiles", "home.dropzoneUploadsHint", "home.sizeLimitOwnFiles",
		"home.sizeLimitOwnFilesUpsell", "upload.folder", "tool.related.more",
		"about.cta.title", "about.cta.ownFiles", "about.cta.dropFiles", "about.cta.button"}
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
