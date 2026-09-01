package notification

import (
	"fmt"

	uuid "github.com/satori/go.uuid"
)

// SubscriptionView is everything a subscription email needs to know about
// the subscription. A view rather than the model: the mailer has no business
// knowing what a poll schedule or a baseline is.
type SubscriptionView struct {
	ID     uuid.UUID
	Title  string
	Season int // 0 for a movie
	// Lang is the language to render in — resolved by the caller from the
	// account, falling back to the language the subscription was created in.
	Lang string
	// UnsubscribeURL is signed by the caller (it holds the secret) so the
	// link works without a login. Empty renders no link.
	UnsubscribeURL string
}

// ReleaseView is one line in the "new releases" list.
type ReleaseView struct {
	Name string
	// InfoHash identifies the release. It never reaches the reader — it is
	// what makes a batch's dedupe key different from the previous batch's.
	InfoHash string
	// URL opens the release on Webtor. It is a magnet entry point rather
	// than a resource page: the torrent does not have to be in our store
	// for the link to work.
	URL string
	// Source names the addon or tracker that had it.
	Source string
}

type subscriptionMailData struct {
	Title          string
	Season         int
	IsSeason       bool
	Releases       []ReleaseView
	Completed      bool
	ManageURL      string
	UnsubscribeURL string
	Domain         string
}

func (s *Service) subscriptionData(sub SubscriptionView) subscriptionMailData {
	return subscriptionMailData{
		Title:          sub.Title,
		Season:         sub.Season,
		IsSeason:       sub.Season > 0,
		ManageURL:      withUTM(s.domain+"/profile#subscriptions", "release-sub"),
		UnsubscribeURL: sub.UnsubscribeURL,
		Domain:         s.domain,
	}
}

// SendSubscriptionOn confirms a new subscription.
//
// The dedupe key carries the subscription id on purpose. Send() mutes a
// repeat of the same (key, to) within 24 hours, so a key like "sub-on" would
// leave a user who subscribes to two things in one evening with one
// confirmation and one silence.
func (s *Service) SendSubscriptionOn(to string, userID uuid.UUID, sub SubscriptionView) error {
	return s.Send(SendOptions{
		To:       to,
		UserID:   userID,
		Lang:     sub.Lang,
		Key:      fmt.Sprintf("sub-on-%s", sub.ID),
		Title:    s.T(sub.Lang, "email.subscription.on.subject", "Title", sub.Title),
		Template: "subscription-on.html",
		Data:     s.subscriptionData(sub),
	})
}

// SendSubscriptionOff confirms that a subscription has ended. completed
// distinguishes the two ways that happens: the season finished airing, or
// the user removed it.
func (s *Service) SendSubscriptionOff(to string, userID uuid.UUID, sub SubscriptionView, completed bool) error {
	data := s.subscriptionData(sub)
	data.Completed = completed
	return s.Send(SendOptions{
		To:       to,
		UserID:   userID,
		Lang:     sub.Lang,
		Key:      fmt.Sprintf("sub-off-%s", sub.ID),
		Title:    s.T(sub.Lang, "email.subscription.off.subject", "Title", sub.Title),
		Template: "subscription-off.html",
		Data:     data,
	})
}

// SendSubscriptionUpdate reports releases the subscription had not seen
// before. Everything found since the last letter goes out in one message —
// four new rips are four lines here, not four emails.
func (s *Service) SendSubscriptionUpdate(to string, userID uuid.UUID, sub SubscriptionView, releases []ReleaseView) error {
	if len(releases) == 0 {
		return nil
	}
	data := s.subscriptionData(sub)
	data.Releases = make([]ReleaseView, len(releases))
	for i, r := range releases {
		r.URL = withUTM(r.URL, "release-sub")
		data.Releases[i] = r
	}
	return s.Send(SendOptions{
		To:     to,
		UserID: userID,
		Lang:   sub.Lang,
		// Unlike the on/off letters this one recurs, so the key has to
		// change between sends or the 24-hour dedupe would swallow the
		// second batch — and its hashes, already recorded as seen, would
		// never be mentioned again. How often a batch may go out is the
		// poller's decision, not the deduper's.
		Key:      fmt.Sprintf("sub-upd-%s-%s", sub.ID, releases[0].InfoHash),
		Title:    s.T(sub.Lang, "email.subscription.update.subject", "Title", sub.Title),
		Template: "subscription-update.html",
		Data:     data,
	})
}
