package support

import (
	"net/http"
	"net/url"
	"testing"

	proto "github.com/webtor-io/abuse-store/proto"
)

// What each cause needs to say about its reporter, checked before anything
// is pushed: a copyright notice its reporter's name, an email and both
// statements, an intimate image report an email, a CSAM report nothing.
func TestSupportReporterFields(t *testing.T) {
	copyright := func() url.Values { return withReporter(url.Values{"cause": {"0"}, "infohash": {hashA}}) }
	without := func(v url.Values, keys ...string) url.Values {
		for _, k := range keys {
			v.Del(k)
		}
		return v
	}
	cases := []struct {
		name    string
		form    url.Values
		wantErr string
	}{
		{"copyright without a name", without(copyright(), "reporter_name"), "error.support_name_required"},
		{"copyright with a blank name", func() url.Values { v := copyright(); v.Set("reporter_name", "  "); return v }(), "error.support_name_required"},
		{"copyright without an email", without(copyright(), "email"), "error.support_email_required"},
		{"copyright without good faith", without(copyright(), "good_faith"), "error.support_statements_required"},
		{"copyright without authority", without(copyright(), "authorized"), "error.support_statements_required"},
		{"intimate image without an email", url.Values{"cause": {"5"}, "infohash": {hashA}}, "error.support_email_required"},
		{"intimate image without a hash", url.Values{"cause": {"5"}, "email": {"me@example.com"}}, "error.support_infohash_required"},
		{"CSAM without a hash", url.Values{"cause": {"4"}}, "error.support_infohash_required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			as := &fakeAbuseStore{got: make(chan *proto.PushRequest, 1)}
			r := newSupportRouter(t, as)
			w := postSupport(r, tc.form)
			if want := "[form][err=" + tc.wantErr + "]"; w.Code != http.StatusBadRequest || w.Body.String() != want {
				t.Fatalf("got %d %q, want 400 %q", w.Code, w.Body.String(), want)
			}
			if len(as.got) != 0 {
				t.Fatal("pushed a notice the form refused")
			}
		})
	}
}

// The fields reach the Push as typed, Source stays FORM, and the causes that
// need less go through with less.
func TestSupportPushesReporterFields(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		want *proto.PushRequest
	}{
		{"copyright", withReporter(url.Values{"cause": {"0"}, "infohash": {hashA}}),
			&proto.PushRequest{Cause: proto.PushRequest_ILLEGAL_CONTENT, Email: "legal@example.com", ReporterName: "Example Studio", GoodFaith: true, Authorized: true}},
		{"intimate image, no name", url.Values{"cause": {"5"}, "infohash": {hashA}, "email": {"me@example.com"}},
			&proto.PushRequest{Cause: proto.PushRequest_INTIMATE_IMAGE, Email: "me@example.com"}},
		{"intimate image with a name", url.Values{"cause": {"5"}, "infohash": {hashA}, "email": {"me@example.com"}, "reporter_name": {"A. Person"}},
			&proto.PushRequest{Cause: proto.PushRequest_INTIMATE_IMAGE, Email: "me@example.com", ReporterName: "A. Person"}},
		{"CSAM without an email", url.Values{"cause": {"4"}, "infohash": {hashA}},
			&proto.PushRequest{Cause: proto.PushRequest_CSAM_REPORT}},
		// support.js only hides another cause's fields, and the browser posts
		// what was typed in them before the cause changed.
		{"CSAM with a copyright notice's fields", withReporter(url.Values{"cause": {"4"}, "infohash": {hashA}}),
			&proto.PushRequest{Cause: proto.PushRequest_CSAM_REPORT, Email: "legal@example.com"}},
		{"intimate image with a copyright notice's statements", withReporter(url.Values{"cause": {"5"}, "infohash": {hashA}}),
			&proto.PushRequest{Cause: proto.PushRequest_INTIMATE_IMAGE, Email: "legal@example.com", ReporterName: "Example Studio"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			as := &fakeAbuseStore{got: make(chan *proto.PushRequest, 1)}
			r := newSupportRouter(t, as)
			if w := postSupport(r, tc.form); w.Code != http.StatusOK || w.Body.String() != "[success]" {
				t.Fatalf("got %d %q, want 200 [success]", w.Code, w.Body.String())
			}
			pr := <-as.got
			if pr.GetSource() != proto.PushRequest_FORM || pr.GetInfohash() != hashA || pr.GetCause() != tc.want.GetCause() ||
				pr.GetEmail() != tc.want.GetEmail() || pr.GetReporterName() != tc.want.GetReporterName() ||
				pr.GetGoodFaith() != tc.want.GetGoodFaith() || pr.GetAuthorized() != tc.want.GetAuthorized() {
				t.Errorf("pushed source %v hash %q cause %v email %q name %q good_faith %v authorized %v; want %v",
					pr.GetSource(), pr.GetInfohash(), pr.GetCause(), pr.GetEmail(), pr.GetReporterName(), pr.GetGoodFaith(), pr.GetAuthorized(), tc.want)
			}
		})
	}
}

// A notice abuse-store holds for confirmation gets the page that says so; any
// other success keeps the usual one.
func TestSupportConfirmationSentPage(t *testing.T) {
	for sent, want := range map[bool]string{true: "[success][confirmation-sent]", false: "[success]"} {
		t.Run(want, func(t *testing.T) {
			as := &fakeAbuseStore{got: make(chan *proto.PushRequest, 1), reply: &proto.PushReply{ConfirmationSent: sent}}
			r := newSupportRouter(t, as)
			w := postSupport(r, withReporter(url.Values{"cause": {"0"}, "infohash": {hashA}}))
			if w.Code != http.StatusOK || w.Body.String() != want {
				t.Errorf("got %d %q, want 200 %q", w.Code, w.Body.String(), want)
			}
		})
	}
}
