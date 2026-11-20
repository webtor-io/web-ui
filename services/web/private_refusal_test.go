package web

import (
	"testing"

	"github.com/webtor-io/web-ui/services/api"
)

const (
	upload0 = "9c5db5752b7c0123456789abcdef0123456789ab" // an account's own upload
	upload1 = "9c5db5752b7c1123456789abcdef0123456789ab" // the upload class, kind 1
)

func TestNewPrivateRefusal(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         int
		code, reason   string
		signedIn       bool
		id             string
		key            string
		signIn, upload bool
		title          string
	}{
		{"anonymous on a private torrent", 403, api.CodePrivateTorrent, api.ReasonSignIn, false, "", "error.private_sign_in", true, false, ""},
		{"signed in without a record", 403, api.CodePrivateTorrent, api.ReasonNoRecord, true, "", "error.private_no_record", false, true, ""},
		// Only a missing record is fixed by the user's own .torrent.
		{"signed in, refused for another reason", 403, api.CodePrivateTorrent, "token_in_query", true, "", "error.generic", false, false, ""},
		{"anonymous upload", 422, api.CodePrivateNeedsAccount, "", false, "", "error.private_needs_account", true, false, ""},
		{"the tracker refused the key", 422, api.CodePrivateTrackerRefused, "key", true, "", "error.private_tracker_refused", false, true, ""},
		{"the server does not take private torrents", 403, api.CodePrivateUnavailable, "", true, "", "error.private.unavailable", false, false, ""},
		// An upload is seen by its owner alone: no tracker, no .torrent of
		// anyone's opens it.
		{"upload, anonymous", 403, api.CodePrivateTorrent, api.ReasonSignIn, false, upload0, "error.upload_private_sign_in", true, false, "private.page.uploadTitle"},
		{"upload, signed in, not the owner", 403, api.CodePrivateTorrent, api.ReasonNoRecord, true, upload0, "error.upload_private_no_record", false, false, "private.page.uploadTitle"},
		{"kind 1 is not an upload's refusal", 403, api.CodePrivateTorrent, api.ReasonNoRecord, true, upload1, "error.private_no_record", false, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewPrivateRefusal(&api.PrivateError{Status: tc.status, Code: tc.code, Reason: tc.reason}, tc.signedIn, tc.id)
			if r.Key != tc.key || r.Code != tc.code || r.Reason != tc.reason {
				t.Errorf("key/code/reason = %q/%q/%q, want %q/%q/%q", r.Key, r.Code, r.Reason, tc.key, tc.code, tc.reason)
			}
			if r.SignIn != tc.signIn || r.Upload != tc.upload {
				t.Errorf("SignIn=%v Upload=%v, want %v/%v", r.SignIn, r.Upload, tc.signIn, tc.upload)
			}
			if r.Title != tc.title {
				t.Errorf("Title=%q, want %q", r.Title, tc.title)
			}
			if r.VaultKept || r.Target != "" {
				t.Errorf("VaultKept=%v Target=%q: the caller sets these", r.VaultKept, r.Target)
			}
		})
	}
}
