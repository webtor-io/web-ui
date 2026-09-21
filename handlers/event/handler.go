package event

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/cache_index"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/notification"
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
	// ci follows the seeder's cache events (cached.go); nil leaves them unread.
	ci   cacheIndexer
	subs []*nats.Subscription
	done chan struct{}
}

func New(c *cli.Context, nats *cs.NATS, pg *cs.PG, v *vault.Vault, cl *claims.Claims, ns *notification.Service, billing notification.Billing, index *cache_index.CacheIndex) *Handler {
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
		done:    make(chan struct{}),
	}
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
// consumers are declared by the deployment, and one that is not there yet (an
// older chart, a NATS without them) must not take the process down over an
// optimisation: the index then works as it did before the events, on marks
// made at play time and the expiry. It says so, once, at start.
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
		handler := sub.handler
		err := h.subscribe(js, "common", sub.subject, sub.consumer, func(b []byte) error { return handler(h.ci, b) })
		if err != nil {
			log.WithError(err).WithField("consumer", sub.consumer).
				Warn("cache events are not consumed: the cache index falls back to play-time marks and expiry")
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
	for _, sub := range h.subs {
		_ = sub.Unsubscribe()
	}
	close(h.done)
}
