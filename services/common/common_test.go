package common

import (
	"errors"
	"strings"
	"testing"
)

const (
	v1Hash = "08ada5a7a6183aae1e09d831df6748d566095a10"
	v2Mh   = "1220f2f69383b0695cb0942962a287b42b6fd262e2485f8572f4a4724e8c63ecefcf"
)

func TestResolveQueryHash(t *testing.T) {
	for _, tc := range []struct {
		name       string
		query      string
		hash       string
		magnetHas  []string
		magnetMiss []string
		errHas     string
	}{
		{
			name:      "plain v1 magnet",
			query:     "magnet:?xt=urn:btih:" + v1Hash + "&dn=Sintel&tr=udp%3A%2F%2Ftracker.example%3A1337",
			hash:      v1Hash,
			magnetHas: []string{"urn:btih:" + v1Hash, "dn=Sintel", "tracker.example"},
		},
		{
			// btmh listed first: the old SHA1R first-match extraction grabbed
			// 40 hex chars out of the v2 multihash and produced a bogus hash
			name:       "hybrid magnet with btmh before btih",
			query:      "magnet:?xt=urn:btmh:" + v2Mh + "&xt=urn:btih:" + v1Hash + "&tr=udp%3A%2F%2Ftracker.example%3A1337",
			hash:       v1Hash,
			magnetHas:  []string{"urn:btih:" + v1Hash, "tracker.example"},
			magnetMiss: []string{"btmh"},
		},
		{
			name:       "hybrid magnet with btih before btmh",
			query:      "magnet:?xt=urn:btih:" + v1Hash + "&xt=urn:btmh:" + v2Mh,
			hash:       v1Hash,
			magnetMiss: []string{"btmh"},
		},
		{
			name:   "v2-only magnet",
			query:  "magnet:?xt=urn:btmh:" + v2Mh,
			errHas: "v2-only",
		},
		{
			name:  "magnet route reassembly without question mark",
			query: "magnet:xt=urn:btih:" + v1Hash,
			hash:  v1Hash,
		},
		{
			name:  "uppercase hex btih",
			query: "magnet:?xt=urn:btih:" + strings.ToUpper(v1Hash),
			hash:  v1Hash,
		},
		{
			name:      "bare hash",
			query:     v1Hash,
			hash:      v1Hash,
			magnetHas: []string{"magnet:?xt=urn:btih:" + v1Hash},
		},
		{
			name:  "bare uppercase hash",
			query: strings.ToUpper(v1Hash),
			hash:  v1Hash,
		},
		{
			name:   "garbage query",
			query:  "zzzz",
			errHas: "no infohash",
		},
		{
			name:   "empty magnet",
			query:  "magnet:?dn=foo",
			errHas: "no infohash",
		},
		{
			// GET /magnet2torrent?magnet=… with the magnet's own = and & still
			// encoded: the route makes the query "magnet2torrent" + RawQuery.
			// Decoded once it parses, and keeps its name and trackers.
			name:      "magnet inside text with encoded = and &",
			query:     "magnet2torrentmagnet=magnet:?xt%3Durn:btih:" + strings.ToUpper(v1Hash) + "%26dn%3DSintel%26tr%3Dudp%253A%252F%252Ftracker.example%253A1337",
			hash:      v1Hash,
			magnetHas: []string{"urn:btih:" + v1Hash, "dn=Sintel", "tracker.example"},
		},
		{
			name:      "magnet with encoded = and &",
			query:     "magnet:?xt%3Durn:btih:" + v1Hash + "%26dn%3DSintel",
			hash:      v1Hash,
			magnetHas: []string{"dn=Sintel"},
		},
		{
			// Does not parse even decoded; the hash in it is still readable.
			name:      "magnet with a slashed scheme",
			query:     "magnet://?xt=urn:btih:" + v1Hash + "&dn=Sintel",
			hash:      v1Hash,
			magnetHas: []string{"magnet:?xt=urn:btih:" + v1Hash},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hash, magnet, err := ResolveQueryHash(tc.query)
			if tc.errHas != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got hash=%q magnet=%q", tc.errHas, hash, magnet)
				}
				if !strings.Contains(err.Error(), tc.errHas) {
					t.Fatalf("expected error containing %q, got %q", tc.errHas, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hash != tc.hash {
				t.Fatalf("expected hash %q, got %q", tc.hash, hash)
			}
			for _, s := range tc.magnetHas {
				if !strings.Contains(magnet, s) {
					t.Fatalf("expected magnet to contain %q, got %q", s, magnet)
				}
			}
			for _, s := range tc.magnetMiss {
				if strings.Contains(magnet, s) {
					t.Fatalf("expected magnet to not contain %q, got %q", s, magnet)
				}
			}
		})
	}
}

// What people paste into the search form, measured on 2026-09-23 (a day of
// "wrong resource provided" in the logs): two thirds titles, a quarter URLs of
// web pages, the rest mostly broken or lower-case-base32 magnets — plus
// ~490 inputs a day that the old {5,40} first match turned into a bogus hash
// (5 to 35 hex long) and sent to the load job. Every row is one input and the
// one outcome it must have.
func TestResolveQueryHash_FormInputs(t *testing.T) {
	const b32 = "BCW2LJ5GDA5K4HQJ3AY56Z2I2VTASWQQ" // v1Hash in base32
	v2 := "caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e"
	for _, tc := range []struct {
		name  string
		query string
		hash  string // expected hash, or
		err   error  // the error it must be (errors.Is)
		// for ErrHashLength: the characters pasted and in a full infohash
		len, full int
	}{
		{name: "40 hex", query: v1Hash, hash: v1Hash},
		{name: "40 hex upper case", query: strings.ToUpper(v1Hash), hash: v1Hash},
		{name: "40 hex with surrounding whitespace", query: "  " + v1Hash + "\n", hash: v1Hash},
		{name: "32 base32", query: b32, hash: v1Hash},
		{name: "32 base32 lower case", query: strings.ToLower(b32), hash: v1Hash},
		{name: "bare urn:btih value", query: "urn:btih:" + v1Hash, hash: v1Hash},
		{name: "full magnet", query: "magnet:?xt=urn:btih:" + v1Hash + "&dn=Sintel&tr=udp%3A%2F%2Ftracker.example%3A1337", hash: v1Hash},
		{name: "magnet with upper-case scheme", query: "MAGNET:?xt=urn:btih:" + v1Hash, hash: v1Hash},
		{name: "magnet with lower-case base32 btih", query: "magnet:?xt=urn:btih:" + strings.ToLower(b32) + "&dn=Sintel", hash: v1Hash},
		{name: "magnet inside other text", query: "url=magnet:?xt=urn:btih:" + strings.ToLower(b32) + "&dn=Sintel", hash: v1Hash},
		{name: "truncated magnet", query: "magnet:?xt=urn:btih:5e4bd524", err: ErrMagnetInvalid},
		{name: "magnet with only a name", query: "magnet:?dn=only+a+name", err: ErrMagnetNoHash},
		// A magnet that does not parse still names its hash: that is taken.
		{name: "magnet with an encoded newline before xt", query: "magnet:?%0Axt=urn:btih:" + v1Hash, hash: v1Hash},
		{name: "magnet with a word joiner after the hash", query: "magnet:?xt=urn:btih:" + v1Hash + "\u2060", hash: v1Hash},
		{name: "magnet inside text with encoded = and &", query: "magnet2torrentmagnet=magnet:?xt%3Durn:btih:" + v1Hash + "%26dn%3DSintel", hash: v1Hash},
		// but a hash split by a line break is not a hash
		{name: "magnet with a line break inside the hash", query: "magnet:?%0Axt=urn:btih:" + v1Hash[:20] + "%0A" + v1Hash[20:], err: ErrMagnetNoHash},
		{name: "v2-only magnet", query: "magnet:?xt=urn:btmh:1220" + v2, err: ErrV2Only},
		// v2-only is its own answer, not a reason to look for another hash
		{name: "v2-only magnet with a hex name", query: "magnet:?xt=urn:btmh:1220" + v2 + "&dn=" + v1Hash, err: ErrV2Only},
		{name: "64 hex (v2 digest)", query: v2, err: ErrV2Only},
		{name: "68 hex (v2 multihash)", query: "1220" + v2, err: ErrV2Only},
		{name: "title with an episode code", query: "Some Show S01E02", err: ErrQueryFreeText},
		{name: "title with year and resolution", query: "Some Movie 1999 1080p", err: ErrQueryFreeText},
		// A standalone 40-hex token is the hash, wherever it stands.
		{name: "title next to an infohash", query: "Sintel " + v1Hash, hash: v1Hash},
		{name: "infohash next to a title", query: v1Hash + " Sintel", hash: v1Hash},
		{name: "labelled infohash", query: "Info Hash: " + strings.ToUpper(v1Hash), hash: v1Hash},
		{name: "resource page link without a scheme", query: "webtor.io/" + v1Hash, hash: v1Hash},
		// ...and nothing shorter or longer is cut out of a longer string.
		{name: "title next to 39 hex", query: "Sintel " + v1Hash[:39], err: ErrQueryFreeText},
		{name: "title next to 41 hex", query: "Sintel " + v1Hash + "a", err: ErrQueryFreeText},
		{name: "title next to a v2 digest", query: "Sintel " + v2, err: ErrQueryFreeText},
		{name: "hex glued to a word", query: "Sintel_" + v1Hash, err: ErrQueryFreeText},
		{name: "page without a scheme with a numeric id", query: "torrents.example/torrent/1234567/some-movie", err: ErrQueryFreeText},
		// The whole input is hash characters, but not as many as a hash has.
		{name: "39 hex", query: v1Hash[:39], err: ErrHashLength, len: 39, full: 40},
		{name: "41 hex", query: v1Hash + "a", err: ErrHashLength, len: 41, full: 40},
		{name: "39 hex upper case, as a urn", query: "urn:btih:" + strings.ToUpper(v1Hash[:39]), err: ErrHashLength, len: 39, full: 40},
		{name: "16 hex", query: v1Hash[:16], err: ErrHashLength, len: 16, full: 40},
		{name: "31 base32", query: b32[:31], err: ErrHashLength, len: 31, full: 32},
		{name: "33 base32 lower case", query: strings.ToLower(b32) + "a", err: ErrHashLength, len: 33, full: 32},
		// ...and too short, or too word-like, to be one.
		{name: "15 hex", query: v1Hash[:15], err: ErrQueryFreeText},
		{name: "hex word", query: "deadbeef", err: ErrQueryFreeText},
		{name: "long number", query: "12345678901234567890", err: ErrQueryFreeText},
		{name: "hex letters only", query: "abcdefabcdefabcdefab", err: ErrQueryFreeText},
		{name: "title run together, one digit", query: "SPIDERMANHOMECOMINGFARFROMHOME2", err: ErrQueryFreeText},
		{name: "base32 alphabet in mixed case", query: "ShapixShapeElementsPack2x3", err: ErrQueryFreeText},
		{name: "23 base32", query: b32[:23], err: ErrQueryFreeText},
		{name: "site name without a scheme", query: "torrents.example", err: ErrQueryFreeText},
		{name: "torrent site page with a numeric id", query: "https://torrents.example/torrent/12345/some-movie/", err: ErrQueryWebPage},
		{name: "page url with an incidental hex run", query: "https://www.facebook.com/story/123", err: ErrQueryWebPage},
		{name: "page url without a scheme", query: "www.torrents.example/torrent/12345/", err: ErrQueryWebPage},
		{name: "page url carrying the infohash", query: "https://torrents.example/torrent/" + v1Hash + "/details", hash: v1Hash},
		{name: "resource page link", query: "https://webtor.io/" + v1Hash, hash: v1Hash},
		{name: "direct .torrent link", query: "https://files.example/iso/distro-2026.2-amd64.iso.torrent", err: ErrQueryTorrentURL},
		{name: "direct .torrent link named by infohash", query: "https://cache.example/torrent/" + strings.ToUpper(v1Hash) + ".torrent", hash: v1Hash},
		{name: "page url carrying a v2 digest", query: "https://torrents.example/" + v2, err: ErrQueryWebPage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hash, magnet, err := ResolveQueryHash(tc.query)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("got hash=%q err=%v, want error %q", hash, err, tc.err)
				}
				var hl *HashLengthError
				if errors.As(err, &hl) != (tc.len != 0) {
					t.Fatalf("err %v: a *HashLengthError is expected exactly for the length cases", err)
				}
				if hl != nil && (hl.Len != tc.len || hl.Full != tc.full) {
					t.Fatalf("got %d of %d characters, want %d of %d", hl.Len, hl.Full, tc.len, tc.full)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hash != tc.hash {
				t.Fatalf("got hash %q, want %q", hash, tc.hash)
			}
			if !strings.Contains(magnet, "urn:btih:"+tc.hash) {
				t.Fatalf("magnet %q does not carry the hex hash", magnet)
			}
		})
	}
}
