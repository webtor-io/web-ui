package claims

import (
	"context"
	"errors"
	"testing"

	proto "github.com/webtor-io/claims-provider/proto"
	"google.golang.org/grpc"
)

type fakeProvider struct {
	resp *proto.GetResponse
	err  error
}

func (f *fakeProvider) Get(context.Context, *proto.GetRequest, ...grpc.CallOption) (*proto.GetResponse, error) {
	return f.resp, f.err
}

func withProvider(p proto.ClaimsProviderClient) *Claims {
	cl := &Client{}
	cl.once.Do(func() {})
	cl.cl = p
	return New(nil, cl, nil)
}

func freeTier() *proto.GetResponse {
	return &proto.GetResponse{
		Context: &proto.Context{Tier: &proto.Tier{Id: 0, Name: "free"}},
		Claims:  &proto.Claims{Connection: &proto.Connection{}, Embed: &proto.Embed{}, Site: &proto.Site{}},
	}
}

var errDown = errors.New("rpc error: code = Internal desc = failed to get claims")

// Every visitor without an identity asks the provider the same question, and
// the provider answers it from Postgres. 2026-10-05 Postgres went away for
// five minutes and every anonymous request -- pages, /assets, the manifest --
// answered 503. The last answer is still the right one; what must not stand
// in for it is the synthetic no-provider claims (no ads, no rate): a provider
// that failed is not a provider that is absent.
func TestAnonymousClaimsOutliveAProviderOutage(t *testing.T) {
	p := &fakeProvider{err: errDown}
	s := withProvider(p)
	anon := &Request{}

	if _, err := s.Get(anon); err == nil {
		t.Fatal("no answer from the provider yet, and Get made one up")
	}

	p.resp, p.err = freeTier(), nil
	if _, err := s.Refresh(anon); err != nil {
		t.Fatal(err)
	}

	p.resp, p.err = nil, errDown
	d, err := s.Refresh(anon)
	if err != nil {
		t.Fatalf("anonymous visitor got %v while the provider was down; want its last answer", err)
	}
	if d.Context.Tier.Name != "free" || d.Claims.Site.NoAds {
		t.Errorf("got %v, want the provider's last answer", d)
	}
}

// The anonymous answer is for anonymous visitors only: a signed-in user's
// tier is theirs, and handing them the free one would take away what they pay
// for without a word.
func TestSignedInClaimsDoNotFallBackToAnonymous(t *testing.T) {
	p := &fakeProvider{resp: freeTier()}
	s := withProvider(p)
	if _, err := s.Get(&Request{}); err != nil {
		t.Fatal(err)
	}

	p.resp, p.err = nil, errDown
	if d, err := s.Get(&Request{Email: "paid@example.com"}); err == nil {
		t.Fatalf("signed-in user got %v while the provider was down", d)
	}
}
