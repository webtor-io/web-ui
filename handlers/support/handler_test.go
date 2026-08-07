package support

import (
	"testing"
)

// A report has to name the torrent it is about. The pattern matches lower-case
// hex only, so matching before lower-casing silently dropped every upper-case
// hash — and an abuse report with an empty infohash cannot be enforced.
func TestInfohashExtraction(t *testing.T) {
	const want = "c9e15763f722f23e98a29decdfae341b98d53056"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"lower-case magnet", "magnet:?xt=urn:btih:" + want + "&dn=x", want},
		{"upper-case magnet", "magnet:?xt=urn:btih:C9E15763F722F23E98A29DECDFAE341B98D53056&dn=x", want},
		{"bare upper-case hash", "C9E15763F722F23E98A29DECDFAE341B98D53056", want},
		{"mixed case", "C9e15763F722f23E98a29DEcdfae341B98d53056", want},
		{"webtor link", "https://webtor.io/" + want + "?file=a.mkv", want},
		{"no hash at all", "please remove my movie", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractInfohash(tc.in); got != tc.want {
				t.Fatalf("extracted %q, want %q", got, tc.want)
			}
		})
	}
}
