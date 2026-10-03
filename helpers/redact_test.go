package helpers

import "testing"

// The shapes the logs carried (Loki, 2026-09-30 and 2026-10-03).
const (
	logJWT       = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpbmZvSGFzaCI6ImFiYyJ9.c2lnbmF0dXJlLXNpZ25hdHVyZQ"
	logAddonUUID = "c7b58e23-9cd3-4747-ac94-fe9864f99ead"
)

func TestRedactURL(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"the addon's token and a resolve link",
			"/token/" + logAddonUUID + "/stremio/resolve/" + logJWT,
			"/token/<redacted>/stremio/resolve/<redacted>"},
		{"a stream token in the query",
			"/stremio/resolve/" + logJWT + "?token=" + logJWT,
			"/stremio/resolve/<redacted>?token=<redacted>"},
		{"a key and a token, encoded in a URL passed on",
			"/x?u=%2Fa.mkv%3Fapi-key%3Dk-12345678%26token%3D" + logJWT + "%26download%3Dtrue",
			"/x?u=%2Fa.mkv%3Fapi-key%3D<redacted>%26token%3D<redacted>%26download%3Dtrue"},
		{"the unsubscribe link",
			"/subscription/unsubscribe/3f9a1c0e7b2d",
			"/subscription/unsubscribe/<redacted>"},
		{"the email verification link",
			"/profile/email/verify/a1b2c3d4e5?lang=ru",
			"/profile/email/verify/<redacted>?lang=ru"},
		{"names that only look like one",
			"/tokens/abc/mytoken/def/stremio/manifest.json?mytoken=1&tokenizer=2",
			"/tokens/abc/mytoken/def/stremio/manifest.json?mytoken=1&tokenizer=2"},
		{"a stream URL to torrent-http-proxy: the site's key and a viewer's token",
			"https://abra--5e4bd524.api.example/89f8ee4c/?api-key=" + logAddonUUID + "&stats=true&token=" + logJWT,
			"https://abra--5e4bd524.api.example/89f8ee4c/?api-key=<redacted>&stats=true&token=<redacted>"},
		{"the download script a job renders",
			"<script>\n        var url = \"https://h.example/89f8ee4c/a.mp4?api-key=" + logAddonUUID + "\";\n",
			"<script>\n        var url = \"https://h.example/89f8ee4c/a.mp4?api-key=<redacted>\";\n"},
		{"nothing to hide",
			"/ru/80d7a3c8?file=/Sintel/Sintel.mkv",
			"/ru/80d7a3c8?file=/Sintel/Sintel.mkv"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := RedactURL(c.in); got != c.want {
				t.Errorf("RedactURL(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}
