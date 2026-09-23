package link_resolver

import (
	"context"
	"errors"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/lazymap"

	proto "github.com/webtor-io/claims-provider/proto"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/claims"
)

// A free account with no backend of its own that has the file is the
// paywall, and ResolveLink says so by name: the Stremio resolve handler
// answers ErrPlanRequired with the paywall clip and anything else as before,
// so a paywall that came back as a bare nil would silently be a 404 again.
func TestResolveLinkNamesThePaywall(t *testing.T) {
	uid := uuid.NewV4()
	s := &LinkResolver{
		enabledBackendsCache: lazymap.New[[]*models.StreamingBackend](&lazymap.Config{Expire: time.Minute}),
	}
	// No enabled backends for this account, without a database.
	if _, err := s.enabledBackendsCache.Get(uid.String(), func() ([]*models.StreamingBackend, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	free := &claims.Data{Context: &proto.Context{Tier: &proto.Tier{Id: 0, Name: "free"}}}

	res, err := s.ResolveLink(context.Background(), uid, nil, free, "08ada5a7a6183aae1e09d831df6748d566095a10", 0, true)
	if !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("err = %v, want ErrPlanRequired", err)
	}
	if res != nil {
		t.Errorf("result = %+v, want nil alongside the error", res)
	}
}
