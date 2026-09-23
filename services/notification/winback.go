package notification

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/payments"
)

// WinBackReason is how a membership ended without a single payment.
type WinBackReason int

const (
	// WinBackTrialEnded: the member cancelled during the free trial, which
	// then ran out.
	WinBackTrialEnded WinBackReason = iota + 1
	// WinBackPaymentFailed: the trial ran out, the card was declined, and
	// the provider has stopped retrying it.
	WinBackPaymentFailed
)

// WinBack is what the winback letter says: why the plan is gone, the cap the
// account is back on, and the promo code that is the way back.
type WinBack struct {
	Reason WinBackReason
	// Tier is the trial's tier, named in the letter for a cancelled trial;
	// "" leaves the plan unnamed. The declined-card letter never names one:
	// the account lost its tier weeks before the provider gave up.
	Tier string
	// CapMbps is the download cap the account is back on; 0 leaves the
	// sentence out.
	CapMbps int64
	// Discount is the live code the letter hands out.
	Discount payments.Discount
	// CheckoutURL is the provider's checkout where the code is typed in;
	// "" sends the reader to the storefront instead.
	CheckoutURL string
}

const (
	winbackKey      = "winback"
	winbackTemplate = "winback.html"
	// winbackRetryAfter: an entry whose letter never left is mailed again
	// only once the row is this old -- younger, and the pod that wrote it may
	// still be talking to the SMTP server.
	winbackRetryAfter = 10 * time.Minute
	// winbackRetryWithin: an owed letter is mailed only while it is younger
	// than this. Its body carries the code it was written with, which had at
	// least offer.DiscountLead (72h) left then, so under 48h it is still
	// honoured when the retry goes out. The retry runs in the daily
	// `notification send` cron, so a failed send gets one or two attempts.
	winbackRetryWithin = 48 * time.Hour
)

// SendWinBack offers a promo code to an account whose membership ended
// without a single payment: a trial cancelled before it converted, or a
// trial whose card was declined until the provider gave up.
//
// Why this exists: of the trials started 2026-07-15..09-07, 46% were
// cancelled during the trial and 60% of the rest ended on a declined card;
// of the declined members who had never paid (first decline 2026-07-20..
// 08-31), ~21% paid later on their own and the rest never did. Both groups used the site like the people
// who paid (a site account for ~93%, activity during the trial for 57-64%).
// The provider gives nobody a second trial, so a discount on the first
// period is the one honest offer left -- and it is sent only once the
// membership is over, because the provider's discount codes are for members
// without a paid membership.
//
// Once per account, ever, whichever the reason. It does not go through Send,
// whose 24h feed guard would reuse a row another pod just wrote and mail it
// a second time. Here the row is the claim: the insert either succeeds --
// this call owns the letter and mails it -- or hits the unique index of
// migration 74 because another pod got there first, and then it is theirs.
// Any earlier entry ends it for events: the events behind this letter come
// once per membership, so a letter whose send failed is not waited for here
// but picked up by SendOwedWinBacks in the daily cron.
func (s *Service) SendWinBack(to string, userID uuid.UUID, w WinBack) error {
	ctx := context.Background()
	prev, err := s.store.GetLastByKeyAndUser(ctx, winbackKey, userID)
	if err != nil {
		return errors.Wrap(err, "failed to check for an earlier winback notification")
	}
	if prev != nil {
		return nil
	}
	lang := s.store.AccountLang(ctx, userID)
	body, err := s.render(winbackTemplate, lang, s.winbackData(lang, w))
	if err != nil {
		return errors.Wrap(err, "failed to render notification template")
	}
	n := &models.Notification{
		Key:      winbackKey,
		Title:    s.winbackSubject(lang, w),
		Template: winbackTemplate,
		Body:     body,
		UserID:   &userID,
	}
	if Deliverable(to) {
		addr := to
		n.To = &addr
	}
	if err := s.store.Create(ctx, n); err != nil {
		if isUniqueViolation(err) {
			return nil
		}
		return errors.Wrap(err, "failed to save notification to db")
	}
	return s.mailEntry(ctx, n, to, lang)
}

// SendOwedWinBacks mails winback entries whose letter never left although
// they had an address -- a failed SMTP send, or a pod stopped between the
// insert and the send. Run from the daily `notification send` cron. Each row
// is mailed as written, to the address it was written for, by the one
// caller that wins its claim; rows past winbackRetryWithin are left alone
// because the code in their body may no longer be honoured. Returns how many
// letters went out.
func (s *Service) SendOwedWinBacks(ctx context.Context) (int, error) {
	if !s.hasMail() {
		return 0, nil
	}
	now := time.Now()
	owed, err := s.store.ListOwed(ctx, winbackKey, now.Add(-winbackRetryAfter), now.Add(-winbackRetryWithin), 500)
	if err != nil {
		return 0, err
	}
	sent := 0
	for i := range owed {
		n := &owed[i]
		if n.To == nil || !Deliverable(*n.To) || n.UserID == nil {
			continue
		}
		claimed, err := s.store.ClaimOwed(ctx, n.NotificationID, n.UpdatedAt)
		if err != nil {
			return sent, err
		}
		if !claimed {
			continue
		}
		if err := s.mailEntry(ctx, n, *n.To, s.store.AccountLang(ctx, *n.UserID)); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

// mailEntry puts an entry this call owns on the wire and stamps it mailed.
func (s *Service) mailEntry(ctx context.Context, n *models.Notification, to, lang string) error {
	if !Deliverable(to) || !s.hasMail() {
		return nil
	}
	letter, err := s.wrapEmail(n.Body, lang)
	if err != nil {
		return errors.Wrap(err, "failed to render email layout")
	}
	if err := s.mail.Send(to, n.Title, letter); err != nil {
		return errors.Wrap(err, "failed to send email")
	}
	return s.store.MarkMailed(ctx, n.NotificationID, to)
}

func isUniqueViolation(err error) bool {
	var pgErr pg.Error
	return stderrors.As(err, &pgErr) && pgErr.Field('C') == "23505"
}

func (s *Service) winbackSubject(lang string, w WinBack) string {
	key := "email.winback.subject"
	if w.Reason == WinBackTrialEnded {
		key += "Trial"
	}
	if w.Discount.PeriodDays == 365 {
		key += "Year"
	} else {
		key += "Month"
	}
	return s.T(lang, key, "Percent", w.Discount.PercentOff)
}

func (s *Service) winbackData(lang string, w WinBack) map[string]any {
	tier := ""
	if w.Tier != "" {
		tier = s.tierTitle(lang, w.Tier)
	}
	checkout := w.CheckoutURL
	if checkout == "" {
		checkout = withUTM(s.domain+"/donate", "winback")
	}
	return map[string]any{
		"Trial":      w.Reason == WinBackTrialEnded,
		"Tier":       tier,
		"Cap":        w.CapMbps,
		"Year":       w.Discount.PeriodDays == 365,
		"Percent":    w.Discount.PercentOff,
		"Code":       w.Discount.Code,
		"URL":        checkout,
		"LastDay":    lastDay(w.Discount.ExpiresAt),
		"SupportURL": withUTM(s.domain+"/support", "winback"),
	}
}

// lastDay is the last calendar day on which the code works for a reader in
// any time zone, UTC-12 included: that zone's day ends at 12:00 UTC of the
// next one, so the day is the one before (expiresAt - 12h). Conservative by
// up to a day, never generous -- the letter calls it the last valid day,
// inclusive. ISO, for the same reason as chargeDate.
func lastDay(expiresAt time.Time) string {
	if expiresAt.IsZero() {
		return ""
	}
	d := expiresAt.Add(-12 * time.Hour).UTC()
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1).Format("2006-01-02")
}

// PreviewWinBack renders the letter exactly as it would go on the wire
// without sending or journaling it -- for the dev-only preview route, since
// the events behind it cannot be replayed at will. Returns subject and HTML.
func (s *Service) PreviewWinBack(lang string, w WinBack) (string, string, error) {
	body, err := s.render(winbackTemplate, lang, s.winbackData(lang, w))
	if err != nil {
		return "", "", err
	}
	letter, err := s.wrapEmail(body, lang)
	if err != nil {
		return "", "", err
	}
	return s.winbackSubject(lang, w), letter, nil
}
