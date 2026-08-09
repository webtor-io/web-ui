package common

import (
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
