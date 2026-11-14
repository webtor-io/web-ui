package web

import "github.com/webtor-io/web-ui/services/api"

// PrivateRefusal is what a person is told when rest-api refuses a private torrent.
// The resource page (resource/private) and the load-job card (load/errors/private)
// render it through one partial, private/refusal.
type PrivateRefusal struct {
	Key       string // ClassifyError of the refusal: the text
	Code      string // rest-api's code: an Umami property
	Reason    string // rest-api's reason: an Umami property
	SignIn    bool   // the way out is an account
	Upload    bool   // the way out is the user's own .torrent
	VaultKept bool   // a Vault copy is kept and plays after the upload (the page sets it)
	Target    string // data-async-target of the upload form: "main" on the page, "#log-load" in the card
}

func NewPrivateRefusal(pe *api.PrivateError, signedIn bool) *PrivateRefusal {
	r := &PrivateRefusal{Key: privateErrKey(pe), Code: pe.Code, Reason: pe.Reason}
	switch pe.Code {
	case api.CodePrivateTorrent:
		r.SignIn = !signedIn
		r.Upload = signedIn && pe.Reason == api.ReasonNoRecord
	case api.CodePrivateNeedsAccount:
		r.SignIn = !signedIn
	case api.CodePrivateNoTracker, api.CodePrivateTooManyTrackers, api.CodePrivateTrackerRefused:
		r.Upload = signedIn
	}
	return r
}
