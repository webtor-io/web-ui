package statusview

import (
	"testing"

	"github.com/webtor-io/web-ui/services/offer"
)

// The player's buffering label (Plan.Player): the viewer's own cap for the
// lock, and the card's link -- the stream box's destination through the
// player's own /trial surface, so the two surfaces count apart. No box, no
// link: the label stays the plain "Buffering".
func TestBuild_PlayerLabel(t *testing.T) {
	build := func(in Input) *Plan {
		t.Helper()
		if in.Lang == "" {
			in.Lang, in.Loc = "ru", loc("ru")
		}
		if in.Torrent.State == "" {
			in.Torrent = caching(61, 31, 38)
		}
		v := Build(in)
		if v.Plan == nil {
			t.Fatalf("%+v: no plan", in.Viewer)
		}
		return v.Plan
	}
	// A free viewer, a promo plan with a trial: the player's own surface,
	// next to the status box's.
	p := build(Input{Viewer: atCap, ClaimCapMbps: 5, Offers: liveOffers(), SizeBytes: gb12})
	if nb(p.Player.Rate) != "5 Мбит/с" || p.Player.URL != "/ru/trial?from=player-label" {
		t.Errorf("free: %+v", p.Player)
	}
	if p.Stream.Box == nil || p.Stream.Box.CTA.URL != "/ru/trial?from=status-bar" {
		t.Errorf("free: the status box keeps its own surface: %+v", p.Stream.Box)
	}
	if p.Player.Rate != "5 Мбит/с" {
		t.Errorf("the number and its unit do not part on a phone: %q", p.Player.Rate)
	}
	// English has no prefix; the link is the page language's.
	if p := build(Input{Lang: "en", Loc: loc("en"), Viewer: atCap, ClaimCapMbps: 5, Offers: liveOffers()}); p.Player.URL != "/trial?from=player-label" || nb(p.Player.Rate) != "5 Mbps" {
		t.Errorf("en: %+v", p.Player)
	}
	// A paying viewer: their own cap, and "compare plans" as the box has it.
	bronze := build(Input{Tier: "bronze", SignedIn: true, Torrent: caching(61, 31, 380),
		Viewer: Viewer{Known: true, Present: true, Mbps: 20, Limited: true, PlanBox: true, CapMbps: 20}, Offers: liveOffers()})
	if nb(bronze.Player.Rate) != "20 Мбит/с" || bronze.Player.URL != "/ru/donate" {
		t.Errorf("bronze: %+v", bronze.Player)
	}
	// No trial the checkout can start: the checkout, the same link as the box's.
	checkout := offer.Offer{Tier: "silver", RateMbps: 50, URL: "https://pay.example/silver"}
	if p := build(Input{Viewer: atCap, Offers: fakeOffers{promo: &checkout}}); p.Player.URL != "https://pay.example/silver" || p.Player.URL != p.Stream.Box.CTA.URL {
		t.Errorf("checkout: %+v / %+v", p.Player, p.Stream.Box)
	}
	// The cap's first seconds, before the box is due: the lock's number is
	// known, the card is not -- nothing to open.
	if p := build(Input{Viewer: capFact, ClaimCapMbps: 5, Offers: liveOffers()}); nb(p.Player.Rate) != "5 Мбит/с" || p.Player.URL != "" {
		t.Errorf("before the box: %+v", p.Player)
	}
	// Nothing faster on sale: the fact alone, no card.
	gold := Input{Tier: "gold", SignedIn: true, Torrent: caching(61, 31, 380),
		Viewer: Viewer{Known: true, Present: true, Mbps: 100, Limited: true, PlanBox: true, CapMbps: 100}, Offers: liveOffers()}
	noCatalog := Input{Viewer: atCap, ClaimCapMbps: 5}
	for name, in := range map[string]Input{"top tier": gold, "no catalog": noCatalog} {
		if p := build(in); p.Player.URL != "" || p.Stream.Box != nil {
			t.Errorf("%s: %+v / %+v", name, p.Player, p.Stream.Box)
		}
	}
	// The view the page keeps while its player plays through an HLS gap
	// (View.Playing) carries the same label.
	v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: Torrent{State: "cached", Progress: 100}, Viewer: zero,
		LastViewer: atCap, ClaimCapMbps: 5, Offers: liveOffers()})
	if v.Playing == nil || v.Playing.Plan == nil || v.Playing.Plan.Player.URL != "/ru/trial?from=player-label" {
		t.Errorf("playing: %+v", v.Playing)
	}
}
