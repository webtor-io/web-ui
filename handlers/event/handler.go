package event

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
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
	subs   []*nats.Subscription
	done   chan struct{}
}

func New(c *cli.Context, nats *cs.NATS, pg *cs.PG, v *vault.Vault, cl *claims.Claims, ns *notification.Service) *Handler {
	if !c.Bool(useEventHandlerFlag) {
		return nil
	}
	return &Handler{
		nats:   nats,
		pg:     pg,
		vault:  v,
		claims: cl,
		ns:     ns,
		done:   make(chan struct{}),
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
	err = h.subscribe(js, "common", "user.updated", "web-ui-user-updated", h.userUpdated)
	if err != nil {
		return err
	}

	<-h.done

	return nil
}

func (h *Handler) subscribe(js nats.JetStreamContext, stream string, subject string, consumer string, handler func([]byte) error) error {
	sub, err := js.PullSubscribe(subject, consumer, nats.Bind(stream, consumer))
	if err != nil {
		return err
	}
	h.subs = append(h.subs, sub)
	go func() {
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
