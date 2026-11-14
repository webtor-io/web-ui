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

// A magnet's tr= values are tracker URLs, and a private tracker's passkey is
// in them: in the query (?passkey=, ?pk=) or as a path segment. The magnet
// reaches the logs as a page address (GET /magnet:?…, the protocol handler's
// /magnet%3A%3F…, /ext/magnet?url=<magnet>) and inside rendered HTML.
func TestRedactURL_MagnetTrackers(t *testing.T) {
	const (
		hash    = "08ada5a7a6183aae1e09d831df6748d566095a10"
		passkey = "0123456789abcdef0123456789abcdef"
	)
	for _, c := range []struct{ name, in, want string }{
		{"a magnet page, its passkey in a tracker's query and in a path",
			"/magnet:?xt=urn:btih:" + hash + "&dn=Sintel&tr=udp%3A%2F%2Ftracker.invalid%3A6969&tr=http%3A%2F%2Ftracker.invalid%2Fannounce%3Fpasskey%3D" + passkey + "&tr=https%3A%2F%2Ftracker.invalid%2F" + passkey + "%2Fannounce",
			"/magnet:?xt=urn:btih:" + hash + "&dn=Sintel&tr=<redacted>&tr=<redacted>&tr=<redacted>"},
		{"the magnet as the url= of /ext/magnet",
			"/ext/magnet?url=magnet%3A%3Fxt%3Durn%3Abtih%3A" + hash + "%26tr%3Dhttp%253A%252F%252Ftracker.invalid%252Fannounce%253Fpasskey%253D" + passkey + "%26tr%3Dudp%253A%252F%252Ftracker.invalid%253A6969%26dn%3DSintel&utm_source=ext",
			"/ext/magnet?url=magnet%3A%3Fxt%3Durn%3Abtih%3A" + hash + "%26tr%3D<redacted>%26tr%3D<redacted>%26dn%3DSintel&utm_source=ext"},
		{"tr first in an encoded magnet",
			"/x?u=magnet%3A%3Ftr%3Dhttp%253A%252F%252Ftracker.invalid%252F" + passkey + "%26xt%3Durn%3Abtih%3A" + hash,
			"/x?u=magnet%3A%3Ftr%3D<redacted>%26xt%3Durn%3Abtih%3A" + hash},
		{"a magnet in rendered HTML",
			`<input type="hidden" name="resource" value="magnet:?xt=urn:btih:` + hash + `&amp;tr=http%3A%2F%2Ftracker.invalid%2Fa%3Fpk%3D` + passkey + `&amp;dn=Sintel" />`,
			`<input type="hidden" name="resource" value="magnet:?xt=urn:btih:` + hash + `&amp;tr=<redacted>&amp;dn=Sintel" />`},
		{"an unencoded tracker URL spills its passkey into the magnet's own query",
			"/magnet:?xt=urn:btih:" + hash + "&tr=http://tracker.invalid/announce.php?uid=1&passkey=" + passkey + "&dn=Sintel",
			"/magnet:?xt=urn:btih:" + hash + "&tr=<redacted>&passkey=<redacted>&dn=Sintel"},
		{"pk the same way",
			"/magnet:?xt=urn:btih:" + hash + "&tr=http://tracker.invalid/a?u=1&pk=" + passkey,
			"/magnet:?xt=urn:btih:" + hash + "&tr=<redacted>&pk=<redacted>"},
		{"names that only look like one",
			"/ru/search?str=1&tr2=x&lang=tr&apk=2&pk2=3",
			"/ru/search?str=1&tr2=x&lang=tr&apk=2&pk2=3"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := RedactURL(c.in); got != c.want {
				t.Errorf("RedactURL(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}

// Inside a magnet only the infohash, the name and the size are kept. Every
// other parameter can carry a private tracker's credential or a link that
// fetches it: tr= (the passkey in a tracker URL), as=/xs= (a .torrent
// download link with the user's key), ws=, x.pe=, and the keys an unencoded
// tracker URL spills into the magnet (uk=, authkey=, ...). A magnet is read
// as is, encoded once (the url= of /ext/magnet) and twice (that address
// passed on as a parameter of another).
func TestRedactURL_MagnetKeepsOnlyItsNameAndHash(t *testing.T) {
	const (
		hash = "08ada5a7a6183aae1e09d831df6748d566095a10"
		key  = "0123456789abcdef0123456789abcdef"
	)
	for _, c := range []struct{ name, in, want string }{
		{"a magnet page",
			"/magnet:?xt=urn:btih:" + hash + "&dn=Sintel&xl=1024&as=https%3A%2F%2Ftracker.invalid%2Fdownload%2F1." + key + "&xs=https%3A%2F%2Ftracker.invalid%2Fdl%3Fid%3D1%26uk%3D" + key + "&ws=https%3A%2F%2Fseed.invalid%2F" + key + "%2F&x.pe=192.0.2.1%3A6881&tr=udp%3A%2F%2Ftracker.invalid%3A6969%2F" + key,
			"/magnet:?xt=urn:btih:" + hash + "&dn=Sintel&xl=1024&as=<redacted>&xs=<redacted>%26uk%3D<redacted>&ws=<redacted>&x.pe=<redacted>&tr=<redacted>"},
		{"keys an unencoded tracker URL spills into the magnet",
			"/magnet:?xt=urn:btih:" + hash + "&tr=http://tracker.invalid/announce.php?uid=1&uk=" + key + "&dn=Sintel&tr=http://tracker.invalid/a?authkey=" + key + "&torrent_pass=" + key,
			"/magnet:?xt=urn:btih:" + hash + "&tr=<redacted>&uk=<redacted>&dn=Sintel&tr=<redacted>&torrent_pass=<redacted>"},
		{"and a bare value it spills",
			"/magnet:?xt=urn:btih:" + hash + "&tr=http://tracker.invalid/a.php?id=1&" + key,
			"/magnet:?xt=urn:btih:" + hash + "&tr=<redacted>&<redacted>"},
		{"the url= of /ext/magnet, up to the next parameter",
			"/ext/magnet?url=magnet%3A%3Fxt%3Durn%3Abtih%3A" + hash + "%26dn%3DSintel%26as%3Dhttps%253A%252F%252Ftracker.invalid%252Fdownload%252F1." + key + "%26tr%3Dhttp%3A%2F%2Ftracker.invalid%2Fa%3Fuid%3D1%26uk%3D" + key + "&utm_source=ext",
			"/ext/magnet?url=magnet%3A%3Fxt%3Durn%3Abtih%3A" + hash + "%26dn%3DSintel%26as%3D<redacted>%26tr%3D<redacted>%26uk%3D<redacted>&utm_source=ext"},
		{"that address passed on as a parameter: the magnet encoded twice",
			"/x?u=%2Fext%2Fmagnet%3Furl%3Dmagnet%253A%253Fxt%253Durn%253Abtih%253A" + hash + "%2526dn%253DSintel%2526as%253Dhttps%25253A%25252F%25252Ftracker.invalid%25252F1." + key + "%26utm_source%3Dext&lang=ru",
			"/x?u=%2Fext%2Fmagnet%3Furl%3Dmagnet%253A%253Fxt%253Durn%253Abtih%253A" + hash + "%2526dn%253DSintel%2526as%253D<redacted>%26utm_source%3Dext&lang=ru"},
		{"its ? as is and the rest encoded (common.ResolveQueryHash takes it)",
			"/ext/magnet?url=magnet:?xt%3Durn%3Abtih%3A" + hash + "%26dn%3DSintel%26as%3Dhttps%253A%252F%252Ftracker.invalid%252F1." + key,
			"/ext/magnet?url=magnet:?xt%3Durn%3Abtih%3A" + hash + "%26dn%3DSintel%26as%3D<redacted>"},
		{"the /magnet: route's query, path and RawQuery without the ?",
			"magnet:xt=urn:btih:" + hash + "&tr=udp://tracker.invalid:6969/" + key + "&as=https://tracker.invalid/1." + key,
			"magnet:xt=urn:btih:" + hash + "&tr=<redacted>&as=<redacted>"},
		{"a pasted magnet runs on past a line break, a space and an apostrophe",
			"magnet:?xt=urn:btih:" + hash + "\n&dn=Don't Look&as=https://tracker.invalid/1." + key,
			"magnet:?xt=urn:btih:" + hash + "\n&dn=Don't Look&as=<redacted>"},
		{"an error that quotes it, up to the quote",
			`parse "magnet:?xt=urn:btih:` + hash + `\n&as=https://tracker.invalid/1.` + key + `": net/url: invalid control character in URL&next=1`,
			`parse "magnet:?xt=urn:btih:` + hash + `\n&as=<redacted>": net/url: invalid control character in URL&next=1`},
		{"in rendered HTML",
			`<input value="magnet:?xt=urn:btih:` + hash + `&amp;dn=Sintel&amp;as=https%3A%2F%2Ftracker.invalid%2F1.` + key + `" />`,
			`<input value="magnet:?xt=urn:btih:` + hash + `&amp;dn=Sintel&amp;as=<redacted>" />`},
		{"whatever the case",
			"MAGNET:?XT=urn:btih:" + hash + "&DN=Sintel&AS=https://tracker.invalid/1." + key,
			"MAGNET:?XT=urn:btih:" + hash + "&DN=Sintel&AS=<redacted>"},
		{"an empty value and a trailing & stay as they are",
			"magnet:?xt=urn:btih:" + hash + "&tr=&",
			"magnet:?xt=urn:btih:" + hash + "&tr=&"},
		{"outside a magnet, as before",
			"/ru/search?str=magnet&as=1&xs=2&uk=3",
			"/ru/search?str=magnet&as=1&xs=2&uk=3"},
		{"a parser's error that names no magnet",
			"failed to parse magnet: error parsing uri: net/url: invalid control character in URL",
			"failed to parse magnet: error parsing uri: net/url: invalid control character in URL"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := RedactURL(c.in); got != c.want {
				t.Errorf("RedactURL(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
			// The logs redact in layers (an error, then the line that
			// carries it): a second pass changes nothing.
			if got := RedactURL(c.want); got != c.want {
				t.Errorf("RedactURL is not idempotent on %q: %q", c.want, got)
			}
		})
	}
}
