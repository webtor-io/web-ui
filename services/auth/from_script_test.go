package auth

import (
	"net/http/httptest"
	"testing"
)

// fromScript decides who gets SuperTokens' 401 for an expired access token
// (the SDK refreshes and retries it) and who gets the refresh interstitial.
// The PUT row is the bug it exists for: a plain fetch with no
// X-Requested-With used to be answered with the interstitial's HTML and a
// 200, so a watch-position save looked successful and was lost.
func TestFromScript(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		headers map[string]string
		want    bool
	}{
		{"async navigation", "GET", map[string]string{"X-Requested-With": "XMLHttpRequest", "Sec-Fetch-Dest": "empty", "Sec-Fetch-Mode": "cors"}, true},
		{"plain fetch PUT /watch/position", "PUT", map[string]string{"Sec-Fetch-Dest": "empty", "Sec-Fetch-Mode": "cors"}, true},
		{"EventSource", "GET", map[string]string{"Sec-Fetch-Dest": "empty", "Sec-Fetch-Mode": "cors", "Accept": "text/event-stream"}, true},
		{"page navigation", "GET", map[string]string{"Sec-Fetch-Dest": "document", "Sec-Fetch-Mode": "navigate", "Accept": "text/html"}, false},
		{"form post", "POST", map[string]string{"Sec-Fetch-Dest": "document", "Sec-Fetch-Mode": "navigate"}, false},
		{"image", "GET", map[string]string{"Sec-Fetch-Dest": "image", "Sec-Fetch-Mode": "no-cors"}, false},
		{"video", "GET", map[string]string{"Sec-Fetch-Dest": "video", "Sec-Fetch-Mode": "no-cors"}, false},
		{"no fetch metadata", "GET", map[string]string{"Accept": "*/*"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/watch/position", nil)
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if got := fromScript(r); got != tc.want {
				t.Errorf("fromScript = %v, want %v", got, tc.want)
			}
		})
	}
}
