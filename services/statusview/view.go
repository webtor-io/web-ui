// Package statusview is the presentation of a torrent's transfer status on
// the resource page: the chain "swarm ▸ cache ▸ you" while something moves
// (only its participants), the badge the page had before the chain while
// nothing does, the piece bar under either, and the hint or the plan box
// under that (docs/transfer_status.html renders every state with the key
// Build returns for it; the design as approved: transfer_status.approved.html).
//
// Build is one pure function from what the server knows — the torrent's
// status, the viewer's own session reading, their tier and cap, the offer
// catalog, the file size, the locale — to everything the page draws, already
// localized. The server-rendered page and every status stream message carry
// its output, so the two can never disagree, and the client only moves text
// and classes into nodes it built once.
//
// The server cannot tell a stream from a download: both are bytes to the same
// session. For a plan-limited state it therefore sends both variants of the
// plan box and the page picks one (Plan).
package statusview

import (
	"math"
	"strconv"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
)

// State keys: the rows of docs/transfer_status.html. The page refines KeyTier
// into tier_dl / stream_ok / stream_stall by what its player is doing, and
// applies the same refinement to KeyCachedTier and KeyVaultedTier.
//
// A key is the cause; whether it is drawn as the chain or the badge is the
// View's Mode (something moves or not). A swarm held through a gap between
// its pieces is drawn as it last moved, key and all (Input.HeldBps): the
// chain never shows a badge key's pause or dash for it.
const (
	KeyActive      = "active"
	KeyChecking    = "checking"
	KeyTier        = "tier"
	KeySwarm       = "swarm"
	KeyStalled     = "stalled"
	KeyMissing     = "missing"
	KeyCachingOnly = "caching_only"
	// KeyCachingIdle: caching, and nothing moves that a label can show,
	// while the verdict is neither a pause nor an empty swarm (a stream
	// reconnecting mid-download, pieces asked for and not arriving): the
	// old "Caching N%" badge. Added to the living design after the approval.
	KeyCachingIdle  = "caching_idle"
	KeyCachedFlow   = "cached_flow"
	KeyCachedTier   = "cached_tier"
	KeyCached       = "cached"
	KeyPaused       = "paused"
	KeyNoSeed       = "noseed"
	KeyMissingIdle  = "missing_idle"
	KeyIdleTorrent  = "idle_torrent"
	KeyUnknown      = "status_unknown"
	KeyVaulting     = "vaulting"
	KeyVaultingOnly = "vaulting_only"
	// KeyVaultingIdle: Vault's transfer, and nothing moves (queued, between
	// attempts): the old "Saving N%" badge. Added after the approval.
	KeyVaultingIdle = "vaulting_idle"
	KeyVaulted      = "vaulted"
	KeyVaultedTier  = "vaulted_tier"
	KeyVaultedIdle  = "vaulted_idle"
	KeyVaultWait    = "vault_waiting"
	KeyVaultMissing = "vault_missing"
	KeyVaultFailed  = "vault_failed"
)

// Modes: how a view is drawn.
const (
	// ModeChain: something moves -- the swarm sends to the cache -- or a
	// request of the viewer's is open (bytes going to them, or a wait for
	// data). The chain draws only who takes part; the sticky bar and the
	// details popover exist only here.
	ModeChain = "chain"
	// ModeBadge: nothing moves. The badge the page had before the chain,
	// with the piece bar and the state's hint under it.
	ModeBadge = "badge"
)

// nameVault is a brand name, the same in every language (docs/i18n.md: not
// translated). The cache is a word, and translated
// (resource.status.chain.cache): the chain's middle node is the cache, not
// the brand (owner, 2026-09-25).
const nameVault = "Vault"

// fewSeeders: at most this many seeders and a swarm slower than the cap, the
// swarm is what the transfer is waiting for — a plan would not speed it up.
const fewSeeders = 3

// View is everything the page draws for the transfer status.
type View struct {
	// Key is the state (Key* constants); also the "state" of the
	// impression and click events.
	Key string `json:"key"`
	// Mode is how it is drawn: ModeChain while something moves or the
	// viewer waits, ModeBadge while nothing does. Both are in the page for
	// good (partials/resource/status.html) and take the same row, so a
	// switch never moves the card; the page shows one of them.
	Mode string `json:"mode"`
	// Sticky: worth keeping on screen when the block scrolls away -- the
	// chain is up (the sticky bar and the details exist only with it).
	Sticky bool `json:"sticky"`
	// Auth and Tier are the viewer, for the analytics props.
	Auth string `json:"auth"` // anon | user
	Tier string `json:"tier"` // free | bronze | …
	// Nodes are the chain's three slots, left to right: the swarm, the
	// source (the cache, or Vault), the viewer. Segs are the two links
	// between them. The slots never change, only what is in them: Show
	// false keeps a slot's element in place and hidden. Show is whether it
	// takes part: the swarm while it sends (or the viewer waits for it),
	// the viewer while a request of theirs is open (Viewer.Present); the
	// source always.
	Nodes [3]Node `json:"nodes"`
	Segs  [2]Seg  `json:"segs"`
	// Badge is the state's badge, filled whatever the mode.
	Badge Badge `json:"badge"`
	Bar   Bar   `json:"bar"`
	// Hint is the line under the bar; empty for none. Never set together
	// with Plan: in a plan-limited state the plan's variant is the line.
	Hint string `json:"hint,omitempty"`
	// Vault: the hint ends with the offer to save the torrent to Vault,
	// which fetches the missing pieces on its own once someone has them.
	// The link is the page's (it knows the viewer and whether Vault is
	// configured at all); this says only that the state calls for it.
	Vault bool `json:"vault,omitempty"`
	// Plan is set in a plan-limited state (KeyTier, KeyCachedTier,
	// KeyVaultedTier): the only place anything is sold, and only once the
	// box is due (Plan).
	Plan    *Plan   `json:"plan,omitempty"`
	Details Details `json:"details"`
	// Playing is the whole view with the viewer on the chain at their last
	// reading (Input.LastViewer), for a viewer whose requests read closed
	// now; nil otherwise. The page shows it while its own player plays or
	// buffers (lib/transferStatus.js playing): an HLS player closes its
	// request between two segments, and the proxy's count honestly reads
	// zero there -- longer than PresenceDebounce while a buffered video
	// plays on. The server cannot see the player; the page can. Its own
	// Playing is nil.
	//
	// There is no view "without the reading" for the free grace window any
	// more (View.Grace until 2026-09-26): grace segment tokens carry the
	// viewer's session (api.GraceClaims), so thp counts them like any other
	// request of theirs (docs/grace_token.md "Session").
	Playing *View `json:"playing,omitempty"`
}

// Badge is the badge of the page before the chain (git 5d55b26e
// partials/resource/status.html), one element whose colour, icon and words
// change in place -- now partials/status/badge.html, the Vault dashboard's
// rows too (handlers/resource sends it alone on their stream). Two differ
// from the old ones, as the approved design draws them: "checking" is cyan,
// "In Vault" Vault's purple with its layers.
type Badge struct {
	// Tone is its colour: muted | cyan | warn | err | ok | vault (style.css
	// .tx-badge, the old badge's utility colours). The element has one more,
	// pink, for the Vault page's own "Expiring"; Build never uses it.
	Tone string `json:"tone"`
	// Icon: idle | dots | noseed | pause | down | check | up | clock | warn
	// | unknown | vault (#tx-b-*, status/badge_symbols; dots are DaisyUI's
	// loading dots).
	Icon string `json:"icon"`
	// Pulse: the icon pulses (a transfer in progress, Vault waiting).
	Pulse bool   `json:"pulse,omitempty"`
	Label string `json:"label"`
	// Extra is the swarm after the label, in brackets: "(14 seeders)".
	Extra string `json:"extra,omitempty"`
}

// BadgeEl is a Badge as a page places it: what partials/status/badge.html
// renders -- the one badge element of the resource page's status block and
// of every status pill on the Vault page -- and lib/statusBadge.js updates
// in place. DescID is the id its words take when another element is
// described by them (the resource page's chain, aria-describedby); "" where
// nothing is, as on the Vault page, whose rows carry a badge each and an id
// would repeat.
type BadgeEl struct {
	Badge
	DescID string
}

// El is the badge placed with its words under descID ("" for none).
func (b Badge) El(descID string) BadgeEl { return BadgeEl{Badge: b, DescID: descID} }

// Node is one stop on the chain.
type Node struct {
	Show bool   `json:"show"`
	Kind string `json:"kind"` // swarm | cache | vault | you
	// Icon names the glyph: swarm | cloud | check (the whole torrent in
	// the cache) | vault (the navbar's three layers) | user.
	Icon string `json:"icon"`
	Name string `json:"name"` // "Swarm", "Cache", "Vault", "You"
	// Caption is the phone label under the icon ("swarm", "cache", "you");
	// Short the number in the phone pill ("31", "61%", "?").
	Caption string `json:"caption"`
	Value   string `json:"value,omitempty"` // "31 seeders", "61%", "saved", "?"
	Short   string `json:"short,omitempty"`
	Tone    string `json:"tone,omitempty"` // "" | cached | vault | err | warn
	Dim     bool   `json:"dim,omitempty"`
}

// Seg is one link of the chain, the arrow towards the viewer.
type Seg struct {
	Show bool   `json:"show"`
	Kind string `json:"kind"` // swarm (swarm → source) | viewer (source → you)
	// Tone is the colour, which says the cause: flow (cyan, data flowing),
	// plan (pink, the plan's cap), swarm (amber, the swarm), vault (purple),
	// pause (amber, dashed), off (grey, dashed).
	Tone string `json:"tone"`
	// On: bytes are moving — the one sweep under the label runs.
	On    bool   `json:"on,omitempty"`
	Speed string `json:"speed,omitempty"` // "38 Mbps", "— Mbps", "0 Mbps", "paused"
	Note  string `json:"note,omitempty"`  // "cap", "few seeders", "waiting for data"
	// Dots: checking — three pulsing dots stand in for the speed.
	Dots bool `json:"dots,omitempty"`
}

// Bar is the piece bar's slot. The pieces themselves ride on the status
// (pieces/active); this says whether to draw them and in which colour.
type Bar struct {
	Mode string `json:"mode"`           // pieces | divider
	Tone string `json:"tone,omitempty"` // flow | vault
}

// Plan is the plan cap's line in its three contexts. Download and Stream
// are empty until the cap has held long enough for the box
// (Viewer.PlanBox): the pink link says the cap first, and nothing is sold
// -- nor its line drawn in the box's place -- before that.
type Plan struct {
	// Download: bytes at the cap and the page's player is not playing.
	Download Variant `json:"download"`
	// Stream: the player is buffering (waiting/stalled in the last minute),
	// or playing a file over the cap. Whatever the file's bitrate: a real
	// stall while thp's limiter holds the viewer's requests is the cap's
	// doing, however well the estimate said the file fits (FitsCap).
	Stream Variant `json:"stream"`
	// Fact: the player plays smoothly under the cap — said, not sold.
	Fact string `json:"fact"`
	// Cap is the cap alone ("Without a subscription — up to 5 Mbps"): the
	// line while a player inside its free grace window buffers -- the
	// grace window is not the cap, so nothing is sold there.
	Cap string `json:"cap"`
}

// Variant is either a plan box with its button or, with nothing to sell
// (top tier, no catalog), the fact alone as a hint line.
type Variant struct {
	Hint string `json:"hint,omitempty"`
	Box  *Box   `json:"box,omitempty"`
}

// Box is the plan box: bolt, title and sub on the left, the button with the
// trial note under it on the right (action/download_file.html's pattern).
type Box struct {
	Title string `json:"title"`
	Sub   string `json:"sub,omitempty"`
	CTA   CTA    `json:"cta"`
}

// CTA is the plan box's button.
type CTA struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	// Note is the line under the button: "7 days free · cancel anytime".
	Note string `json:"note,omitempty"`
	// Target is the funnel step for analytics: trial | checkout | donate.
	Target string `json:"target"`
}

// Details is the "where is the bottleneck" popover: a title and four rows
// in fixed slots — swarm, source, you, cap. The plan box under them is the
// block's own (the same variant the page picked).
type Details struct {
	Title string `json:"title"`
	Rows  [4]Row `json:"rows"`
}

// Row is one line of Details.
type Row struct {
	Show  bool   `json:"show"`
	Key   string `json:"key"` // swarm | source | you | cap
	Label string `json:"label"`
	Tag   string `json:"tag,omitempty"` // the "cap" chip next to "To you"
	Sub   string `json:"sub,omitempty"`
	Value string `json:"value,omitempty"`
}

// Torrent is the torrent side of the status (handlers/resource TorrentStatus).
type Torrent struct {
	// State: idle, caching, cached, vaulting, vaulted, vault_waiting,
	// vault_failed, unknown.
	State string
	// Pending: the page is rendering and nothing has been asked of the
	// seeder yet — an idle status then means "not known yet", not "idle".
	Pending  bool
	Progress float64 // 0..100
	// Seeders, Leechers and Peers as the seeder reports them (connected);
	// SwarmKnown says it did.
	Seeders    int
	Leechers   int
	Peers      int
	SwarmKnown bool
	// The swarm's availability (api.EventData): whether the seeder knows
	// the connected peers' piece sets, the share of the torrent complete
	// here or claimed by one of them, whether some pieces nobody connected
	// has are not complete here either (Missing -- the bar hatches them),
	// and how many wanted pieces nobody has, and of those how many an open
	// reader is on or reads ahead into. Nothing of it counts unless
	// AvailabilityKnown.
	AvailabilityKnown bool
	Availability      float64
	Missing           bool
	WantedMissing     int
	ReaderMissing     int
	// RateBps is the swarm's useful throughput, bytes a second.
	RateBps   float64
	Checking  bool
	Paused    bool
	NoSeeders bool
	// Pieces: there is a piece map to draw.
	Pieces bool
	// Settling: the status stream has watched the swarm for less than its
	// settle window and has not seen it move yet -- too early to say that
	// nothing moves. The pieces nobody has are not blamed then (a swarm
	// that was downloading read "needed pieces missing" for the seconds
	// before its first measured piece).
	Settling bool
	// CacheProgress is how much of the torrent is in Webtor's cache,
	// 0..100, for the states whose own Progress is Vault's (vault_waiting,
	// vault_failed); 0 when not known.
	CacheProgress float64
}

// Offers is what is on sale (*offer.Service; nil-safe there).
type Offers interface {
	Promo() *offer.Offer
	FasterOnSale(rateMbps float64) bool
}

// Input is everything Build needs.
type Input struct {
	Lang    string
	Loc     *goi18n.Localizer
	Torrent Torrent
	Viewer  Viewer
	// Tier is the viewer's tier from the claims; "" is free.
	Tier     string
	SignedIn bool
	// ClaimCapMbps is the cap in the viewer's own claims — the one thp
	// applies to everything this session downloads; 0 — none. The cap thp
	// reports (Viewer.CapMbps) wins when there is one.
	ClaimCapMbps float64
	Offers       Offers
	// SizeBytes is the file (or torrent) the download ETA prices, 0 unknown.
	SizeBytes int64
	// BitrateMbps is what the played file needs, 0 unknown.
	BitrateMbps float64
	// HeldBps is the swarm's rate as the hold keeps it (Hold.Swarm): the
	// rate it last moved at, for HoldFor after it stopped; 0 once the hold
	// is over. Read only while the swarm does not move now: it stays on the
	// chain through the gaps between its pieces drawn as it last moved --
	// its last speed and the key that went with it, no verdict about a
	// stillness of seconds.
	HeldBps float64
	// LastViewer is the viewer's last reading on the chain (Meter.Last),
	// while Viewer says they left: View.Playing is built from it, for the
	// page to show while its own player plays or buffers. Zero -- none.
	LastViewer Viewer
}

// Build is the view for in. It never fails: a missing input degrades what is
// said, never whether the page gets a view.
func Build(in Input) *View {
	// The proxy's word on their requests says whether the viewer takes
	// part (Viewer.Present), never the speed: a viewer thp saw no request
	// of is a known absence -- nobody downloading -- whatever number the
	// window's tail or a verdict still carries.
	if !takesPart(in.Viewer) {
		in.Viewer = Viewer{Known: in.Viewer.Known, CapMbps: in.Viewer.CapMbps}
	}
	b := &builder{in: in, v: in.Viewer}
	if in.Tier == "" {
		b.in.Tier = "free"
	}
	b.cap = in.ClaimCapMbps
	if b.v.Known && b.v.CapMbps > 0 {
		b.cap = b.v.CapMbps
	}
	t := in.Torrent
	moves := SwarmMoving(t)
	if !moves && in.HeldBps > 0 && sendsSwarm(t.State) {
		t.RateBps = in.HeldBps
		t.Paused, t.NoSeeders, t.Checking, t.Settling = false, false, false, false
		moves = true
	}
	b.in.Torrent = t
	b.key = key(t, b.v, b.cap, moves)
	b.swarm, b.you = participants(t, b.v, b.key, moves)
	if in.Offers != nil {
		b.promo = in.Offers.Promo()
	}
	v := b.build()
	// Gone by the proxy's count, and seen with a number before: the view
	// with them still there, for the page's own player (View.Playing).
	if l := in.LastViewer; in.Viewer.Known && !takesPart(in.Viewer) && takesPart(l) && (l.Mbps > 0 || l.Limited || l.PlanBox) {
		alt := in
		alt.Viewer, alt.LastViewer = l, Viewer{}
		v.Playing = Build(alt)
	}
	return v
}

// SwarmMoving: the swarm sends the torrent to the cache -- caching it, or
// through it into Vault -- at a rate its label shows. The status loop gives
// the view a rate only while the swarm's bytes arrive (Completed grew just
// now), and zeroes the rate of a paused, empty or unverdicted swarm and of
// one below half a kilobyte a second.
func SwarmMoving(t Torrent) bool {
	return sendsSwarm(t.State) && Quantize(BytesToMbps(t.RateBps)) > 0
}

// sendsSwarm: the states in which the swarm sends anything at all -- to the
// cache, or through it into Vault.
func sendsSwarm(state string) bool { return state == "caching" || state == "vaulting" }

// participants are who the chain draws besides the source. The swarm, where
// it sends anything at all (caching, or into Vault): while it moves or is
// held through a gap, and while the viewer waits -- then it is what they
// wait for; before anything is cached, only as the reason the viewer waits
// on pieces nobody has. The viewer: while a request of theirs is open
// (Viewer.Present) -- bytes going to them, the plan's cap, a wait for data,
// or no number yet. A viewer the proxy says nothing about is never drawn.
func participants(t Torrent, v Viewer, k string, swarmMoves bool) (swarm, you bool) {
	you = takesPart(v)
	switch {
	case sendsSwarm(t.State):
		swarm = swarmMoves || (v.Known && v.Stalled)
	case t.State == "idle":
		swarm = k == KeyMissing
	}
	return swarm, you
}

type builder struct {
	in  Input
	v   Viewer
	cap float64
	key string
	// swarm and you: who takes part (participants).
	swarm, you bool
	// promo is the promo plan read once, so the button, its label and the
	// ETA quote one snapshot of the catalog.
	promo *offer.Offer
}

func (b *builder) t(key string) string { return i18n.TranslateWithLocalizer(b.in.Loc, key) }

func (b *builder) td(key string, data map[string]any) string {
	return i18n.TranslateWithLocalizerData(b.in.Loc, key, data)
}

func (b *builder) tn(key string, n int) string {
	return i18n.TranslateWithLocalizerPlural(b.in.Loc, key, n, nil)
}

func (b *builder) speed(v float64) string { return speedLabel(b.in.Loc, b.in.Lang, v) }

// key is the state: the cause, drawn as the chain or the badge by what
// moves (Mode). Vault's facts come first (they outrank what the seeder
// says), then the torrent's, then the viewer's; within caching the order is
// the order of blame -- a swarm that is not there, the plan, pieces nobody
// has, a pause, a stall. swarmMoves: the swarm moves now, or is held
// through a gap (Build draws it as it last moved). present: the viewer is on
// the chain -- a request of theirs open, whatever its speed (Build has
// already made anyone else a known absence). limited: the cap is the cause
// -- its fact, or its box still held (capped).
func key(t Torrent, v Viewer, capMbps float64, swarmMoves bool) string {
	present := takesPart(v)
	limited := present && capped(v)
	waiting := present && v.Stalled
	switch t.State {
	case "vaulted":
		switch {
		case limited:
			return KeyVaultedTier
		case present:
			return KeyVaulted
		}
		return KeyVaultedIdle
	case "vaulting":
		switch {
		case present:
			return KeyVaulting
		case swarmMoves:
			return KeyVaultingOnly
		case missingHere(t) && !t.Settling:
			return KeyVaultMissing
		}
		return KeyVaultingIdle
	case "vault_waiting":
		return KeyVaultWait
	case "vault_failed":
		return KeyVaultFailed
	case "unknown":
		return KeyUnknown
	case "cached":
		switch {
		case limited:
			return KeyCachedTier
		case present:
			return KeyCachedFlow
		}
		return KeyCached
	case "caching":
		switch {
		case t.Checking:
			return KeyChecking
		case t.NoSeeders:
			return KeyNoSeed
		// A few slow seeders and the viewer at the cap anyway: they read
		// what is cached already, and the rest waits for the swarm with or
		// without a plan -- selling one there quotes a wait it cannot keep.
		case limited && !swarmBound(t, capMbps):
			return KeyTier
		// The viewer waits on a piece nobody connected has.
		case waiting && t.ReaderMissing > 0 && missingHere(t):
			return KeyMissing
		// Nothing moves, and pieces nobody has are why it cannot: that
		// is the story, before "nobody is downloading this". Not while
		// the stream is still settling: a swarm that downloads has not
		// had the time to show it yet.
		case !swarmMoves && !present && missingHere(t) && !t.Settling:
			return KeyMissingIdle
		// A paused swarm while the viewer is on the chain: their bytes
		// come from what is cached, and "nobody is downloading this" is
		// false.
		case t.Paused && !present:
			return KeyPaused
		case waiting:
			return KeyStalled
		// Few slow seeders say why the viewer's bytes are slow -- which
		// is nothing to a viewer who gets none.
		case swarmMoves && v.Known && !present:
			return KeyCachingOnly
		case swarmMoves && swarmBound(t, capMbps):
			return KeySwarm
		case swarmMoves && present:
			return KeyActive
		case swarmMoves:
			return KeyCachingOnly
		case present:
			return KeyActive
		}
		return KeyCachingIdle
	case "idle":
		// Nothing cached yet, and the swarm known: the pieces nobody has
		// are the story before anything is -- the head of a file nobody
		// connected has keeps a torrent at 0% for good.
		switch {
		case waiting && t.ReaderMissing > 0 && missingHere(t):
			return KeyMissing
		case !present && missingHere(t) && !t.Settling:
			return KeyMissingIdle
		}
	}
	if t.Pending {
		return KeyChecking
	}
	return KeyIdleTorrent
}

// capped: the plan's cap is the cause of the viewer's state -- its fact
// (Viewer.Limited), or its box still held after a dip under the cap
// (Viewer.PlanBox). Never a held box over a wait: nothing is sold on a
// stall, and the swarm or the cache is what the viewer waits for.
func capped(v Viewer) bool {
	return v.Limited || (v.PlanBox && !v.Stalled)
}

// missingHere: the seeder knows the connected peers' pieces, there are peers
// and no seeder among them, and some pieces nobody of them has -- wanted
// ones, or any not complete here. Until the seeder knows, its union is a
// lower bound: holes that are not there.
func missingHere(t Torrent) bool {
	return t.AvailabilityKnown && t.Seeders == 0 && t.Peers > 0 && (t.WantedMissing > 0 || t.Missing)
}

// swarmBound: a few seeders and a swarm slower than the cap — the swarm is
// the bottleneck, and no plan would help.
func swarmBound(t Torrent, capMbps float64) bool {
	if t.Seeders < 1 || t.Seeders > fewSeeders {
		return false
	}
	s := Quantize(BytesToMbps(t.RateBps))
	return s > 0 && (capMbps <= 0 || s < capMbps)
}

func isTierKey(k string) bool {
	return k == KeyTier || k == KeyCachedTier || k == KeyVaultedTier
}

func (b *builder) build() *View {
	v := &View{Key: b.key, Mode: ModeBadge, Tier: b.in.Tier, Auth: "anon"}
	if b.in.SignedIn {
		v.Auth = "user"
	}
	if b.swarm || b.you {
		v.Mode = ModeChain
	}
	v.Nodes[0] = b.swarmNode()
	v.Nodes[1] = b.sourceNode()
	v.Segs[0] = b.swarmSeg()
	v.Segs[1], v.Nodes[2] = b.viewerSeg()
	v.Nodes[0].Show, v.Segs[0].Show = b.swarm, b.swarm
	v.Nodes[2].Show, v.Segs[1].Show = b.you, b.you
	v.Badge = b.badge()
	v.Bar = b.bar()
	if isTierKey(b.key) {
		v.Plan = b.plan()
	} else {
		v.Hint = b.hint()
	}
	v.Vault = b.key == KeyMissing || b.key == KeyMissingIdle
	v.Details = b.details(v)
	v.Sticky = v.Mode == ModeChain
	return v
}

// isMissing: a state whose story is the pieces nobody connected has.
func isMissing(k string) bool {
	return k == KeyMissing || k == KeyMissingIdle || k == KeyVaultMissing
}

// isVaultTransfer: the swarm into Vault, moving or not.
func isVaultTransfer(k string) bool {
	return k == KeyVaulting || k == KeyVaultingOnly || k == KeyVaultingIdle || k == KeyVaultMissing
}

func (b *builder) swarmNode() Node {
	t := b.in.Torrent
	n := Node{Kind: "swarm", Icon: "swarm", Name: b.t("resource.status.chain.swarm"), Caption: b.t("resource.status.chain.swarmCaption")}
	switch {
	case b.key == KeyUnknown:
		n.Value, n.Short, n.Dim = "?", "?", true
		return n
	case isMissing(b.key):
		// "12 peers · 0 seeders": who is there, and that nobody of them
		// has the whole file.
		n.Value = b.tn("resource.status.peers", t.Peers) + " · " + b.tn("resource.status.seeders", t.Seeders)
		n.Short = strconv.Itoa(t.Peers)
		return n
	}
	if t.SwarmKnown || t.NoSeeders || b.key == KeyVaultWait {
		if t.Seeders > 0 || t.Peers == 0 {
			n.Value, n.Short = b.tn("resource.status.seeders", t.Seeders), strconv.Itoa(t.Seeders)
		} else {
			n.Value, n.Short = b.tn("resource.status.peers", t.Peers), strconv.Itoa(t.Peers)
		}
	}
	if b.key == KeyNoSeed || b.key == KeyVaultWait {
		n.Tone = "err"
	}
	return n
}

// pct is a progress as a whole percent, rounded down: a transfer at 99.6%
// is not done, and "100%" next to "caching" would say it is.
func pct(p float64) int {
	if !(p > 0) {
		return 0
	}
	return int(math.Min(math.Floor(p), 100))
}

// availPct is the seeder's availability (a float32 share) as a whole
// percent, rounded down but not below what it says: 0.29 arrives as
// 0.28999999, which is 29%.
func availPct(a float64) int {
	return pct(math.Round(a*1e4) / 100)
}

// sourceNode is the chain's middle node: the cache (owner, 2026-09-25: "Рой
// ▸ Кэш ▸ Вы", not the brand) with its share of the torrent, the check once
// the whole torrent is there; or Vault, with its share while the torrent is
// saved to it, "saved" once it is.
func (b *builder) sourceNode() Node {
	t := b.in.Torrent
	cache := Node{Show: true, Kind: "cache", Icon: "cloud", Name: b.t("resource.status.chain.cache"), Caption: b.t("resource.status.chain.cacheCaption")}
	vault := Node{Show: true, Kind: "vault", Icon: "vault", Name: nameVault, Caption: nameVault, Tone: "vault"}
	share := func(n Node, p int) Node {
		n.Value = b.td("resource.status.chain.pct", map[string]any{"Pct": p})
		n.Short = n.Value
		return n
	}
	// The whole torrent is in the cache: the green check says it, and no
	// word goes next to it.
	whole := func() Node {
		cache.Icon, cache.Tone = "check", "cached"
		return cache
	}
	switch {
	case b.key == KeyCached || b.key == KeyCachedFlow || b.key == KeyCachedTier:
		return whole()
	case isVaultTransfer(b.key):
		// The swarm into Vault, whether or not the viewer downloads too:
		// Vault's node and its share.
		return share(vault, pct(t.Progress))
	case (b.key == KeyVaultWait || b.key == KeyVaultFailed) && b.you:
		// Vault has stored nothing it could send: the viewer's bytes come
		// from the cache, which the chain names; what Vault is doing is the
		// hint's to say. Without the seeder's numbers there is no share of
		// the cache to quote.
		switch c := pct(t.CacheProgress); {
		case !t.SwarmKnown:
			return cache
		case c >= 100:
			return whole()
		default:
			return share(cache, c)
		}
	case b.key == KeyVaultWait:
		vault.Dim = true
		vault.Value = b.t("resource.status.chain.vaultWaiting")
		return vault
	case b.key == KeyVaultFailed:
		vault.Tone = "warn"
		vault.Value = b.t("resource.status.chain.vaultRetrying")
		return vault
	case b.key == KeyVaulted || b.key == KeyVaultedTier || b.key == KeyVaultedIdle:
		vault.Value = b.t("resource.status.chain.saved")
		return vault
	case b.key == KeyUnknown:
		cache.Value, cache.Short, cache.Dim = "?", "?", true
		return cache
	}
	if t.State != "caching" && !isMissing(b.key) {
		// Nothing to quote: the page before the first status, or a torrent
		// with nothing cached (idle_torrent). The chain draws the latter
		// only with the viewer on it -- a request of theirs open, their
		// first bytes on the way -- and "waiting" next to the cache read
		// as nobody being there, right next to "You". Without a share the
		// seeder's numbers can back (the stats may be gone: a torrent the
		// seeder unloaded is idle too), no "0%" either.
		return cache
	}
	return share(cache, pct(t.Progress))
}

// swarmSeg is the swarm's link as it is now; whether it is drawn is
// participants'.
func (b *builder) swarmSeg() Seg {
	s := Seg{Kind: "swarm", Tone: "off"}
	t := b.in.Torrent
	rate := Quantize(BytesToMbps(t.RateBps))
	flow := func(tone string) {
		s.Tone = tone
		if rate > 0 {
			s.On, s.Speed = true, b.speed(rate)
			return
		}
		s.Speed = dashLabel(b.in.Loc)
	}
	switch {
	case b.key == KeyChecking:
		s.Dots = true
	case isMissing(b.key):
		// Amber and still: the swarm is why, and nothing comes.
		s.Tone, s.Speed, s.Note = "swarm", b.speed(0), b.t("resource.status.chain.noPieces")
	case b.key == KeyNoSeed || b.key == KeyIdleTorrent || b.key == KeyUnknown || b.key == KeyVaultWait || b.key == KeyVaultFailed:
		s.Speed = dashLabel(b.in.Loc)
	case isVaultTransfer(b.key):
		flow("vault")
	case b.key == KeySwarm:
		flow("swarm")
		s.Note = b.t("resource.status.chain.fewSeeders")
	case b.key == KeyPaused || t.Paused:
		s.Tone, s.Speed = "pause", b.t("resource.status.chain.paused")
	default:
		flow("flow")
	}
	return s
}

// viewerSeg is the last link and the viewer's node, as they are now;
// whether they are drawn is participants'.
func (b *builder) viewerSeg() (Seg, Node) {
	you := Node{Kind: "you", Icon: "user", Name: b.t("resource.status.chain.you"), Caption: b.t("resource.status.chain.youCaption")}
	s := Seg{Kind: "viewer", Tone: "off"}
	v := b.v
	switch {
	case !v.Known:
	case v.Limited:
		s.Tone, s.On = "plan", true
		s.Speed, s.Note = b.speed(Quantize(b.cap)), b.t("resource.status.chain.cap")
	case v.Stalled:
		// Amber blames the swarm, which is right only while the content
		// comes from it: whole on our side (the cache, Vault), a request
		// waiting is something else, and the link is drawn neutral.
		s.Tone = "swarm"
		if st := b.in.Torrent.State; st == "cached" || st == "vaulted" {
			s.Tone = "off"
		}
		s.Speed, s.Note = b.speed(0), b.t("resource.status.chain.waitingData")
	case v.Mbps > 0:
		s.Tone, s.On, s.Speed = "flow", true, b.speed(v.Mbps)
	default:
		s.Speed = dashLabel(b.in.Loc)
	}
	return s, you
}

// badge is the state's badge as the page had it before the chain: the
// torrent's state in its colour, the percent where it had one, the swarm in
// brackets where it had it.
func (b *builder) badge() Badge {
	t := b.in.Torrent
	ps := b.td("resource.status.chain.pct", map[string]any{"Pct": pct(t.Progress)})
	swarm := b.swarmSuffix()
	switch {
	case b.key == KeyChecking:
		// Cyan and the dots, as approved (2026-09-25); the old one was grey.
		return Badge{Tone: "cyan", Icon: "dots", Label: b.t("resource.status.checking")}
	case b.key == KeyNoSeed:
		return Badge{Tone: "err", Icon: "noseed", Label: b.t("resource.status.noSeeders") + " · " + ps}
	case b.key == KeyPaused:
		return Badge{Tone: "warn", Icon: "pause", Label: b.t("resource.status.cachingPaused") + " " + ps, Extra: swarm}
	case b.key == KeyMissing || b.key == KeyMissingIdle:
		return Badge{Tone: "warn", Icon: "warn", Label: b.t("resource.status.missing") + " · " + ps,
			Extra: "(" + b.tn("resource.status.peers", t.Peers) + ", " + b.tn("resource.status.seeders", t.Seeders) + ")"}
	case b.key == KeyIdleTorrent:
		return Badge{Tone: "muted", Icon: "idle", Label: b.t("resource.status.idle"), Extra: swarm}
	case b.key == KeyCached || b.key == KeyCachedFlow || b.key == KeyCachedTier:
		return Badge{Tone: "ok", Icon: "check", Label: b.t("resource.status.cached")}
	case b.key == KeyUnknown:
		return Badge{Tone: "muted", Icon: "unknown", Label: b.t("resource.status.unknown")}
	case b.key == KeyVaultMissing:
		return Badge{Tone: "vault", Icon: "clock", Pulse: true, Label: b.t("resource.status.vault_missing") + " · " + ps}
	case isVaultTransfer(b.key):
		return Badge{Tone: "vault", Icon: "up", Pulse: true, Label: b.t("resource.status.vaulting") + " " + ps, Extra: swarm}
	case b.key == KeyVaultWait:
		return Badge{Tone: "vault", Icon: "clock", Pulse: true, Label: b.t("resource.status.vault_waiting")}
	case b.key == KeyVaultFailed:
		l := b.t("resource.status.vault_failed")
		if pct(t.Progress) > 0 {
			l += " " + ps
		}
		return Badge{Tone: "warn", Icon: "warn", Label: l, Extra: swarm}
	case b.key == KeyVaulted || b.key == KeyVaultedTier || b.key == KeyVaultedIdle:
		// Vault's purple and its layers, as approved (2026-09-25); the old
		// one was the green shield.
		return Badge{Tone: "vault", Icon: "vault", Label: b.t("resource.status.vaulted")}
	}
	// Caching, whatever moves: the old "Caching N%".
	return Badge{Tone: "cyan", Icon: "down", Pulse: true, Label: b.t("resource.status.caching") + " " + ps, Extra: swarm}
}

// swarmSuffix is the badge's swarm in brackets: seeders and leechers when
// the seeder splits them ("(14 seeders · 9 leechers)", with no "0 leechers"
// tail), the combined peers otherwise, nothing when nothing is known.
func (b *builder) swarmSuffix() string {
	t := b.in.Torrent
	s := ""
	switch {
	case t.Leechers > 0:
		s = b.tn("resource.status.seeders", t.Seeders) + " · " + b.tn("resource.status.leechers", t.Leechers)
	case t.Seeders > 0:
		s = b.tn("resource.status.seeders", t.Seeders)
	case t.Peers > 0:
		s = b.tn("resource.status.peers", t.Peers)
	}
	if s == "" {
		return ""
	}
	return "(" + s + ")"
}

// bar draws the pieces where the torrent is on its way to Webtor or Vault
// (handlers/resource barStates strips them everywhere else) -- Vault's wait
// for seeders too, over what the cache already holds, as approved -- and
// before anything is cached only where there are pieces nobody has to
// hatch; everything else is the hairline divider.
func (b *builder) bar() Bar {
	t := b.in.Torrent
	if !t.Pieces {
		return Bar{Mode: "divider"}
	}
	switch t.State {
	case "caching":
		return Bar{Mode: "pieces", Tone: "flow"}
	case "idle":
		if t.Missing {
			return Bar{Mode: "pieces", Tone: "flow"}
		}
	case "vaulting", "vault_failed", "vault_waiting":
		return Bar{Mode: "pieces", Tone: "vault"}
	}
	return Bar{Mode: "divider"}
}

// hint says the state's cause -- only the cause: never what a
// subscription would or would not do about it.
func (b *builder) hint() string {
	t := b.in.Torrent
	missing := map[string]any{"Pct": availPct(t.Availability)}
	switch {
	case b.key == KeySwarm:
		return b.tn("resource.status.hint.swarm", t.Seeders)
	case b.key == KeyStalled:
		return b.t("resource.status.hint.stalled")
	case b.key == KeyMissing:
		return i18n.TranslateWithLocalizerPlural(b.in.Loc, "resource.status.hint.missing", t.Peers, missing)
	case b.key == KeyMissingIdle:
		return i18n.TranslateWithLocalizerPlural(b.in.Loc, "resource.status.hint.missingIdle", t.Peers, missing)
	case b.key == KeyPaused:
		return b.t("resource.status.cachingPausedHint")
	case b.key == KeyNoSeed:
		return b.t("resource.status.noSeedersHint")
	case b.key == KeyUnknown:
		return b.t("resource.status.unknownHint")
	case b.key == KeyVaultMissing:
		return i18n.TranslateWithLocalizerPlural(b.in.Loc, "resource.status.hint.vaultMissing", t.Peers, missing)
	case isVaultTransfer(b.key):
		return b.t("resource.status.hint.vaulting")
	case b.key == KeyVaultWait:
		return b.t("resource.status.vault_waitingHint")
	case b.key == KeyVaultFailed:
		return b.t("resource.status.vault_failedHint")
	}
	return ""
}

func (b *builder) paid() bool { return b.in.Tier != "free" && b.in.Tier != "nobody" }

// CapLine is the cap as a fact about the viewer: "Without a subscription —
// up to 5 Mbps" for anyone who does not pay (never "your plan": an anonymous
// viewer has none), "Your subscription — up to 20 Mbps" for one who does.
func CapLine(loc *goi18n.Localizer, lang string, paid bool, capMbps float64) string {
	k := "resource.status.capFree"
	if paid {
		k = "resource.status.capPaid"
	}
	return i18n.TranslateWithLocalizerData(loc, k, map[string]any{"Rate": FormatNumber(lang, Quantize(capMbps))})
}

// StallSub is the stream box's line while the player buffers: the cap, and
// what the file needs when that is known — "Without a subscription — up to
// 5 Mbps, and this file needs 8 Mbps". Only the stream job knows the file's
// bitrate (jobs/scripts StreamContent.StatusStallSub); the status stream
// sends the line without it. "" without a cap: nothing binds.
func StallSub(loc *goi18n.Localizer, lang string, paid bool, capMbps, needMbps float64) string {
	if Quantize(capMbps) == 0 {
		return ""
	}
	line := CapLine(loc, lang, paid, capMbps)
	// Only over the cap as the labels say them: "up to 5 Mbps, and this
	// file needs 4.6" -- a file in FitsMargin -- explains nothing.
	if need := Quantize(needMbps); need > 0 && OverCap(capMbps, needMbps) {
		line = i18n.TranslateWithLocalizerData(loc, "resource.status.streamNeeds", map[string]any{"Cap": line, "Need": speedLabel(loc, lang, need)})
	}
	return line
}

// FitsMargin is how much more than its estimate (jobs/scripts
// playedBitrate) a stream may really pull before "it fits under the cap"
// stops being a safe thing to say of it. The estimate is the tracks'
// average rate; what crosses thp is MPEG-TS segments of a stretch of the
// film. Recorded in Chrome at a 5M cap (2026-09-26, The Knick s02e01,
// 720p H.264, two AC3 dubs): estimated 4.56 Mbit/s (4.34 in the cap's
// megabit); the first 132 s of video came to 83.3 MB, 5.05 Mbit/s against
// 4.42 for the file less its audio (+14.4%: TS packets and a scene heavier
// than the average), and the transcoder's AAC to 236 kbit/s against the
// 139.6 its encoder is set to (233-242 over three runs of two files) --
// 5.29 in all (5.04 in the cap's megabit), 1.16 times the estimate and just
// over the cap. It stalled four times in 180 s while marked "fits". 1.15
// would have kept that mark (0.869 of the cap x 1.15 = 0.999); 1.2 drops it
// with four points to spare. Of 1601 H.264 files in 24 h of transcoder
// probes with every number in ffprobe's own fields, it moves 98 from "fits"
// to unknown, 8 more than 1.15 would.
const FitsMargin = 1.2

// FitsCap: the stream's bitrate is known and, FitsMargin over it, no more
// than the cap. Only a playing player uses it: such a file plays smoothly
// at the cap, so nothing is said under the bar while it plays (the stream
// job marks the player, jobs/scripts StreamContent.StatusFitsCap). A real
// stall of it while the limiter binds still gets the stream box -- the
// estimate is not proof (lib/transferStatus.js present). A file in the
// margin is neither this nor OverCap: unknown.
func FitsCap(capMbps, needMbps float64) bool {
	c := Quantize(capMbps)
	return Quantize(needMbps) > 0 && c > 0 && needMbps*FitsMargin <= c
}

// OverCap: the file's bitrate is known and above the cap, as the labels say
// them -- FitsCap's other side; a file whose bitrate is not known, or that
// is under the cap by less than FitsMargin, is neither. At the cap such a
// player stalls once its buffer runs out, however smoothly it plays now:
// the page shows the stream box as soon as it is due, without waiting for
// the first stall (owner, 2026-09-25/26; the stream job marks the player,
// jobs/scripts StreamContent.StatusOverCap).
func OverCap(capMbps, needMbps float64) bool {
	need, c := Quantize(needMbps), Quantize(capMbps)
	return need > 0 && c > 0 && need > c
}

// cta is the plan box's button for this viewer, without its label, and
// whether there is one. A free viewer is sold the promo plan when it is
// faster than their cap: its trial through /trial when the checkout can
// start one, else its checkout, else /donate. A paying viewer is sent to
// compare plans when one on sale is faster. Nothing faster, or nothing on
// sale — no button: a link that leads to nothing faster sells nothing.
func (b *builder) cta() (CTA, bool) {
	if b.in.Offers == nil {
		return CTA{}, false
	}
	if b.paid() {
		if !b.in.Offers.FasterOnSale(b.cap) {
			return CTA{}, false
		}
		return CTA{URL: i18n.LangPath(b.in.Lang, "/donate"), Target: "donate"}, true
	}
	promo := b.promo
	if promo == nil || (promo.RateMbps > 0 && float64(promo.RateMbps) <= b.cap) {
		return CTA{}, false
	}
	c := CTA{}
	// FromStatusBar is a known surface: TrialURL cannot fail on it.
	if u, _ := offer.TrialURL(b.in.Lang, offer.FromStatusBar, promo); u != "" {
		c.URL, c.Target = u, "trial"
		c.Note = b.tn("offer.trialNote", promo.TrialDays)
	} else if promo.URL != "" {
		c.URL, c.Target = promo.URL, "checkout"
	} else {
		c.URL, c.Target = i18n.LangPath(b.in.Lang, "/donate"), "donate"
	}
	return c, true
}

// plan is the plan-limited state's line. The fact and the cap alone come
// with the fact (Viewer.Limited); the variants -- a box, or its line where
// nothing is on sale -- only once the box is due (Viewer.PlanBox): until
// then nothing stands in for it, so the page grows once, by the box.
func (b *builder) plan() *Plan {
	capN := FormatNumber(b.in.Lang, Quantize(b.cap))
	capLine := CapLine(b.in.Loc, b.in.Lang, b.paid(), b.cap)
	p := &Plan{Fact: b.td("resource.status.streamSmooth", map[string]any{"Cap": capLine}), Cap: capLine}
	if !b.v.PlanBox {
		return p
	}
	needs := StallSub(b.in.Loc, b.in.Lang, b.paid(), b.cap, b.in.BitrateMbps)
	title := b.td("action.download.limitedTitle", map[string]any{"Rate": capN})
	c, ok := b.cta()
	if !ok {
		p.Download.Hint = title
		p.Stream.Hint = needs
		return p
	}
	dl, st := c, c
	if b.paid() {
		dl.Label = b.t("action.upgrade.paid")
		st.Label = dl.Label
	} else {
		dl.Label = b.downloadLabel()
		st.Label = b.t("offer.watchUncapped")
	}
	p.Download.Box = &Box{Title: title, Sub: b.downloadSub(), CTA: dl}
	// Also for a file the estimate says fits: the page shows it only at a
	// real stall there, and a stall at the cap is the cap's doing (Stream).
	p.Stream.Box = &Box{Title: b.t("resource.status.streamStallTitle"), Sub: needs, CTA: st}
	return p
}

// downloadLabel promises the outcome: "up to N× faster" while the swarm may
// still be the slower side, "N× faster" once the file is whole on our side
// (cached, Vault) and the cap is the only brake.
func (b *builder) downloadLabel() string {
	x := offer.SpeedUp(b.promo, int(math.Round(b.cap)))
	switch {
	case x == 0:
		return b.t("offer.downloadFasterPlain")
	case b.key == KeyTier:
		return b.tn("offer.downloadUpTo", x)
	}
	return b.tn("offer.downloadFaster", x)
}

// downloadSub prices this file's wait at the cap and with the promo plan
// ("1.2 GB — about 32 min. With a subscription — about 3 min"), after what
// is special about the source: whole in the cache, or served from Vault.
func (b *builder) downloadSub() string {
	prefix := ""
	switch b.key {
	case KeyCachedTier:
		prefix = b.t("resource.status.cachedWhole")
	case KeyVaultedTier:
		prefix = b.t("resource.status.vaultedServe")
	}
	eta := ""
	if promo := b.promo; !b.paid() && promo != nil {
		if pitch := offer.PitchWith(promo, b.in.SizeBytes, int(math.Round(b.cap)), func(key string, data map[string]any) string { return b.td(key, data) }); pitch != nil {
			eta = pitch.ETA
		} else if promo.RateMbps > 0 {
			eta = b.td("action.download.limitedSub", map[string]any{"Rate": promo.RateMbps})
		} else {
			eta = b.t("action.download.limitedSubUnlimited")
		}
	}
	switch {
	case prefix == "":
		return eta
	case eta == "":
		return prefix
	}
	return prefix + " " + eta
}

func (b *builder) details(v *View) Details {
	d := Details{Title: b.t("resource.status.details.title")}
	t := b.in.Torrent
	swarm := Row{Show: v.Nodes[0].Show, Key: "swarm", Label: b.t("resource.status.details.swarm"), Value: v.Segs[0].Speed}
	if swarm.Show && t.Seeders > 0 {
		swarm.Sub = b.tn("resource.status.details.swarmSub", t.Seeders)
	}
	source := Row{Show: true, Key: "source", Label: v.Nodes[1].Name, Value: v.Nodes[1].Value, Sub: b.t("resource.status.details.webtorSub")}
	if n := v.Nodes[1]; n.Kind == "cache" && n.Icon == "check" {
		// The chain's check, in words the row can hold: all of it.
		source.Value = b.td("resource.status.chain.pct", map[string]any{"Pct": 100})
	}
	switch {
	case v.Nodes[1].Kind != "vault":
	case b.key == KeyVaultWait || b.key == KeyVaultFailed:
		// Nothing is stored yet: "a stored copy, no seeders needed" would
		// contradict the hint right under it.
		source.Sub = b.t("resource.status.details.vaultPendingSub")
	case isVaultTransfer(b.key):
		// Part of it is stored, the rest still comes from the swarm: "no
		// seeders needed" would contradict the swarm's row above it and
		// the hint under it ("…later available without seeders").
		source.Sub = b.t("resource.status.details.vaultSavingSub")
	default:
		source.Sub = b.t("resource.status.details.vaultSub")
	}
	you := Row{Show: v.Nodes[2].Show, Key: "you", Label: b.t("resource.status.details.you"), Value: v.Segs[1].Speed}
	if you.Show {
		if b.v.Limited {
			you.Tag = b.t("resource.status.chain.cap")
		}
		mbps := b.v.Mbps
		if b.v.Limited {
			mbps = b.cap
		}
		// What a browser's download manager shows: megabytes, not bits.
		if mb := Quantize(mbps / 8); mb > 0 && !b.v.Stalled {
			you.Sub = b.td("resource.status.details.youSub", map[string]any{"N": FormatNumber(b.in.Lang, mb)})
		}
	}
	capRow := Row{Show: b.cap > 0, Key: "cap", Sub: b.t("resource.status.details.capSub")}
	if capRow.Show {
		capRow.Label = b.t("resource.status.details.capFree")
		if b.paid() {
			capRow.Label = b.t("resource.status.details.capPaid")
		}
		capRow.Value = b.td("resource.status.details.capValue", map[string]any{"Rate": FormatNumber(b.in.Lang, Quantize(b.cap))})
	}
	d.Rows = [4]Row{swarm, source, you, capRow}
	return d
}
