package stremio

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/handlers/trial"
	"github.com/webtor-io/web-ui/services/i18n"
)

// These guard the committed clips in pub/stremio against the three ways they
// go wrong without anything else noticing: a locale without a clip (its
// viewers silently get the English one), a copy edit that was never
// re-rendered (the clip keeps saying the old thing), and an encode a TV
// player cannot start.

// paywallKeys and the digest format are scripts/stremio_paywall_video's.
var paywallKeys = []string{"title", "needsPlan", "trial", "open", "sameAccount"}

const (
	paywallSite         = "https://webtor.io"
	paywallDigestMarker = "webtor-paywall-src:"
	paywallMaxBytes     = 600 << 10
)

func localeTexts(t *testing.T) map[string]map[string]string {
	t.Helper()
	paths, err := filepath.Glob("../../locales/??.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no locales: %v", err)
	}
	out := map[string]map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var d map[string]any
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		texts := map[string]string{}
		for _, k := range paywallKeys {
			v, _ := d["stremio.paywall."+k].(string)
			texts[k] = v
		}
		out[strings.TrimSuffix(filepath.Base(p), ".json")] = texts
	}
	return out
}

// paywallQR is what the clip's QR code must encode: the /trial short link in
// the clip's language with the utm query the trial handler knows.
func paywallQR(lang string) string {
	return paywallSite + i18n.LangPath(lang, "/trial") + "?" + trial.PaywallUTM
}

func paywallSourceDigest(texts map[string]string, lang string) string {
	h := sha256.New()
	for _, k := range paywallKeys {
		fmt.Fprintf(h, "stremio.paywall.%s=%s\n", k, texts[k])
	}
	fmt.Fprintf(h, "qr=%s\n", paywallQR(lang))
	return hex.EncodeToString(h.Sum(nil))
}

func TestEveryLocaleHasAPaywallClipAndItsCopy(t *testing.T) {
	for lang, texts := range localeTexts(t) {
		for _, k := range paywallKeys {
			if strings.TrimSpace(texts[k]) == "" {
				t.Errorf("locales/%s.json: stremio.paywall.%s is missing", lang, k)
			}
		}
		if _, err := os.Stat(filepath.Join("../..", paywallDir, "paywall-"+lang+".mp4")); err != nil {
			t.Errorf("%s: no paywall clip for this locale (%v) — render it: see docs/stremio.md", lang, err)
		}
	}
}

var digestRe = regexp.MustCompile(regexp.QuoteMeta(paywallDigestMarker) + `([0-9a-f]{64})`)

// The clip records a digest of the text and QR link it was drawn from. A
// changed translation, or a changed utm query in the trial handler, without
// a re-render fails here.
func TestPaywallClipsAreRenderedFromTheCurrentCopy(t *testing.T) {
	for lang, texts := range localeTexts(t) {
		b, err := os.ReadFile(filepath.Join("../..", paywallDir, "paywall-"+lang+".mp4"))
		if err != nil {
			continue // reported by TestEveryLocaleHasAPaywallClipAndItsCopy
		}
		m := digestRe.FindSubmatch(b)
		if m == nil {
			t.Errorf("paywall-%s.mp4 carries no source digest — was it rendered by scripts/stremio_paywall_video?", lang)
			continue
		}
		if got, want := string(m[1]), paywallSourceDigest(texts, lang); got != want {
			t.Errorf("paywall-%s.mp4 was rendered from other text or another QR link than locales/%s.json and %s now give — re-render it (docs/stremio.md)", lang, lang, paywallQR(lang))
		}
	}
}

// The clip plays in Stremio's own player on every platform — ExoPlayer on
// Android TV, libmpv on desktop, AVPlayer on Apple TV and iOS — so it is the
// common subset: H.264 High at level 3.1 or lower, 1280x720, AAC, the index
// (moov) before the data so playback starts before the download ends.
func TestPaywallClipsPlayEverywhere(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join("../..", paywallDir, "paywall-*.mp4"))
	if len(paths) == 0 {
		t.Fatal("no clips")
	}
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if len(b) > paywallMaxBytes {
				t.Errorf("%d bytes, the budget is %d", len(b), paywallMaxBytes)
			}
			boxes := topLevelBoxes(t, b)
			if len(boxes) == 0 || boxes[0].typ != "ftyp" {
				t.Fatalf("top-level boxes %v: an MP4 starts with ftyp", boxes)
			}
			moov, mdat := -1, -1
			for i, bx := range boxes {
				switch bx.typ {
				case "moov":
					moov = i
				case "mdat":
					mdat = i
				}
			}
			if moov < 0 || mdat < 0 || moov > mdat {
				t.Fatalf("top-level boxes %v: moov must come before mdat (faststart)", boxes)
			}
			mv := b[boxes[moov].start : boxes[moov].start+boxes[moov].size]

			avc1 := bytes.Index(mv, []byte("avc1"))
			avcC := bytes.Index(mv, []byte("avcC"))
			if avc1 < 0 || avcC < 0 {
				t.Fatal("no H.264 (avc1/avcC) sample entry")
			}
			// VisualSampleEntry: 8 bytes of header, then 24 bytes before
			// the 16-bit width and height.
			w := binary.BigEndian.Uint16(mv[avc1+28:])
			h := binary.BigEndian.Uint16(mv[avc1+30:])
			if w != 1280 || h != 720 {
				t.Errorf("%dx%d, want 1280x720", w, h)
			}
			// AVCDecoderConfigurationRecord: version, profile, compat, level.
			profile, level := mv[avcC+5], mv[avcC+7]
			if profile != 100 && profile != 77 {
				t.Errorf("H.264 profile_idc %d, want High (100) or Main (77)", profile)
			}
			if level > 31 {
				t.Errorf("H.264 level %d.%d, want 3.1 or lower", level/10, level%10)
			}
			if !bytes.Contains(mv, []byte("mp4a")) {
				t.Error("no AAC (mp4a) track: some TV players refuse a video without audio")
			}
			mvhd := bytes.Index(mv, []byte("mvhd"))
			if mvhd < 0 {
				t.Fatal("no mvhd")
			}
			var timescale, duration uint64
			if mv[mvhd+4] == 1 {
				timescale = uint64(binary.BigEndian.Uint32(mv[mvhd+24:]))
				duration = binary.BigEndian.Uint64(mv[mvhd+28:])
			} else {
				timescale = uint64(binary.BigEndian.Uint32(mv[mvhd+16:]))
				duration = uint64(binary.BigEndian.Uint32(mv[mvhd+20:]))
			}
			if timescale == 0 {
				t.Fatal("mvhd timescale 0")
			}
			if sec := float64(duration) / float64(timescale); sec < 10 || sec > 14 {
				t.Errorf("%.1f s, want 10–14 s", sec)
			}
		})
	}
}

// Copy rules for the clip (docs/stremio.md): no numbers — trial length,
// speed and price are the catalog's and the clip cannot follow them; no
// promise of instant playback; and, the owner's decision, no pointer to
// connecting one's own debrid backend.
func TestPaywallCopyRules(t *testing.T) {
	banned := []string{"torbox", "real-debrid", "realdebrid", "debrid",
		"instant", "мгновен", "sofort", "natychmiast", "anında", "okamžit", "meteen"}
	for lang, texts := range localeTexts(t) {
		for _, k := range paywallKeys {
			v := texts[k]
			if strings.ContainsAny(v, "0123456789") {
				t.Errorf("%s stremio.paywall.%s quotes a number: %q", lang, k, v)
			}
			for _, w := range banned {
				if strings.Contains(strings.ToLower(v), w) {
					t.Errorf("%s stremio.paywall.%s says %q: %q", lang, k, w, v)
				}
			}
		}
	}
}

type mp4Box struct {
	typ         string
	start, size int
}

func (b mp4Box) String() string { return b.typ }

func topLevelBoxes(t *testing.T, b []byte) []mp4Box {
	t.Helper()
	var out []mp4Box
	for off := 0; off+8 <= len(b); {
		size := int(binary.BigEndian.Uint32(b[off:]))
		typ := string(b[off+4 : off+8])
		switch size {
		case 0:
			size = len(b) - off
		case 1:
			if off+16 > len(b) {
				t.Fatalf("truncated largesize box %q", typ)
			}
			size = int(binary.BigEndian.Uint64(b[off+8:]))
		}
		if size < 8 || off+size > len(b) {
			t.Fatalf("bad box %q of size %d at %d", typ, size, off)
		}
		out = append(out, mp4Box{typ: typ, start: off, size: size})
		off += size
	}
	return out
}
