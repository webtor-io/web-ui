package web

import (
	"testing"

	"github.com/webtor-io/web-ui/services/api"
)

func TestNewPrivateRefusal(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         int
		code, reason   string
		signedIn       bool
		key            string
		signIn, upload bool
	}{
		{"anonymous on a private torrent", 403, api.CodePrivateTorrent, api.ReasonSignIn, false, "error.private_sign_in", true, false},
		{"signed in without a record", 403, api.CodePrivateTorrent, api.ReasonNoRecord, true, "error.private_no_record", false, true},
		// Only a missing record is fixed by the user's own .torrent.
		{"signed in, refused for another reason", 403, api.CodePrivateTorrent, "token_in_query", true, "error.generic", false, false},
		{"anonymous upload", 422, api.CodePrivateNeedsAccount, "", false, "error.private_needs_account", true, false},
		{"the tracker refused the key", 422, api.CodePrivateTrackerRefused, "key", true, "error.private_tracker_refused", false, true},
		{"the server does not take private torrents", 403, api.CodePrivateUnavailable, "", true, "error.private.unavailable", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewPrivateRefusal(&api.PrivateError{Status: tc.status, Code: tc.code, Reason: tc.reason}, tc.signedIn)
			if r.Key != tc.key || r.Code != tc.code || r.Reason != tc.reason {
				t.Errorf("key/code/reason = %q/%q/%q, want %q/%q/%q", r.Key, r.Code, r.Reason, tc.key, tc.code, tc.reason)
			}
			if r.SignIn != tc.signIn || r.Upload != tc.upload {
				t.Errorf("SignIn=%v Upload=%v, want %v/%v", r.SignIn, r.Upload, tc.signIn, tc.upload)
			}
			if r.VaultKept || r.Target != "" {
				t.Errorf("VaultKept=%v Target=%q: the caller sets these", r.VaultKept, r.Target)
			}
		})
	}
}
