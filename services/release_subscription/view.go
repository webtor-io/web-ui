package release_subscription

import (
	"context"

	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/notification"
)

// langResolver is the half of a store that answers "what language does this
// account read in". Both the request side and the cron side have one, which
// is the point: a letter must not change language depending on which of
// them sent it.
type langResolver interface {
	AccountLang(ctx context.Context, userID uuid.UUID) string
}

// subscriptionView builds the mail-facing shape of a subscription, and with
// it the language decision: what the account is browsing in wins, and the
// language captured when the subscription was created is the fallback for
// accounts that have not been seen since user_settings.lang existed.
func subscriptionView(ctx context.Context, langs langResolver, sub *models.ReleaseSubscription, domain, secret string) notification.SubscriptionView {
	lang := sub.Lang
	if langs != nil {
		if fromAccount := langs.AccountLang(ctx, sub.UserID); fromAccount != "" {
			lang = fromAccount
		}
	}
	return notification.SubscriptionView{
		ID:             sub.ID,
		Title:          sub.GetTitle(),
		Season:         sub.GetSeason(),
		Lang:           lang,
		UnsubscribeURL: UnsubscribeURL(domain, secret, sub.ID),
	}
}
