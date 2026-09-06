package resource

import (
	"strings"
	"testing"
	"time"

	ra "github.com/webtor-io/rest-api/services"
)

func TestTorrentFileToken(t *testing.T) {
	const secret = "s3cret"
	const hash = "08ada5a7a6183aae1e09d831df6748d566095a10"
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	tok, err := SignTorrentFileToken(secret, hash, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckTorrentFileToken(secret, tok, hash, now.Add(time.Hour)); err != nil {
		t.Fatalf("fresh token rejected: %v", err)
	}
	if err := CheckTorrentFileToken(secret, tok, strings.ToUpper(hash), now); err != nil {
		t.Fatalf("infohash case must not matter: %v", err)
	}
	cases := []struct {
		name   string
		raw    string
		hash   string
		secret string
		at     time.Time
	}{
		{"expired", tok, hash, secret, now.Add(torrentFileTokenTTL + time.Minute)},
		{"other torrent", tok, "0000000000000000000000000000000000000000", secret, now},
		{"other secret", tok, hash, "other", now},
		{"empty", "", hash, secret, now},
		{"garbage", "not-a-token", hash, secret, now},
	}
	for _, c := range cases {
		if err := CheckTorrentFileToken(c.secret, c.raw, c.hash, c.at); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
}

// A token signed for another audience with the same secret must not open a
// .torrent — the unsubscribe links in release emails share the secret.
func TestTorrentFileTokenRejectsOtherAudience(t *testing.T) {
	const secret = "s3cret"
	const hash = "08ada5a7a6183aae1e09d831df6748d566095a10"
	now := time.Now()
	tok, err := SignTorrentFileToken(secret, hash, now)
	if err != nil {
		t.Fatal(err)
	}
	// Re-sign the same claims under a different audience by tampering the
	// audience through the public helper is impossible, so mint one directly.
	other := mintForAudience(t, secret, hash, "release-subscription-unsubscribe", now)
	if err := CheckTorrentFileToken(secret, other, hash, now); err == nil {
		t.Fatal("token for another audience accepted")
	}
	if err := CheckTorrentFileToken(secret, tok, hash, now); err != nil {
		t.Fatal(err)
	}
}

func TestTorrentFileURLFallsBackWithoutSecret(t *testing.T) {
	gd := &ExtendedResource{ResourceResponse: &ra.ResourceResponse{ID: "08ada5a7a6183aae1e09d831df6748d566095a10"}}
	if got := NewHelper("").TorrentFileURL(gd); got != "/"+gd.ID+".torrent" {
		t.Fatalf("no secret: %s", got)
	}
	got := NewHelper("s3cret").TorrentFileURL(gd)
	if !strings.HasPrefix(got, "/"+gd.ID+".torrent?token=") {
		t.Fatalf("signed: %s", got)
	}
	if err := CheckTorrentFileToken("s3cret", strings.TrimPrefix(got, "/"+gd.ID+".torrent?token="), gd.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
}
