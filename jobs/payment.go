package j

import (
	"context"
	"crypto/sha1"
	"fmt"
	"time"

	"github.com/webtor-io/web-ui/services/claims"
	np "github.com/webtor-io/web-ui/services/payments"

	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/web"
)

const (
	paymentPollInterval = 5 * time.Second
	// paymentWatchDeadline bounds the polling loop, not the payment: the IPN
	// grants the tier regardless, the page just stops watching.
	paymentWatchDeadline = 20 * time.Minute
	// paymentJobTimeout caps the whole job context. Deliberately longer than
	// the watch deadline plus the tier-activation wait, so the final log
	// messages and the close marker are still published on a LIVE context —
	// writing them after ctx expiry would silently drop them from the shared
	// Redis log and leave cross-pod SSE observers hanging.
	paymentJobTimeout = paymentWatchDeadline + 4*time.Minute
)

func paymentTerminal(status string) bool {
	switch status {
	case "finished", "partially_paid", "failed", "expired", "refunded":
		return true
	}
	return false
}

// PaymentStatus watches a payment until it reaches a terminal state, then
// redirects back to the success page, which renders the final outcome.
// Concurrent viewers of the same payment share one job.
func (s *Jobs) PaymentStatus(c *web.Context, pc *np.Client, paymentID string) (*job.Job, error) {
	ctx, cancel := context.WithTimeout(context.Background(), paymentJobTimeout)
	id := fmt.Sprintf("%x", sha1.Sum([]byte(paymentID+"/"+c.Lang)))
	returnURL := web.LangURL(c.Lang, "/donate/crypto/success?payment_id="+paymentID)
	j := s.q.GetOrCreate("payment").Enqueue(ctx, cancel, id, job.NewScript(func(j *job.Job) error {
		j.InProgress(s.T(c, "job.payment.checking"))
		lastStatus := ""
		errStreak := 0
		deadline := time.NewTimer(paymentWatchDeadline)
		defer deadline.Stop()
		ticker := time.NewTicker(paymentPollInterval)
		defer ticker.Stop()
		for {
			p, err := pc.GetPayment(ctx, paymentID)
			if err != nil {
				errStreak++
				// The gateway hiccuping must not kill the watch: the
				// payment is progressing at the provider regardless.
				if errStreak%12 == 0 {
					j.Warn(err)
				}
			} else {
				errStreak = 0
				if paymentTerminal(p.Status) {
					j.Done()
					if p.Status == "finished" {
						s.waitTierApplied(ctx, j, c, p.TierID)
					}
					j.Redirect(returnURL, s.T(c, "job.redirecting"))
					return nil
				}
				status := paymentPhase(p.Status)
				if status != lastStatus {
					// Close whichever entry is open — the initial
					// "checking" line or the previous phase — so exactly
					// one task is ever active.
					j.Done()
					j.InProgress(s.T(c, "job.payment."+status))
					lastStatus = status
				}
			}
			select {
			case <-ctx.Done():
				return nil
			case <-deadline.C:
				j.Done()
				j.Info(s.T(c, "job.payment.stillPending"))
				return nil
			case <-ticker.C:
			}
		}
	}), false, s.errorFormatter(c))
	if j.Context != ctx {
		// Deduped onto an already-running watch: our context was never
		// adopted by Enqueue, release its timer.
		cancel()
	}
	return j, nil
}

// paymentPhase collapses provider statuses into the two user-visible waiting
// phases: money not seen yet vs confirming on-chain.
func paymentPhase(status string) string {
	switch status {
	case "confirming", "confirmed", "sending":
		return "confirming"
	default:
		return "waiting"
	}
}

// waitTierApplied force-refreshes the user's claims (dropping the web-ui
// cache each attempt) until the granted tier is visible, so the user leaves
// the success page with the tier already active everywhere. Bounded: on
// timeout it proceeds anyway — the membership is granted, downstream caches
// just need their TTL.
func (s *Jobs) waitTierApplied(ctx context.Context, jb *job.Job, c *web.Context, tierID int) {
	if s.claims == nil || c.User == nil || c.User.Email == "" {
		return
	}
	jb.InProgress(s.T(c, "job.payment.activating"))
	defer jb.Done()
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(paymentPollInterval)
	defer ticker.Stop()
	req := &claims.Request{Email: c.User.Email, PatreonUserID: c.User.PatreonUserID}
	for {
		// Poll the provider directly: hammering Refresh would Drop hot
		// cache entries every tick and race the per-request middleware.
		d, err := s.claims.Fetch(req)
		if err == nil && d.Context != nil && d.Context.Tier != nil && int(d.Context.Tier.Id) >= tierID {
			// Single refresh seeds the web-ui cache with the new tier.
			_, _ = s.claims.Refresh(req)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
		}
	}
}
