package notification

import "strings"

// withUTM tags a link in an outgoing letter so the click is attributable in
// Umami, which reads utm_* off the landing page's query string. This is the
// whole of "email click tracking" here: no redirect hop, no per-link
// endpoint — a letter's CTR is sessions with this campaign over letters sent.
//
// The fragment, if any, has to stay last: "/profile?utm…#subscriptions" is a
// link to the section, "/profile#subscriptions?utm…" is a link to nowhere.
// Signed links (unsubscribe) must not pass through here — the signature
// covers the URL as issued.
func withUTM(raw, campaign string) string {
	if raw == "" {
		return raw
	}
	base, frag := raw, ""
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		base, frag = raw[:i], raw[i:]
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "utm_source=webtor&utm_medium=email&utm_campaign=" + campaign + frag
}
