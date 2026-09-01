package notification

import "testing"

func TestWithUTM(t *testing.T) {
	cases := map[string]string{
		"https://webtor.io/vault":                       "https://webtor.io/vault?utm_source=webtor&utm_medium=email&utm_campaign=c",
		"https://webtor.io/profile#subscriptions":       "https://webtor.io/profile?utm_source=webtor&utm_medium=email&utm_campaign=c#subscriptions",
		"https://webtor.io/magnet:?xt=urn:btih:ab&dn=x": "https://webtor.io/magnet:?xt=urn:btih:ab&dn=x&utm_source=webtor&utm_medium=email&utm_campaign=c",
		"": "",
	}
	for in, want := range cases {
		if got := withUTM(in, "c"); got != want {
			t.Errorf("withUTM(%q):\n got %q\nwant %q", in, got, want)
		}
	}
}
