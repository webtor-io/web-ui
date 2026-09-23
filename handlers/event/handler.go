package event

import (
	"context"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/cache_index"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/notification"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/vault"
)

type Handler struct {
	nats   *cs.NATS
	pg     *cs.PG
	vault  *vault.Vault
	claims *claims.Claims
	ns     *notification.Service
	// billing feeds the tier-welcome message; zero when no provider is on.
	billing notification.Billing
	// offers is the storefront catalog: the tier's benefit lines and trial
	// length in that message come from it, and so does the discount code of
	// the winback letter.
	offers *offer.Service
	// winbackHoldout is the percent of eligible accounts kept out of the
	// winback letter as a control group (winbackHeldOut).
	winbackHoldout int
	// winbackTrialEndedFrom: a cancelled trial earns the winback letter from
	// this instant on; zero keeps that reason off.
	winbackTrialEndedFrom time.Time
	// ci follows the seeder's cache events (cached.go); nil leaves them unread.
	ci   cacheIndexer
	subs []*nats.Subscription
	// subsMu: the cache subscriptions are added from their retry goroutines.
	subsMu sync.Mutex
	done   chan struct{}
}

func New(c *cli.Context, nats *cs.NATS, pg *cs.PG, v *vault.Vault, cl *claims.Claims, ns *notification.Service, billing notification.Billing, offers *offer.Service, index *cache_index.CacheIndex) *Handler {
	if !c.Bool(useEventHandlerFlag) {
		return nil
	}
	var ci cacheIndexer
	if index != nil {
		ci = index
	}
	return &Handler{
		ci:      ci,
		nats:    nats,
		pg:      pg,
		vault:   v,
		claims:  cl,
		ns:      ns,
		billing: billing,
		offers:  offers,
		done:    make(chan struct{}),

		winbackHoldout:        c.Int(winbackHoldoutFlag),
		winbackTrialEndedFrom: parseWinbackFrom(c.String(winbackTrialEndedFromFlag)),
	}
}

// parseWinbackFrom reads WINBACK_TRIAL_ENDED_FROM. A value that does not
// parse keeps the reason off and says so at startup, rather than guessing a
// date the owner did not write.
func parseWinbackFrom(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		log.WithError(err).WithField("value", v).Error("WINBACK_TRIAL_ENDED_FROM is not RFC 3339: the cancelled-trial winback letter stays off")
		return time.Time{}
	}
	return t
}

func (h *Handler) Serve() error {
	nc := h.nats.Get()
	if nc == nil {
		log.Warn("nats connection is nil, skipping subscriptions")
		return nil
	}
	js, err := nc.JetStream()
	if err != nil {
		return err
	}
	err = h.subscribe(js, "common", "resource.vaulted", "web-ui-resource-vaulted", h.resourceVaulted)
	if err != nil {
		return err
	}
	err = h.subscribe(js, "common", "resource.banned", "web-ui-resource-banned", h.resourceBanned)
	if err != nil {
		return err
	}
	err = h.subscribe(js, "common", "user.updated", "web-ui-user-updated", h.userUpdated)
	if err != nil {
		return err
	}

	h.subscribeCacheEvents(js)

	<-h.done

	return nil
}

// subscribeCacheEvents is allowed to fail, unlike the subscriptions above. The
// consumers are declared by the deployment, and one that is not there (an
// older chart, a NATS without them) must not take the process down over an
// optimisation: the index then works as it did before the events, on marks
// made at play time and the expiry. It says so, once.
//
// It keeps trying, though. On the rollout that introduces a consumer the pod
// can easily start before the operator has created it, and a single attempt
// would leave that pod deaf until its next restart.
func (h *Handler) subscribeCacheEvents(js nats.JetStreamContext) {
	if h.ci == nil {
		return
	}
	for _, sub := range []struct {
		subject, consumer string
		handler           func(cacheIndexer, []byte) error
	}{
		{"resource.cached", "web-ui-resource-cached", resourceCached},
		{"resource.uncached", "web-ui-resource-uncached", resourceUncached},
	} {
		subject, consumer, handler := sub.subject, sub.consumer, sub.handler
		go retryUntil(h.done, cacheSubscribeRetry, func(attempt int) bool {
			h.subsMu.Lock()
			err := h.subscribe(js, "common", subject, consumer, func(b []byte) error { return handler(h.ci, b) })
			h.subsMu.Unlock()
			if err == nil {
				if attempt > 0 {
					log.WithField("consumer", consumer).Info("cache events are consumed")
				}
				return true
			}
			if attempt == 0 {
				log.WithError(err).WithField("consumer", consumer).
					Warn("cache events are not consumed yet: the cache index runs on play-time marks and expiry; retrying")
			}
			return false
		})
	}
}

const cacheSubscribeRetry = time.Minute

// retryUntil calls try (attempt 0, 1, 2, ...) until it reports success or done
// is closed.
func retryUntil(done <-chan struct{}, every time.Duration, try func(attempt int) bool) {
	for attempt := 0; ; attempt++ {
		if try(attempt) {
			return
		}
		select {
		case <-done:
			return
		case <-time.After(every):
		}
	}
}

func (h *Handler) subscribe(js nats.JetStreamContext, stream string, subject string, consumer string, handler func([]byte) error) error {
	sub, err := js.PullSubscribe(subject, consumer, nats.Bind(stream, consumer))
	if err != nil {
		return err
	}
	h.subs = append(h.subs, sub)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.WithField("consumer", consumer).Errorf("panic in message handler: %v", r)
			}
		}()
		for {
			select {
			case <-h.done:
				return
			default:
				msgs, err := sub.Fetch(1, nats.MaxWait(5*time.Second))
				if err != nil {
					if err == context.DeadlineExceeded || err == nats.ErrTimeout {
						continue
					}
					log.WithError(err).WithField("consumer", consumer).Error("failed to fetch message")
					continue
				}
				msg := msgs[0]
				err = handler(msg.Data)
				if err != nil {
					log.WithError(err).WithField("consumer", consumer).Error("failed to handle message")
					_ = msg.Nak()
				} else {
					_ = msg.Ack()
				}
			}
		}
	}()
	return nil
}

func (h *Handler) Close() {
	h.subsMu.Lock()
	defer h.subsMu.Unlock()
	for _, sub := range h.subs {
		_ = sub.Unsubscribe()
	}
	close(h.done)
}
