package web

import (
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/uploads"
)

// PrivateRefusal is what a person is told when rest-api refuses a private torrent
// or an account's own upload. The resource page (resource/private) and the
// load-job card (load/errors/private) render it through one partial, private/refusal.
type PrivateRefusal struct {
	Title     string // i18n key of the card's heading; "" reads private.page.title
	Key       string // ClassifyError of the refusal: the text
	Code      string // rest-api's code: an Umami property
	Reason    string // rest-api's reason: an Umami property
	SignIn    bool   // the way out is an account
	Upload    bool   // the way out is the user's own .torrent
	VaultKept bool   // a Vault copy is kept and plays after the upload (the page sets it)
	Target    string // data-async-target of the upload form: "main" on the page, "#log-load" in the card
}

// NewPrivateRefusal is the refusal of the resource id. An upload is seen by
// its owner alone: no tracker, and no .torrent of anyone's opens it.
func NewPrivateRefusal(pe *api.PrivateError, signedIn bool, id string) *PrivateRefusal {
	r := &PrivateRefusal{Key: privateErrKey(pe), Code: pe.Code, Reason: pe.Reason}
	if uploads.Owned(id) && pe.Code == api.CodePrivateTorrent {
		r.Title = "private.page.uploadTitle"
		switch pe.Reason {
		case api.ReasonSignIn:
			r.Key, r.SignIn = "error.upload_private_sign_in", !signedIn
		case api.ReasonNoRecord:
			r.Key = "error.upload_private_no_record"
		}
		return r
	}
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
