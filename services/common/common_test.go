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
		{name: "magnet with an encoded newline before xt", query: "magnet:?%0Axt=urn:btih:" + v1Hash, err: ErrMagnetNoHash},
		{name: "v2-only magnet", query: "magnet:?xt=urn:btmh:1220" + v2, err: ErrV2Only},
		{name: "64 hex (v2 digest)", query: v2, err: ErrV2Only},
		{name: "68 hex (v2 multihash)", query: "1220" + v2, err: ErrV2Only},
		{name: "title with an episode code", query: "Some Show S01E02", err: ErrQueryFreeText},
		{name: "title with year and resolution", query: "Some Movie 1999 1080p", err: ErrQueryFreeText},
		{name: "title next to an infohash", query: "Sintel " + v1Hash, err: ErrQueryFreeText},
		{name: "39 hex", query: v1Hash[:39], err: ErrQueryFreeText},
		{name: "41 hex", query: v1Hash + "a", err: ErrQueryFreeText},
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
