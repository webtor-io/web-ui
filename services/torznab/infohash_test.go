package torznab

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

func mustParseIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("bad test ip %q", s)
	}
	return ip
}

func TestNormalizeInfoHash(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want string
	}{
		{"8C4ADBF9EBDC2C31E4B3D01A9E9C5C0F2A1B3C4D", "8c4adbf9ebdc2c31e4b3d01a9e9c5c0f2a1b3c4d"},
		{" 8c4adbf9ebdc2c31e4b3d01a9e9c5c0f2a1b3c4d ", "8c4adbf9ebdc2c31e4b3d01a9e9c5c0f2a1b3c4d"},
		// base32 spelling of the same 20 bytes
		{"RRFNX6PL3QWDDZFT2ANJ5HC4B4VBWPCN", "8c4adbf9ebdc2c31e4b3d01a9e9c5c0f2a1b3c4d"},
		{"", ""},
		{"nothex-nothex-nothex-nothex-nothexxxxxxx", ""},
		{"8c4adbf9", ""},
	} {
		if got := normalizeInfoHash(tt.in); got != tt.want {
			t.Errorf("normalizeInfoHash(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestInfoHashFromMagnet(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want string
	}{
		{"magnet:?xt=urn:btih:aaaabbbbccccddddeeeeffff0000111122223333&dn=x", "aaaabbbbccccddddeeeeffff0000111122223333"},
		{"MAGNET:?xt=urn:btih:AAAABBBBCCCCDDDDEEEEFFFF0000111122223333", "aaaabbbbccccddddeeeeffff0000111122223333"},
		{"magnet:?dn=no-hash-here", ""},
		{"https://example.com/x.torrent", ""},
		{"", ""},
	} {
		if got := infoHashFromMagnet(tt.in); got != tt.want {
			t.Errorf("infoHashFromMagnet(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// torrentBytes builds a minimal single-file .torrent so the parse path is
// exercised against a real bencoded body rather than a fixture blob.
func torrentBytes(t *testing.T) ([]byte, string) {
	t.Helper()
	info := metainfo.Info{
		Name:        "release.mkv",
		Length:      1024,
		PieceLength: 256 * 1024,
		Pieces:      make([]byte, 20),
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("failed to bencode info: %v", err)
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		t.Fatalf("failed to write metainfo: %v", err)
	}
	return buf.Bytes(), mi.HashInfoBytes().HexString()
}

func TestResolveInfoHashPrefersCheapSources(t *testing.T) {
	// A download must not happen when the feed already told us the hash.
	var downloads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := testClient()
	res := &Result{
		InfoHash: "8C4ADBF9EBDC2C31E4B3D01A9E9C5C0F2A1B3C4D",
		Link:     srv.URL,
	}
	got, err := c.ResolveInfoHash(context.Background(), res)
	if err != nil {
		t.Fatalf("ResolveInfoHash() error = %v", err)
	}
	if got != "8c4adbf9ebdc2c31e4b3d01a9e9c5c0f2a1b3c4d" {
		t.Errorf("hash = %q, want the attribute value", got)
	}
	if downloads != 0 {
		t.Errorf("downloaded %d times, want 0 — the attribute was enough", downloads)
	}
}

func TestResolveInfoHashFollowsMagnetRedirect(t *testing.T) {
	const magnet = "magnet:?xt=urn:btih:aaaabbbbccccddddeeeeffff0000111122223333&dn=rel"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public-tracker download links commonly 302 to a magnet URI,
		// which no HTTP transport can dial — the redirect has to be read
		// rather than followed.
		w.Header().Set("Location", magnet)
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	got, err := testClient().ResolveInfoHash(context.Background(), &Result{Link: srv.URL})
	if err != nil {
		t.Fatalf("ResolveInfoHash() error = %v", err)
	}
	if got != "aaaabbbbccccddddeeeeffff0000111122223333" {
		t.Errorf("hash = %q, want the redirect's btih", got)
	}
}

func TestResolveInfoHashParsesTorrentFile(t *testing.T) {
	body, want := torrentBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	got, err := testClient().ResolveInfoHash(context.Background(), &Result{Link: srv.URL})
	if err != nil {
		t.Fatalf("ResolveInfoHash() error = %v", err)
	}
	if got != want {
		t.Errorf("hash = %q, want %q", got, want)
	}
}

func TestResolveInfoHashFailsOnNonTorrentBody(t *testing.T) {
	// A private tracker that has logged the user out serves an HTML login
	// page with HTTP 200. Treating that as a torrent would mint a garbage
	// infohash and put a dead stream in front of the user.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>Please log in</body></html>"))
	}))
	defer srv.Close()

	if _, err := testClient().ResolveInfoHash(context.Background(), &Result{Link: srv.URL}); err == nil {
		t.Fatal("ResolveInfoHash() accepted an HTML body")
	}
}

func TestResolveInfoHashWithoutAnySource(t *testing.T) {
	if _, err := testClient().ResolveInfoHash(context.Background(), &Result{Title: "rel"}); err == nil {
		t.Fatal("ResolveInfoHash() succeeded with nothing to go on")
	}
}
