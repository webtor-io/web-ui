package statusview

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
)

var i18nSvc = i18n.New(os.DirFS("../../locales"))

func loc(lang string) *goi18n.Localizer { return i18nSvc.Localizer(lang) }

// fakeOffers is the storefront of 2026-09-24: silver (50 Mbps, 7-day trial)
// is the promo plan, gold (100) the fastest plan on sale.
type fakeOffers struct {
	promo  *offer.Offer
	faster func(float64) bool
}

func (f fakeOffers) Promo() *offer.Offer { return f.promo }
func (f fakeOffers) FasterOnSale(r float64) bool {
	if f.faster == nil {
		return false
	}
	return f.faster(r)
}

func liveOffers() fakeOffers {
	return fakeOffers{
		promo:  &offer.Offer{Tier: "silver", PeriodDays: 30, RateMbps: 50, TrialDays: 7, URL: "https://pay.example/silver"},
		faster: func(r float64) bool { return r < 100 },
	}
}

// mbpsBytes is a swarm rate in bytes a second that reads v Mbps.
func mbpsBytes(v float64) float64 { return v * mbit / 8 }

// gb12 is the design's "1,2 ГБ" file.
const gb12 = 1288490189

// chain renders the chain the way the design reads: nodes by name and value,
// "(tone)" and "~" for dimmed; segments as [tone(+ when the sweep runs)
// speed · note]. Hidden slots -- the ones not on the chain -- are left out.
func chain(v *View) string {
	var parts []string
	node := func(n Node) {
		if !n.Show {
			return
		}
		s := n.Name
		if n.Value != "" {
			s += " " + n.Value
		}
		if n.Tone != "" {
			s += " (" + n.Tone + ")"
		}
		if n.Dim {
			s += "~"
		}
		parts = append(parts, s)
	}
	seg := func(g Seg) {
		if !g.Show {
			return
		}
		s := "[" + g.Tone
		if g.On {
			s += "+"
		}
		if g.Dots {
			s += " …"
		}
		if g.Speed != "" {
			s += " " + g.Speed
		}
		if g.Note != "" {
			s += " · " + g.Note
		}
		parts = append(parts, s+"]")
	}
	node(v.Nodes[0])
	seg(v.Segs[0])
	node(v.Nodes[1])
	seg(v.Segs[1])
	node(v.Nodes[2])
	return strings.ReplaceAll(strings.Join(parts, " "), "\u00a0", " ")
}

// badge renders the badge the way the design reads: "tone icon(+ when it
// pulses) label extra".
func badge(v *View) string {
	b := v.Badge
	s := b.Tone + " " + b.Icon
	if b.Pulse {
		s += "+"
	}
	s += " " + b.Label
	if b.Extra != "" {
		s += " " + b.Extra
	}
	return nb(s)
}

func nb(s string) string { return strings.ReplaceAll(s, "\u00a0", " ") }

func caching(pct float64, seeders int, rate float64) Torrent {
	return Torrent{State: "caching", Progress: pct, Seeders: seeders, SwarmKnown: true, RateBps: mbpsBytes(rate), Pieces: true}
}

// holes is a caching torrent whose swarm has 12 peers, no seeder, and 73%
// of the torrent between them and us: some pieces nobody connected has.
func holes(state string, pct float64) Torrent {
	return Torrent{State: state, Progress: pct, Peers: 12, SwarmKnown: true, Pieces: true,
		AvailabilityKnown: true, Availability: 0.73, Missing: true, WantedMissing: 5}
}

// The viewer's readings as the meter gives them: on the chain (their
// requests are open) with bytes flowing, at the cap, or waiting; and zero --
// numbers arrive and say no request of theirs is open, so nothing goes to
// them.
func flowing(v float64) Viewer { return Viewer{Known: true, Present: true, Mbps: v, CapMbps: 5} }

var zero = Viewer{Known: true, CapMbps: 5}

// atCap is the viewer at the cap long enough for the plan box (the
// design's rows); capFact the first seconds of it, the pink link and no box
// yet; boxHeld the box's hold, the cap gone and the viewer still
// downloading under it.
var atCap = Viewer{Known: true, Present: true, Mbps: 5, Limited: true, PlanBox: true, CapMbps: 5}
var capFact = Viewer{Known: true, Present: true, Mbps: 5, Limited: true, CapMbps: 5}
var boxHeld = Viewer{Known: true, Present: true, Mbps: 2.5, PlanBox: true, CapMbps: 5}
var stalled = Viewer{Known: true, Present: true, Stalled: true, CapMbps: 5}

// The pitch for the design's file at 5 Mbps against silver's 50.
func eta(t *testing.T) string {
	p := offer.PitchWith(liveOffers().promo, gb12, 5, func(k string, d map[string]any) string {
		return i18n.TranslateWithLocalizerData(loc("ru"), k, d)
	})
	if p == nil {
		t.Fatal("no pitch")
	}
	return nb(fmt.Sprintf("%s — около %s. С подпиской — около %s", p.Size, p.Slow, p.Fast))
}

// designKeys reads the state keys of a design page.
func designKeys(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range strings.Split(string(b), `class="ts-key">`)[1:] {
		out = append(out, m[:strings.Index(m, "<")])
	}
	return out
}

// Every row of docs/transfer_status.html, with the design's sample numbers
// and its Russian copy. tier_dl, stream_ok, stream_stall and stream_over are
// one server state (KeyTier) — the page picks the variant by what its player
// does and what the stream job knows of its file — so they are checked
// against the variants of one plan. A row is
// either the chain (something moves, or the viewer waits: only its
// participants drawn) or the badge (nothing moves: the badge the page had
// before the chain, with the bar and the hint under it).
func TestBuild_EveryStateOfTheDesign(t *testing.T) {
	anonFree := func(tr Torrent, v Viewer) Input {
		return Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: v, ClaimCapMbps: 5, Offers: liveOffers(), SizeBytes: gb12}
	}
	missing := holes("caching", 43)
	missing.ReaderMissing = 3
	vaultMissing := holes("vaulting", 58)
	paused := Torrent{State: "caching", Progress: 43, Seeders: 14, SwarmKnown: true, Paused: true, Pieces: true}
	cases := []struct {
		design string // the row's key in docs/transfer_status.html
		in     Input
		key    string
		chain  string // the chain, for a chain row
		badge  string // the badge, for a badge row
		bar    Bar
		hint   string
		vault  bool // the hint offers Vault
		check  func(t *testing.T, v *View)
	}{
		{design: "active", in: anonFree(caching(43.4, 14, 38), flowing(12)), key: KeyActive,
			chain: "Рой 14 сидов [flow+ 38 Мбит/с] Кэш 43% [flow+ 12 Мбит/с] Вы",
			bar:   Bar{Mode: "pieces", Tone: "flow"}},
		{design: "tier_dl", in: anonFree(caching(61, 31, 38), atCap), key: KeyTier,
			chain: "Рой 31 сид [flow+ 38 Мбит/с] Кэш 61% [plan+ 5 Мбит/с · потолок] Вы",
			bar:   Bar{Mode: "pieces", Tone: "flow"},
			check: func(t *testing.T, v *View) {
				b := v.Plan.Download.Box
				if b == nil {
					t.Fatal("no download box")
				}
				if nb(b.Title) != "Скорость скачивания ограничена: 5 Мбит/с" {
					t.Errorf("title %q", b.Title)
				}
				if nb(b.Sub) != eta(t) {
					t.Errorf("sub %q, want %q", nb(b.Sub), eta(t))
				}
				// Literally, not only as the helper says: the design's
				// "около 32 мин" priced 1.2e9 bytes at 10^6 bits a
				// megabit; the cap's megabit is 2^20 (32.8 min), and sizes
				// keep the site's one format (docs/i18n.md, helpers.Bytes).
				if nb(b.Sub) != "1.2 GB — около 33 мин. С подпиской — около 3 мин" {
					t.Errorf("sub %q", nb(b.Sub))
				}
				want := CTA{Label: "Скачать до 10 раз быстрее", URL: "/ru/trial?from=status-bar", Note: "7 дней бесплатно · отмена в любой момент", Target: "trial"}
				if b.CTA != want {
					t.Errorf("cta %+v, want %+v", b.CTA, want)
				}
			}},
		{design: "stream_ok", in: anonFree(caching(61, 31, 38), atCap), key: KeyTier,
			chain: "Рой 31 сид [flow+ 38 Мбит/с] Кэш 61% [plan+ 5 Мбит/с · потолок] Вы",
			bar:   Bar{Mode: "pieces", Tone: "flow"},
			check: func(t *testing.T, v *View) {
				if nb(v.Plan.Fact) != "Без подписки — до 5 Мбит/с. Видео идёт без остановок." {
					t.Errorf("fact %q", v.Plan.Fact)
				}
			}},
		{design: "stream_stall", in: func() Input { in := anonFree(caching(61, 31, 38), atCap); in.BitrateMbps = 8; return in }(), key: KeyTier,
			chain: "Рой 31 сид [flow+ 38 Мбит/с] Кэш 61% [plan+ 5 Мбит/с · потолок] Вы",
			bar:   Bar{Mode: "pieces", Tone: "flow"},
			check: func(t *testing.T, v *View) {
				b := v.Plan.Stream.Box
				if b == nil {
					t.Fatal("no stream box")
				}
				if b.Title != "Видео подгружается медленнее, чем играет" || nb(b.Sub) != "Без подписки — до 5 Мбит/с, а файлу нужно 8 Мбит/с" {
					t.Errorf("box %q / %q", b.Title, nb(b.Sub))
				}
				if b.CTA.Label != "Смотреть без ограничения скорости" || b.CTA.URL != "/ru/trial?from=status-bar" || b.CTA.Note == "" {
					t.Errorf("cta %+v", b.CTA)
				}
			}},
		// The page's player playing a file the stream job marked over the
		// cap: the same stream box as at a stall (the page shows it before
		// the first one; lib/transferStatus.js present).
		{design: "stream_over", in: anonFree(caching(61, 31, 38), atCap), key: KeyTier,
			chain: "Рой 31 сид [flow+ 38 Мбит/с] Кэш 61% [plan+ 5 Мбит/с · потолок] Вы",
			bar:   Bar{Mode: "pieces", Tone: "flow"},
			check: func(t *testing.T, v *View) {
				b := v.Plan.Stream.Box
				if b == nil || b.Title != "Видео подгружается медленнее, чем играет" || b.CTA.Label != "Смотреть без ограничения скорости" {
					t.Fatalf("stream box %+v", b)
				}
			}},
		{design: "swarm", in: anonFree(caching(8, 2, 1.2), flowing(1.2)), key: KeySwarm,
			chain: "Рой 2 сида [swarm+ 1,2 Мбит/с · мало сидов] Кэш 8% [flow+ 1,2 Мбит/с] Вы",
			bar:   Bar{Mode: "pieces", Tone: "flow"},
			hint:  "Скорость ограничивают раздающие — их сейчас 2."},
		{design: "stalled", in: anonFree(caching(43, 14, 38), stalled), key: KeyStalled,
			chain: "Рой 14 сидов [flow+ 38 Мбит/с] Кэш 43% [swarm 0 Мбит/с · ждём данные] Вы",
			bar:   Bar{Mode: "pieces", Tone: "flow"},
			hint:  "Ждём от раздающих нужный кусок файла."},
		{design: "missing", in: anonFree(missing, stalled), key: KeyMissing,
			chain: "Рой 12 пиров · 0 сидов [swarm 0 Мбит/с · нет нужных кусков] Кэш 43% [swarm 0 Мбит/с · ждём данные] Вы",
			bar:   Bar{Mode: "pieces", Tone: "flow"},
			hint:  "12 пиров на связи, но нужных кусков нет ни у кого — у всех неполные копии. В рое есть 73% раздачи, остальное появится только с полным сидом.",
			vault: true},
		{design: "caching_only", in: anonFree(caching(43, 14, 38), zero), key: KeyCachingOnly,
			chain: "Рой 14 сидов [flow+ 38 Мбит/с] Кэш 43%",
			bar:   Bar{Mode: "pieces", Tone: "flow"}},
		{design: "cached_flow", in: anonFree(Torrent{State: "cached", Progress: 100}, flowing(24)), key: KeyCachedFlow,
			chain: "Кэш (cached) [flow+ 24 Мбит/с] Вы",
			bar:   Bar{Mode: "divider"}},
		{design: "cached_tier", in: anonFree(Torrent{State: "cached", Progress: 100}, atCap), key: KeyCachedTier,
			chain: "Кэш (cached) [plan+ 5 Мбит/с · потолок] Вы",
			bar:   Bar{Mode: "divider"},
			check: func(t *testing.T, v *View) {
				b := v.Plan.Download.Box
				if nb(b.Sub) != "Файл уже у нас целиком. "+eta(t) {
					t.Errorf("sub %q", nb(b.Sub))
				}
				if b.CTA.Label != "Скачать в 10 раз быстрее" {
					t.Errorf("cached: the cap is the only brake, %q", b.CTA.Label)
				}
			}},
		{design: "checking", in: anonFree(Torrent{State: "caching", Progress: 43, Seeders: 14, SwarmKnown: true, Checking: true, Pieces: true}, zero), key: KeyChecking,
			badge: "cyan dots Проверяем активность…",
			bar:   Bar{Mode: "pieces", Tone: "flow"}},
		{design: "paused", in: anonFree(paused, zero), key: KeyPaused,
			badge: "warn pause Кэширование на паузе 43% (14 сидов)",
			bar:   Bar{Mode: "pieces", Tone: "flow"},
			hint:  "Сейчас это никто не скачивает — кэширование продолжится, как только кто-нибудь начнёт смотреть."},
		{design: "noseed", in: anonFree(Torrent{State: "caching", Progress: 43, SwarmKnown: true, NoSeeders: true, Pieces: true}, zero), key: KeyNoSeed,
			badge: "err noseed Нет сидов · 43%",
			bar:   Bar{Mode: "pieces", Tone: "flow"},
			hint:  "Сейчас этот торрент никто не раздаёт; кэширование продолжится, как только появится сид."},
		{design: "missing_idle", in: anonFree(holes("caching", 43), zero), key: KeyMissingIdle,
			badge: "warn warn Нет нужных кусков · 43% (12 пиров, 0 сидов)",
			bar:   Bar{Mode: "pieces", Tone: "flow"},
			hint:  "12 пиров на связи, но у всех неполные копии: в рое есть 73% раздачи, остальное появится только с полным сидом.",
			vault: true},
		{design: "idle_torrent", in: anonFree(Torrent{State: "idle", Seeders: 14, SwarmKnown: true}, zero), key: KeyIdleTorrent,
			badge: "muted idle Ожидает (14 сидов)",
			bar:   Bar{Mode: "divider"}},
		{design: "cached", in: anonFree(Torrent{State: "cached", Progress: 100}, zero), key: KeyCached,
			badge: "ok check В кэше",
			bar:   Bar{Mode: "divider"}},
		{design: "status_unknown", in: anonFree(Torrent{State: "unknown"}, zero), key: KeyUnknown,
			badge: "muted unknown Статус недоступен",
			bar:   Bar{Mode: "divider"},
			hint:  "Не удалось получить статус торрента — о самой раздаче это ничего не говорит."},
		{design: "caching_idle", in: anonFree(caching(43, 14, 0), zero), key: KeyCachingIdle,
			badge: "cyan down+ Кэширование 43% (14 сидов)",
			bar:   Bar{Mode: "pieces", Tone: "flow"}},
		{design: "vaulting", in: anonFree(Torrent{State: "vaulting", Progress: 64, Seeders: 9, SwarmKnown: true, RateBps: mbpsBytes(22), Pieces: true}, flowing(12)), key: KeyVaulting,
			chain: "Рой 9 сидов [vault+ 22 Мбит/с] Vault 64% (vault) [flow+ 12 Мбит/с] Вы",
			bar:   Bar{Mode: "pieces", Tone: "vault"},
			hint:  "Раздача сохраняется в Vault — потом будет доступна и без сидов."},
		{design: "vaulting_only", in: anonFree(Torrent{State: "vaulting", Progress: 64, Seeders: 9, SwarmKnown: true, RateBps: mbpsBytes(22), Pieces: true}, zero), key: KeyVaultingOnly,
			chain: "Рой 9 сидов [vault+ 22 Мбит/с] Vault 64% (vault)",
			bar:   Bar{Mode: "pieces", Tone: "vault"},
			hint:  "Раздача сохраняется в Vault — потом будет доступна и без сидов."},
		{design: "vaulted", in: anonFree(Torrent{State: "vaulted"}, flowing(24)), key: KeyVaulted,
			chain: "Vault сохранено (vault) [flow+ 24 Мбит/с] Вы",
			bar:   Bar{Mode: "divider"}},
		{design: "vaulted_tier", in: anonFree(Torrent{State: "vaulted"}, atCap), key: KeyVaultedTier,
			chain: "Vault сохранено (vault) [plan+ 5 Мбит/с · потолок] Вы",
			bar:   Bar{Mode: "divider"},
			check: func(t *testing.T, v *View) {
				b := v.Plan.Download.Box
				if nb(b.Sub) != "Отдаём из Vault без сидов. "+eta(t) || b.CTA.Label != "Скачать в 10 раз быстрее" {
					t.Errorf("box %q / %q", nb(b.Sub), b.CTA.Label)
				}
			}},
		{design: "vaulted_idle", in: anonFree(Torrent{State: "vaulted"}, zero), key: KeyVaultedIdle,
			badge: "vault vault В Vault",
			bar:   Bar{Mode: "divider"}},
		// The bar is what the cache holds, in Vault's purple (as approved).
		{design: "vault_waiting", in: anonFree(Torrent{State: "vault_waiting", SwarmKnown: true, Pieces: true}, zero), key: KeyVaultWait,
			badge: "vault clock+ Ждём сидов",
			bar:   Bar{Mode: "pieces", Tone: "vault"},
			hint:  "Vault пока нечего скачивать — сидов онлайн нет. Он продолжает проверять и вернёт очки, если сиды не появятся."},
		{design: "vault_missing", in: anonFree(vaultMissing, zero), key: KeyVaultMissing,
			badge: "vault clock+ Ждём недостающие куски · 58%",
			bar:   Bar{Mode: "pieces", Tone: "vault"},
			hint:  "Vault ждёт недостающие куски: у 12 пиров на связи есть только 73% раздачи. Он продолжает проверять и вернёт очки, если куски не появятся."},
		{design: "vault_failed", in: anonFree(Torrent{State: "vault_failed", Progress: 37, Seeders: 3, SwarmKnown: true, Pieces: true}, zero), key: KeyVaultFailed,
			badge: "warn warn Перенос не удался, повторяем 37% (3 сида)",
			bar:   Bar{Mode: "pieces", Tone: "vault"},
			hint:  "Последняя попытка сохранить раздачу в Vault не прошла. Vault продолжает пробовать; ничего не потеряно."},
		{design: "vaulting_idle", in: anonFree(Torrent{State: "vaulting", Progress: 64, Seeders: 9, SwarmKnown: true, Pieces: true}, zero), key: KeyVaultingIdle,
			badge: "vault up+ Сохраняется 64% (9 сидов)",
			bar:   Bar{Mode: "pieces", Tone: "vault"},
			hint:  "Раздача сохраняется в Vault — потом будет доступна и без сидов."},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		t.Run(c.design, func(t *testing.T) {
			seen[c.design] = true
			v := Build(c.in)
			if v.Key != c.key {
				t.Fatalf("key %q, want %q", v.Key, c.key)
			}
			if (c.chain != "") == (c.badge != "") {
				t.Fatal("a row is either the chain or the badge")
			}
			switch {
			case c.chain != "":
				if v.Mode != ModeChain || !v.Sticky {
					t.Errorf("mode %q sticky %v, want the chain", v.Mode, v.Sticky)
				}
				if got := chain(v); got != c.chain {
					t.Errorf("chain\n got %s\nwant %s", got, c.chain)
				}
			default:
				if v.Mode != ModeBadge || v.Sticky {
					t.Errorf("mode %q sticky %v, want the badge", v.Mode, v.Sticky)
				}
				if got := badge(v); got != c.badge {
					t.Errorf("badge\n got %s\nwant %s", got, c.badge)
				}
			}
			if v.Bar != c.bar {
				t.Errorf("bar %+v, want %+v", v.Bar, c.bar)
			}
			if nb(v.Hint) != c.hint {
				t.Errorf("hint %q, want %q", nb(v.Hint), c.hint)
			}
			if v.Vault != c.vault {
				t.Errorf("vault %v, want %v", v.Vault, c.vault)
			}
			if isTierKey(c.key) != (v.Plan != nil) {
				t.Errorf("plan %+v for key %s", v.Plan, c.key)
			}
			if v.Plan != nil && v.Hint != "" {
				t.Error("a hint next to the plan box")
			}
			// The source node is on every chain, and the chain row's height
			// with it: the badge takes the same row (partials/resource/
			// status.html), so switching between them never moves the card.
			if !v.Nodes[1].Show {
				t.Error("the source node is off the chain")
			}
			if c.check != nil {
				c.check(t, v)
			}
		})
	}
	// The design's own list: a row added there without one here fails.
	for _, k := range designKeys(t, "../../docs/transfer_status.html") {
		if !seen[k] {
			t.Errorf("docs/transfer_status.html has state %q, not covered here", k)
		}
	}
}

// Every state Build can say has a row in the design, and the page's own
// refinements of the tier state too: a state the code emits without a
// picture has nothing to be checked against. (The other direction is the
// table above.)
func TestDesignHasEveryState(t *testing.T) {
	have := map[string]bool{}
	for _, k := range designKeys(t, "../../docs/transfer_status.html") {
		have[k] = true
	}
	for _, k := range []string{KeyActive, KeyChecking, KeySwarm, KeyStalled, KeyMissing, KeyCachingOnly, KeyCachingIdle,
		KeyCachedFlow, KeyCachedTier, KeyCached, KeyPaused, KeyNoSeed, KeyMissingIdle, KeyIdleTorrent, KeyUnknown,
		KeyVaulting, KeyVaultingOnly, KeyVaultingIdle, KeyVaulted, KeyVaultedTier, KeyVaultedIdle, KeyVaultWait,
		KeyVaultMissing, KeyVaultFailed,
		// KeyTier is never shown as it is: the page refines it.
		"tier_dl", "stream_ok", "stream_stall", "stream_over"} {
		if !have[k] {
			t.Errorf("state %q has no row in docs/transfer_status.html", k)
		}
	}
}

// The design the owner approved is kept as it was, frozen (approved
// 2026-09-24, revised 2026-09-25: the chain only while something moves and
// only its participants, the old badge otherwise): the living page may add
// rows, never lose one of the approved.
func TestDesignKeepsTheApprovedStates(t *testing.T) {
	approved := designKeys(t, "../../docs/transfer_status.approved.html")
	living := map[string]bool{}
	for _, k := range designKeys(t, "../../docs/transfer_status.html") {
		living[k] = true
	}
	if len(approved) != 25 {
		t.Errorf("%d states in the approved design, it has 25", len(approved))
	}
	for _, k := range approved {
		if !living[k] {
			t.Errorf("approved state %q is gone from docs/transfer_status.html", k)
		}
	}
}

// The chain draws only who takes part: the swarm while it sends (or while
// the viewer waits for it), the viewer while bytes go to them (or while
// they wait). The source is always on it.
func TestBuild_OnlyParticipants(t *testing.T) {
	cases := []struct {
		name  string
		tr    Torrent
		v     Viewer
		key   string
		chain string
	}{
		{"the swarm moves, nothing goes to the viewer: no «you»", caching(43, 14, 38), zero, KeyCachingOnly,
			"Рой 14 сидов [flow+ 38 Мбит/с] Кэш 43%"},
		{"the swarm moves, the viewer unknown: no «you»", caching(43, 14, 38), Viewer{}, KeyCachingOnly,
			"Рой 14 сидов [flow+ 38 Мбит/с] Кэш 43%"},
		{"from the cache: no swarm", Torrent{State: "cached", Seeders: 14, SwarmKnown: true}, flowing(24), KeyCachedFlow,
			"Кэш (cached) [flow+ 24 Мбит/с] Вы"},
		{"a paused swarm, the viewer reads what is cached: no swarm", Torrent{State: "caching", Progress: 43, Paused: true, Seeders: 14, SwarmKnown: true}, flowing(4), KeyActive,
			"Кэш 43% [flow+ 4 Мбит/с] Вы"},
		{"from Vault at the cap: no swarm", Torrent{State: "vaulted", Seeders: 3, SwarmKnown: true}, atCap, KeyVaultedTier,
			"Vault сохранено (vault) [plan+ 5 Мбит/с · потолок] Вы"},
		{"the viewer waits on a still swarm: the swarm is on the chain", caching(43, 14, 0), stalled, KeyStalled,
			"Рой 14 сидов [flow — Мбит/с] Кэш 43% [swarm 0 Мбит/с · ждём данные] Вы"},
		{"a stall on cached content: nothing to blame on a swarm", Torrent{State: "cached"}, stalled, KeyCachedFlow,
			"Кэш (cached) [off 0 Мбит/с · ждём данные] Вы"},
		{"the swarm into Vault, the viewer idle: the Vault node", Torrent{State: "vaulting", Progress: 64, Seeders: 9, SwarmKnown: true, RateBps: mbpsBytes(22)}, zero, KeyVaultingOnly,
			"Рой 9 сидов [vault+ 22 Мбит/с] Vault 64% (vault)"},
		{"Vault waits, the cache serves the viewer: no Vault as the source", Torrent{State: "vault_waiting", SwarmKnown: true, CacheProgress: 43}, flowing(3), KeyVaultWait,
			"Кэш 43% [flow+ 3 Мбит/с] Вы"},
		{"Vault retries, the cache serves the viewer", Torrent{State: "vault_failed", Progress: 30, Seeders: 2, SwarmKnown: true, CacheProgress: 61}, flowing(3), KeyVaultFailed,
			"Кэш 61% [flow+ 3 Мбит/с] Вы"},
		{"nothing cached, the viewer waits on pieces nobody has: the swarm is why", func() Torrent { t := holes("idle", 0); t.ReaderMissing = 2; return t }(), stalled, KeyMissing,
			"Рой 12 пиров · 0 сидов [swarm 0 Мбит/с · нет нужных кусков] Кэш 0% [swarm 0 Мбит/с · ждём данные] Вы"},
		{"nothing cached, the viewer waits on something else: no swarm", Torrent{State: "idle", Seeders: 14, SwarmKnown: true}, stalled, KeyIdleTorrent,
			"Кэш [swarm 0 Мбит/с · ждём данные] Вы"},
		// Their request has just opened: they are there, and no word next
		// to the cache says anyone is "waiting" for a viewer.
		{"nothing cached, the viewer's request just opened", Torrent{State: "idle", Seeders: 14, SwarmKnown: true}, Viewer{Known: true, Present: true, CapMbps: 5}, KeyIdleTorrent,
			"Кэш [off — Мбит/с] Вы"},
		{"nothing cached, no stats, the viewer's request open", Torrent{State: "idle"}, Viewer{Known: true, Present: true, CapMbps: 5}, KeyIdleTorrent,
			"Кэш [off — Мбит/с] Вы"},
	}
	for _, c := range cases {
		v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: c.tr, Viewer: c.v, ClaimCapMbps: 5, Offers: liveOffers()})
		if v.Key != c.key || v.Mode != ModeChain || chain(v) != c.chain {
			t.Errorf("%s:\n got %s %s %s\nwant %s chain %s", c.name, v.Key, v.Mode, chain(v), c.key, c.chain)
		}
		// The details say what the chain says: no row for who is not on it.
		if v.Details.Rows[0].Show != v.Nodes[0].Show || v.Details.Rows[2].Show != v.Nodes[2].Show {
			t.Errorf("%s: details rows %+v", c.name, v.Details.Rows)
		}
	}
}

// No data from the proxy: the viewer's segment and node are not drawn at
// all -- and a known zero is not either: nothing goes to them, so they are
// not on the chain (the dimmed «you» and "—" of 2026-09-24 are gone).
func TestBuild_ViewerOnlyWhileBytesGoOrTheyWait(t *testing.T) {
	for _, v := range []Viewer{{}, zero} {
		for _, tr := range []Torrent{caching(43, 14, 38), {State: "cached"}, {State: "vaulted"}, {State: "unknown"}} {
			b := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: v, Offers: liveOffers(), ClaimCapMbps: 5})
			if b.Nodes[2].Show || b.Segs[1].Show || b.Details.Rows[2].Show {
				t.Errorf("%s %+v: viewer drawn: %s", tr.State, v, chain(b))
			}
			if b.Plan != nil {
				t.Errorf("%s: a plan without bytes at the cap", tr.State)
			}
		}
	}
	for _, v := range []Viewer{flowing(3), atCap, stalled} {
		if b := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: Torrent{State: "cached"}, Viewer: v, ClaimCapMbps: 5}); !b.Nodes[2].Show || !b.Segs[1].Show || b.Mode != ModeChain {
			t.Errorf("%+v: the viewer is off the chain: %s", v, chain(b))
		}
	}
}

// Nothing moves: the badge, whatever the state -- and the chain the moment
// anything moves again.
func TestBuild_ModeFollowsMovement(t *testing.T) {
	cases := []struct {
		name string
		tr   Torrent
		v    Viewer
		mode string
	}{
		{"cached, nobody downloads", Torrent{State: "cached"}, zero, ModeBadge},
		{"cached, no reading", Torrent{State: "cached"}, Viewer{}, ModeBadge},
		{"cached, bytes to the viewer", Torrent{State: "cached"}, flowing(0.1), ModeChain},
		{"idle torrent", Torrent{State: "idle", Seeders: 2, SwarmKnown: true}, zero, ModeBadge},
		{"the page before its first status", Torrent{State: "idle", Pending: true}, Viewer{}, ModeBadge},
		{"a swarm below any label (0.04 Mbps)", caching(43, 14, 0.04), zero, ModeBadge},
		{"a swarm at 0.1 Mbps", caching(43, 14, 0.1), zero, ModeChain},
		{"vault failed, the viewer receives", Torrent{State: "vault_failed"}, flowing(2), ModeChain},
		{"no seeders, the viewer waits", Torrent{State: "caching", NoSeeders: true, SwarmKnown: true}, stalled, ModeChain},
	}
	for _, c := range cases {
		v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: c.tr, Viewer: c.v, ClaimCapMbps: 5})
		if v.Mode != c.mode || v.Sticky != (c.mode == ModeChain) {
			t.Errorf("%s: mode %q sticky %v, want %q (%s)", c.name, v.Mode, v.Sticky, c.mode, v.Key)
		}
	}
}

// Pieces arrive in bursts: a swarm that just stopped stays on the chain
// through the hold (Input.HeldBps, from Hold) drawn as it last moved -- its
// last speed, its sweep, the key that went with it -- never with a badge
// key's pause, dash or hint (the look the owner rejected for «you»), and the
// badge takes over only once the hold is over.
func TestBuild_HeldSwarmIsDrawnAsItLastMoved(t *testing.T) {
	paused := Torrent{State: "caching", Progress: 43, Seeders: 14, SwarmKnown: true, Paused: true, Pieces: true}
	still := caching(43, 14, 0)
	fewStill := caching(8, 2, 0)
	cases := []struct {
		name  string
		tr    Torrent
		v     Viewer
		held  float64 // Mbps
		key   string
		chain string
		hint  string
	}{
		{"paused within the hold", paused, zero, 38, KeyCachingOnly, "Рой 14 сидов [flow+ 38 Мбит/с] Кэш 43%", ""},
		{"a gap between pieces", still, zero, 38, KeyCachingOnly, "Рой 14 сидов [flow+ 38 Мбит/с] Кэш 43%", ""},
		{"a gap, bytes to the viewer", still, flowing(2), 38, KeyActive, "Рой 14 сидов [flow+ 38 Мбит/с] Кэш 43% [flow+ 2 Мбит/с] Вы", ""},
		{"a gap in a few slow seeders", fewStill, flowing(1.2), 1.2, KeySwarm, "Рой 2 сида [swarm+ 1,2 Мбит/с · мало сидов] Кэш 8% [flow+ 1,2 Мбит/с] Вы",
			"Скорость ограничивают раздающие — их сейчас 2."},
		{"pieces nobody has, within the hold", holes("caching", 43), zero, 2, KeyCachingOnly, "Рой 12 пиров [flow+ 2 Мбит/с] Кэш 43%", ""},
		{"no seeders called within the hold", Torrent{State: "caching", Progress: 43, SwarmKnown: true, NoSeeders: true, Pieces: true}, zero, 5, KeyCachingOnly,
			"Рой 0 сидов [flow+ 5 Мбит/с] Кэш 43%", ""},
		{"into Vault, within the hold", Torrent{State: "vaulting", Progress: 64, Seeders: 9, SwarmKnown: true}, zero, 22, KeyVaultingOnly,
			"Рой 9 сидов [vault+ 22 Мбит/с] Vault 64% (vault)", "Раздача сохраняется в Vault — потом будет доступна и без сидов."},
	}
	for _, c := range cases {
		v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: c.tr, Viewer: c.v, ClaimCapMbps: 5, HeldBps: mbpsBytes(c.held)})
		if v.Key != c.key || v.Mode != ModeChain || !v.Sticky || chain(v) != c.chain || nb(v.Hint) != c.hint {
			t.Errorf("%s:\n got %s %s %s %q\nwant %s chain %s %q", c.name, v.Key, v.Mode, chain(v), nb(v.Hint), c.key, c.chain, c.hint)
		}
		if s := chain(v); strings.Contains(s, "—") || strings.Contains(s, "пауза") || strings.Contains(s, "pause") {
			t.Errorf("%s: a still look on the held chain: %s", c.name, s)
		}
	}
	// The hold over: the badge, and the state's own story.
	if v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: paused, Viewer: zero}); v.Key != KeyPaused || v.Mode != ModeBadge || v.Hint == "" {
		t.Errorf("paused, the hold over: %s %s %q", v.Key, v.Mode, v.Hint)
	}
	if v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: holes("caching", 43), Viewer: zero}); v.Key != KeyMissingIdle || v.Mode != ModeBadge || !v.Vault {
		t.Errorf("missing, the hold over: %s %s", v.Key, v.Mode)
	}
	// Moving now: its own rate, whatever the hold remembers.
	if v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: caching(43, 14, 12), Viewer: zero, HeldBps: mbpsBytes(38)}); chain(v) != "Рой 14 сидов [flow+ 12 Мбит/с] Кэш 43%" {
		t.Errorf("moving, a stale hold: %s", chain(v))
	}
	// Held or not, the swarm is on the chain only where it sends anything:
	// a torrent that is cached now has none.
	if v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: Torrent{State: "cached"}, Viewer: zero, HeldBps: mbpsBytes(38)}); v.Mode != ModeBadge || v.Nodes[0].Show {
		t.Errorf("cached, held: %s %s", v.Mode, chain(v))
	}
}

// The availability counts only once the seeder knows it, only with peers
// around and no seeder among them -- anything else is another story.
func TestBuild_MissingNeedsAKnownSwarmWithoutASeeder(t *testing.T) {
	cases := []struct {
		name string
		tr   func(Torrent) Torrent
		v    Viewer
		key  string
	}{
		{"the case itself", func(t Torrent) Torrent { return t }, zero, KeyMissingIdle},
		{"the case, the viewer waiting on it", func(t Torrent) Torrent { t.ReaderMissing = 2; return t }, stalled, KeyMissing},
		{"not known yet: a lower bound", func(t Torrent) Torrent { t.AvailabilityKnown = false; return t }, zero, KeyCachingIdle},
		{"not known, the viewer waiting", func(t Torrent) Torrent { t.AvailabilityKnown = false; t.ReaderMissing = 2; return t }, stalled, KeyStalled},
		{"a seeder is there", func(t Torrent) Torrent { t.Seeders = 1; return t }, zero, KeyCachingIdle},
		{"nobody is there", func(t Torrent) Torrent { t.Peers = 0; return t }, zero, KeyCachingIdle},
		{"only wanted pieces missing", func(t Torrent) Torrent { t.Missing = false; return t }, zero, KeyMissingIdle},
		{"only unwanted pieces missing", func(t Torrent) Torrent { t.WantedMissing = 0; return t }, zero, KeyMissingIdle},
		{"nothing missing", func(t Torrent) Torrent { t.Missing, t.WantedMissing = false, 0; return t }, zero, KeyCachingIdle},
		{"the viewer waits, but not on a missing piece", func(t Torrent) Torrent { return t }, stalled, KeyStalled},
		{"the swarm moves: something comes", func(t Torrent) Torrent { t.RateBps = mbpsBytes(2); return t }, zero, KeyCachingOnly},
		{"paused, and pieces nobody has: the missing ones are the story", func(t Torrent) Torrent { t.Paused = true; return t }, zero, KeyMissingIdle},
	}
	for _, c := range cases {
		v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: c.tr(holes("caching", 43)), Viewer: c.v, ClaimCapMbps: 5})
		if v.Key != c.key {
			t.Errorf("%s: %s, want %s", c.name, v.Key, c.key)
		}
		if v.Vault != (c.key == KeyMissing || c.key == KeyMissingIdle) {
			t.Errorf("%s: vault %v", c.name, v.Vault)
		}
	}
	// Vault's transfer: the same condition, its own words.
	if k := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: func() Torrent { t := holes("vaulting", 58); t.AvailabilityKnown = false; return t }(), Viewer: zero}).Key; k != KeyVaultingIdle {
		t.Errorf("vault, not known: %s", k)
	}
}

// Before a single piece is cached the swarm's holes are the story too: the
// head of a file nobody connected has keeps a torrent at 0% for good, and a
// viewer waiting on it was told "Waiting" with no reason and no way out.
func TestBuild_MissingBeforeAnythingIsCached(t *testing.T) {
	idle := holes("idle", 0)
	waiting := idle
	waiting.ReaderMissing = 3
	v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: waiting, Viewer: stalled, ClaimCapMbps: 5})
	if v.Key != KeyMissing || v.Mode != ModeChain || !v.Vault || !v.Nodes[0].Show || v.Bar != (Bar{Mode: "pieces", Tone: "flow"}) {
		t.Errorf("waiting: %s %s %s vault %v bar %+v", v.Key, v.Mode, chain(v), v.Vault, v.Bar)
	}
	v = Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: idle, Viewer: zero, ClaimCapMbps: 5})
	if v.Key != KeyMissingIdle || v.Mode != ModeBadge || !v.Vault || badge(v) != "warn warn Нет нужных кусков · 0% (12 пиров, 0 сидов)" || v.Bar != (Bar{Mode: "pieces", Tone: "flow"}) {
		t.Errorf("nobody waiting: %s %s %q vault %v bar %+v", v.Key, v.Mode, badge(v), v.Vault, v.Bar)
	}
	// Not known, or a seeder there: the old "Waiting (N)".
	for _, tr := range []Torrent{
		func() Torrent { t := idle; t.AvailabilityKnown = false; return t }(),
		func() Torrent { t := idle; t.Seeders = 1; return t }(),
	} {
		if v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: zero}); v.Key != KeyIdleTorrent || v.Vault {
			t.Errorf("%+v: %s", tr, v.Key)
		}
	}
	// Without holes to hatch, an idle torrent draws no bar.
	if v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: Torrent{State: "idle", Seeders: 14, SwarmKnown: true, Pieces: true}, Viewer: zero}); v.Bar != (Bar{Mode: "divider"}) {
		t.Errorf("idle, no holes: bar %+v", v.Bar)
	}
}

// The first seconds of a status stream: nothing of the swarm arrived yet,
// and a swarm that is downloading has not had the time to show it. The
// pieces nobody has are not blamed then -- that read "needed pieces
// missing" and offered Vault for the second or two before the first piece
// of every such page load.
func TestBuild_MissingNotBlamedWhileSettling(t *testing.T) {
	for _, c := range []struct {
		tr   Torrent
		want string
	}{
		{holes("caching", 43), KeyCachingIdle},
		{holes("vaulting", 58), KeyVaultingIdle},
		{holes("idle", 0), KeyIdleTorrent},
	} {
		c.tr.Settling = true
		v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: c.tr, Viewer: zero})
		if v.Key != c.want || v.Vault {
			t.Errorf("%s settling: %s vault %v, want %s", c.tr.State, v.Key, v.Vault, c.want)
		}
		c.tr.Settling = false
		if v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: c.tr, Viewer: zero}); !isMissing(v.Key) {
			t.Errorf("%s settled: %s", c.tr.State, v.Key)
		}
	}
	// A viewer who already waits on a missing piece is told at once.
	tr := holes("caching", 43)
	tr.Settling, tr.ReaderMissing = true, 2
	if v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: stalled}); v.Key != KeyMissing {
		t.Errorf("settling, the viewer waiting on it: %s", v.Key)
	}
}

// Russian's "one" is 1, 21, 31, 101…: the hint for one peer must read right
// for 21 of them -- no "у него", no "его копия".
func TestBuild_MissingHintPluralsReadForTwentyOne(t *testing.T) {
	for _, n := range []int{1, 21, 31, 101, 2, 5, 11} {
		for _, c := range []struct {
			tr Torrent
			v  Viewer
		}{
			{func() Torrent { t := holes("caching", 43); t.Peers, t.ReaderMissing = n, 2; return t }(), stalled},
			{func() Torrent { t := holes("caching", 43); t.Peers = n; return t }(), zero},
		} {
			h := nb(Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: c.tr, Viewer: c.v}).Hint)
			// Every copy is incomplete: "nobody has", "everyone has only".
			if !strings.HasPrefix(h, fmt.Sprintf("%d пир", n)) || strings.Contains(h, "у него") || strings.Contains(h, "его копия") ||
				!(strings.Contains(h, "ни у кого") || strings.Contains(h, "у всех")) {
				t.Errorf("%d peers: %q", n, h)
			}
		}
	}
}

// "В рое есть 73% раздачи": the seeder's float32 share, never rounded up.
func TestBuild_AvailabilityPercent(t *testing.T) {
	for _, c := range []struct {
		a    float64
		want string
	}{{float64(float32(0.73)), "73%"}, {float64(float32(0.29)), "29%"}, {0.999, "99%"}, {0.5, "50%"}} {
		tr := holes("caching", 43)
		tr.Availability = c.a
		if h := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: zero}).Hint; !strings.Contains(h, "есть "+c.want+" раздачи") {
			t.Errorf("%v: %q", c.a, h)
		}
	}
}

// The badge is the one the page had before the chain: its colour, icon and
// words, the swarm in brackets where it had them.
func TestBuild_BadgeSwarm(t *testing.T) {
	for _, c := range []struct {
		tr   Torrent
		want string
	}{
		{Torrent{State: "idle", Seeders: 14, Leechers: 9, SwarmKnown: true}, "(14 сидов · 9 личей)"},
		{Torrent{State: "idle", Seeders: 0, Leechers: 3, SwarmKnown: true}, "(0 сидов · 3 лича)"},
		{Torrent{State: "idle", Seeders: 14, SwarmKnown: true}, "(14 сидов)"},
		{Torrent{State: "idle", Peers: 5, SwarmKnown: true}, "(5 пиров)"},
		{Torrent{State: "idle"}, ""},
	} {
		if got := nb(Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: c.tr, Viewer: zero}).Badge.Extra); got != c.want {
			t.Errorf("%+v: %q, want %q", c.tr, got, c.want)
		}
	}
	// Answers carry no swarm: cached and vaulted play whoever is around.
	for _, st := range []string{"cached", "vaulted", "unknown", "vault_waiting"} {
		if e := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: Torrent{State: st, Seeders: 4, SwarmKnown: true}, Viewer: zero}).Badge.Extra; e != "" {
			t.Errorf("%s: %q", st, e)
		}
	}
	// A failed transfer with nothing stored says no percent.
	if l := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: Torrent{State: "vault_failed"}, Viewer: zero}).Badge.Label; l != "Перенос не удался, повторяем" {
		t.Errorf("vault failed at 0: %q", l)
	}
}

// Nothing is sold where a plan would not help, where nothing faster exists,
// or where nothing is on sale at all.
func TestBuild_NoCTA(t *testing.T) {
	base := func(tr Torrent, v Viewer) Input {
		return Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: v, ClaimCapMbps: 5, Offers: liveOffers(), SizeBytes: gb12}
	}
	// States that carry no plan at all, even with the viewer at the cap.
	for name, in := range map[string]Input{
		"pause":       base(Torrent{State: "caching", Progress: 43, Seeders: 14, SwarmKnown: true, Paused: true}, zero),
		"no seeders":  base(Torrent{State: "caching", Progress: 43, SwarmKnown: true, NoSeeders: true}, atCap),
		"few seeders": base(caching(8, 2, 1.2), flowing(1.2)),
		// Reading what is cached at the cap while the rest waits for two
		// slow seeders: "10× faster, about 3 min" would be false.
		"few seeders, the viewer at the cap": base(caching(40, 2, 1.2), atCap),
		"one seeder, the viewer at the cap":  base(caching(40, 1, 0.3), atCap),
		"stall":                              base(caching(43, 14, 38), stalled),
		"missing pieces":                     base(func() Torrent { t := holes("caching", 43); t.ReaderMissing = 1; return t }(), stalled),
		"vault failed":                       base(Torrent{State: "vault_failed", Seeders: 3, SwarmKnown: true}, atCap),
		"checking":                           base(Torrent{State: "caching", Checking: true}, atCap),
	} {
		if v := Build(in); v.Plan != nil {
			t.Errorf("%s (%s): plan %+v", name, v.Key, v.Plan)
		}
	}
	// Plan-limited, but no box: the fact alone.
	gold := base(caching(61, 31, 380), Viewer{Known: true, Present: true, Mbps: 100, Limited: true, PlanBox: true, CapMbps: 100})
	gold.Tier, gold.SignedIn = "gold", true
	noCatalog := base(caching(61, 31, 38), atCap)
	noCatalog.Offers = nil
	slowPromo := base(caching(61, 31, 38), Viewer{Known: true, Present: true, Mbps: 50, Limited: true, PlanBox: true, CapMbps: 50})
	emptyCatalog := base(caching(61, 31, 38), atCap)
	emptyCatalog.Offers = fakeOffers{}
	for name, in := range map[string]Input{
		"top tier: nothing faster on sale": gold,
		"no offer catalog":                 noCatalog,
		"catalog without a promo plan":     emptyCatalog,
		"promo no faster than the cap":     slowPromo,
	} {
		v := Build(in)
		if v.Plan == nil {
			t.Fatalf("%s: no plan at all", name)
		}
		if v.Plan.Download.Box != nil || v.Plan.Stream.Box != nil {
			t.Errorf("%s: a box %+v / %+v", name, v.Plan.Download.Box, v.Plan.Stream.Box)
		}
		if v.Plan.Download.Hint == "" || v.Plan.Stream.Hint == "" || v.Plan.Fact == "" {
			t.Errorf("%s: the fact must still be said: %+v", name, v.Plan)
		}
	}
	if h := nb(Build(gold).Plan.Download.Hint); h != "Скорость скачивания ограничена: 100 Мбит/с" {
		t.Errorf("gold download hint %q", h)
	}
}

// The cap in two steps (owner, 2026-09-25). The fact first: the pink link,
// the cap tag, the key that says the cap is the cause and the fact line for
// a player that plays -- and nothing sold, not even the box's line, until
// the cap has held for PlanBoxAfter (Viewer.PlanBox). Then the box, which
// outlives a dip under the cap for PlanBoxHold: the link says the speed as
// it is, the box stays put. The honesty rules take a held box down at once.
func TestBuild_FactBeforeTheBox(t *testing.T) {
	build := func(tr Torrent, v Viewer) *View {
		return Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: v, ClaimCapMbps: 5, Offers: liveOffers(), SizeBytes: gb12})
	}
	cases := []struct {
		name  string
		tr    Torrent
		key   string
		chain string
	}{
		{"caching", caching(61, 31, 38), KeyTier, "Рой 31 сид [flow+ 38 Мбит/с] Кэш 61% [plan+ 5 Мбит/с · потолок] Вы"},
		{"cached", Torrent{State: "cached", Progress: 100}, KeyCachedTier, "Кэш (cached) [plan+ 5 Мбит/с · потолок] Вы"},
		{"vaulted", Torrent{State: "vaulted"}, KeyVaultedTier, "Vault сохранено (vault) [plan+ 5 Мбит/с · потолок] Вы"},
	}
	for _, c := range cases {
		v := build(c.tr, capFact)
		if v.Key != c.key || chain(v) != c.chain {
			t.Errorf("%s, the fact: %s %s, want %s %s", c.name, v.Key, chain(v), c.key, c.chain)
		}
		if v.Plan == nil || v.Plan.Fact == "" || v.Plan.Cap == "" {
			t.Fatalf("%s, the fact: no fact to say %+v", c.name, v.Plan)
		}
		if v.Plan.Download != (Variant{}) || v.Plan.Stream != (Variant{}) {
			t.Errorf("%s, the fact: something sold before the box is due: %+v / %+v", c.name, v.Plan.Download, v.Plan.Stream)
		}
		if r := v.Details.Rows[2]; r.Tag == "" || r.Value != "5\u00a0Мбит/с" {
			t.Errorf("%s, the fact: the details' row %+v", c.name, r)
		}
		full := build(c.tr, atCap)
		if full.Key != c.key || chain(full) != c.chain || full.Plan.Download.Box == nil || full.Plan.Stream.Box == nil {
			t.Errorf("%s, the box: %s %s %+v", c.name, full.Key, chain(full), full.Plan)
		}
		if full.Plan.Fact != v.Plan.Fact || full.Plan.Cap != v.Plan.Cap {
			t.Errorf("%s: the fact changed with the box", c.name)
		}
	}
	// Held through a dip: the cause is still the cap, the box as it was, the
	// link and the details the speed as it is.
	v, want := build(caching(61, 31, 38), boxHeld), build(caching(61, 31, 38), atCap)
	if v.Key != KeyTier || chain(v) != "Рой 31 сид [flow+ 38 Мбит/с] Кэш 61% [flow+ 2,5 Мбит/с] Вы" {
		t.Errorf("held: %s %s", v.Key, chain(v))
	}
	if !reflect.DeepEqual(v.Plan, want.Plan) {
		t.Errorf("held: the box is not the one that was up:\n got %+v\nwant %+v", v.Plan, want.Plan)
	}
	if r := v.Details.Rows[2]; r.Tag != "" {
		t.Errorf("held: the cap tag on a link under the cap: %+v", r)
	}
	// Nothing held for a viewer who is not there, and no CTA on a stall,
	// few seeders or a Vault failure, box or not.
	for name, c := range map[string]struct {
		tr Torrent
		v  Viewer
	}{
		"the box held, the viewer waits":  {caching(43, 14, 38), Viewer{Known: true, Present: true, Stalled: true, PlanBox: true, CapMbps: 5}},
		"the box held, a stall on cached": {Torrent{State: "cached", Progress: 100}, Viewer{Known: true, Present: true, Stalled: true, PlanBox: true, CapMbps: 5}},
		"the box held, few slow seeders":  {caching(40, 2, 1.2), boxHeld},
		"the box held, Vault failed":      {Torrent{State: "vault_failed", Seeders: 3, SwarmKnown: true}, boxHeld},
		"the box held, the viewer gone":   {Torrent{State: "cached", Progress: 100}, Viewer{Known: true, PlanBox: true, CapMbps: 5}},
	} {
		if v := build(c.tr, c.v); v.Plan != nil || isTierKey(v.Key) {
			t.Errorf("%s: %s %+v", name, v.Key, v.Plan)
		}
	}
	// The page's own player keeps a held box's viewer on the chain too.
	if p := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: Torrent{State: "cached", Progress: 100}, Viewer: zero,
		LastViewer: Viewer{Known: true, Present: true, PlanBox: true, CapMbps: 5}, ClaimCapMbps: 5, Offers: liveOffers()}).Playing; p == nil || p.Key != KeyCachedTier {
		t.Errorf("the player's view of a held box: %+v", p)
	}
}

// An anonymous or free viewer has no plan to call theirs; a paying one is
// sent to compare plans, never to a trial.
func TestBuild_Wording(t *testing.T) {
	anon := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: caching(61, 31, 38), Viewer: atCap, Offers: liveOffers(), SizeBytes: gb12})
	bronze := Build(Input{Lang: "ru", Loc: loc("ru"), Tier: "bronze", SignedIn: true, Torrent: caching(61, 31, 380),
		Viewer: Viewer{Known: true, Present: true, Mbps: 20, Limited: true, PlanBox: true, CapMbps: 20}, Offers: liveOffers(), SizeBytes: gb12})
	for _, v := range []*View{anon, bronze} {
		b, _ := json.Marshal(v)
		if strings.Contains(strings.ToLower(string(b)), "ваш тариф") {
			t.Errorf("%s: says \"ваш тариф\": %s", v.Tier, b)
		}
	}
	if nb(anon.Plan.Fact) != "Без подписки — до 5 Мбит/с. Видео идёт без остановок." || anon.Auth != "anon" || anon.Tier != "free" {
		t.Errorf("anon: %q %s %s", anon.Plan.Fact, anon.Auth, anon.Tier)
	}
	if nb(bronze.Plan.Fact) != "Ваша подписка — до 20 Мбит/с. Видео идёт без остановок." || bronze.Auth != "user" {
		t.Errorf("bronze: %q %s", bronze.Plan.Fact, bronze.Auth)
	}
	want := CTA{Label: "Улучшить тариф", URL: "/ru/donate", Target: "donate"}
	if bronze.Plan.Download.Box == nil || bronze.Plan.Download.Box.CTA != want || bronze.Plan.Stream.Box.CTA != want {
		t.Errorf("bronze cta: %+v", bronze.Plan)
	}
	if bronze.Plan.Download.Box.Sub != "" {
		t.Errorf("a subscriber is not told \"with a subscription\": %q", bronze.Plan.Download.Box.Sub)
	}
	if row := bronze.Details.Rows[3]; row.Label != "Ваша подписка" || nb(row.Value) != "до 20 Мбит/с" {
		t.Errorf("bronze cap row %+v", row)
	}
	// The swarm, the missing pieces and Vault's wait say only the cause:
	// never what a subscription would or would not do.
	for _, in := range []Input{
		{Torrent: caching(8, 2, 1.2), Viewer: flowing(1.2)},
		{Torrent: func() Torrent { t := holes("caching", 43); t.ReaderMissing = 1; return t }(), Viewer: stalled},
		{Torrent: holes("caching", 43), Viewer: zero},
		{Torrent: holes("vaulting", 58), Viewer: zero},
	} {
		for _, lang := range i18n.SupportedLangs {
			in.Lang, in.Loc, in.ClaimCapMbps, in.Offers = lang, loc(lang), 5, liveOffers()
			v := Build(in)
			if h := strings.ToLower(v.Hint); v.Hint == "" || strings.Contains(h, "подписк") || strings.Contains(h, "subscri") {
				t.Errorf("%s %s: hint %q", lang, v.Key, v.Hint)
			}
		}
	}
}

// Where the button leads when the promo plan has no trial the checkout can
// start: its checkout; without that, /donate. No trial — no trial note.
func TestBuild_CTATargets(t *testing.T) {
	for _, c := range []struct {
		promo  offer.Offer
		url    string
		target string
	}{
		{offer.Offer{Tier: "silver", RateMbps: 50, URL: "https://pay.example/silver"}, "https://pay.example/silver", "checkout"},
		{offer.Offer{Tier: "silver", RateMbps: 50}, "/ru/donate", "donate"},
		{offer.Offer{Tier: "silver", RateMbps: 0, TrialDays: 7}, "/ru/trial?from=status-bar", "trial"},
	} {
		p := c.promo
		v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: caching(61, 31, 38), Viewer: atCap, Offers: fakeOffers{promo: &p}, SizeBytes: gb12})
		cta := v.Plan.Download.Box.CTA
		if cta.URL != c.url || cta.Target != c.target || (cta.Note != "") != (p.TrialDays > 0) {
			t.Errorf("%+v: %+v", c.promo, cta)
		}
	}
	// An unlimited promo plan: no multiple to quote.
	p := offer.Offer{Tier: "silver", RateMbps: 0, TrialDays: 7}
	v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: caching(61, 31, 38), Viewer: atCap, Offers: fakeOffers{promo: &p}})
	if v.Plan.Download.Box.CTA.Label != "Скачать быстрее" || v.Plan.Download.Box.Sub != "С подпиской — без ограничения скорости" {
		t.Errorf("unlimited promo: %+v", v.Plan.Download.Box)
	}
}

// Nothing is stored yet while Vault waits or retries: the source row does
// not say "a stored copy, no seeders needed" next to a hint that says the
// opposite -- and while bytes go to the viewer then, the source they come
// from is Webtor's cache, and so is the row.
func TestBuild_DetailsVaultNotStoredYet(t *testing.T) {
	for _, lang := range []string{"ru", "en"} {
		for _, c := range []struct {
			st   string
			v    Viewer
			want string
		}{
			{"vault_waiting", zero, "resource.status.details.vaultPendingSub"},
			{"vault_failed", zero, "resource.status.details.vaultPendingSub"},
			{"vault_waiting", flowing(2), "resource.status.details.webtorSub"},
			{"vault_failed", flowing(2), "resource.status.details.webtorSub"},
			{"vaulted", flowing(2), "resource.status.details.vaultSub"},
		} {
			v := Build(Input{Lang: lang, Loc: loc(lang), Torrent: Torrent{State: c.st, SwarmKnown: true, Seeders: 3}, Viewer: c.v})
			if got, w := v.Details.Rows[1].Sub, i18n.TranslateWithLocalizer(loc(lang), c.want); got != w || got == "" {
				t.Errorf("%s %s %+v: source sub %q, want %q", lang, c.st, c.v, got, w)
			}
		}
	}
}

// A transfer into Vault under way: part of it is stored, the rest still
// comes from the swarm. The Vault row says what Vault has so far -- never
// "a stored copy, no seeders needed" next to the swarm's row and a hint
// that says "later available without seeders".
func TestBuild_DetailsVaultTransferIsNotAStoredCopy(t *testing.T) {
	for _, lang := range []string{"ru", "en"} {
		for _, c := range []struct {
			tr  Torrent
			v   Viewer
			key string
		}{
			{Torrent{State: "vaulting", Progress: 64, Seeders: 9, SwarmKnown: true, RateBps: mbpsBytes(22)}, flowing(12), KeyVaulting},
			{Torrent{State: "vaulting", Progress: 64, Seeders: 9, SwarmKnown: true, RateBps: mbpsBytes(22)}, zero, KeyVaultingOnly},
			{Torrent{State: "vaulting", Progress: 64, Seeders: 9, SwarmKnown: true}, zero, KeyVaultingIdle},
			{holes("vaulting", 58), zero, KeyVaultMissing},
		} {
			v := Build(Input{Lang: lang, Loc: loc(lang), Torrent: c.tr, Viewer: c.v, ClaimCapMbps: 5})
			if v.Key != c.key {
				t.Fatalf("%s: key %s, want %s", lang, v.Key, c.key)
			}
			row, want := v.Details.Rows[1], i18n.TranslateWithLocalizer(loc(lang), "resource.status.details.vaultSavingSub")
			if row.Label != "Vault" || row.Sub != want || want == "" || row.Sub == i18n.TranslateWithLocalizer(loc(lang), "resource.status.details.vaultSub") {
				t.Errorf("%s %s: source row %+v, want sub %q", lang, c.key, row, want)
			}
			if row.Value != strconv.Itoa(pct(c.tr.Progress))+"%" {
				t.Errorf("%s %s: source value %q", lang, c.key, row.Value)
			}
		}
	}
}

// Vault waiting for seeders, or retrying, has stored nothing it could send:
// bytes that reach the viewer then come from Webtor's cache, and the chain
// names the cache as their source -- never Vault ("Vault ▸ you" read as
// Vault serving them). What Vault is doing stays the hint's.
func TestBuild_VaultWaitWithAViewerDrawsTheCache(t *testing.T) {
	waiting := Torrent{State: "vault_waiting", SwarmKnown: true, CacheProgress: 43.7}
	failed := Torrent{State: "vault_failed", Progress: 30, Seeders: 2, SwarmKnown: true, CacheProgress: 100}
	for _, c := range []struct {
		name  string
		tr    Torrent
		v     Viewer
		key   string
		chain string
	}{
		{"waiting, bytes to the viewer", waiting, flowing(3), KeyVaultWait, "Кэш 43% [flow+ 3 Мбит/с] Вы"},
		{"waiting, the viewer waits", waiting, stalled, KeyVaultWait, "Кэш 43% [swarm 0 Мбит/с · ждём данные] Вы"},
		{"waiting, at the cap", waiting, atCap, KeyVaultWait, "Кэш 43% [plan+ 5 Мбит/с · потолок] Вы"},
		{"retrying, the whole file cached", failed, flowing(3), KeyVaultFailed, "Кэш (cached) [flow+ 3 Мбит/с] Вы"},
		{"no seeder numbers: nothing to quote", Torrent{State: "vault_waiting"}, flowing(3), KeyVaultWait, "Кэш [flow+ 3 Мбит/с] Вы"},
	} {
		v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: c.tr, Viewer: c.v, ClaimCapMbps: 5})
		if v.Key != c.key || v.Mode != ModeChain || chain(v) != c.chain || v.Nodes[1].Kind != "cache" || v.Hint == "" {
			t.Errorf("%s:\n got %s %s %s (%s) %q\nwant %s chain %s", c.name, v.Key, v.Mode, chain(v), v.Nodes[1].Kind, v.Hint, c.key, c.chain)
		}
	}
	// Nothing to the viewer: the badge, and Vault's own node behind it.
	for _, tr := range []Torrent{waiting, failed} {
		if v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: zero}); v.Mode != ModeBadge || v.Nodes[1].Kind != "vault" {
			t.Errorf("%s, nothing moving: %s %s", tr.State, v.Mode, v.Nodes[1].Kind)
		}
	}
}

// A request waiting with nothing arriving is amber -- the swarm -- only
// while the content comes from the swarm; whole on our side it is drawn
// neutral, with no seeder to blame.
func TestBuild_StallBlamesTheSwarmOnlyWhenItIsTheSource(t *testing.T) {
	for st, tone := range map[string]string{
		"caching": "swarm", "vaulting": "swarm", "vault_failed": "swarm",
		"cached": "off", "vaulted": "off",
	} {
		tr := Torrent{State: st, Seeders: 3, SwarmKnown: true}
		v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: stalled})
		if s := v.Segs[1]; s.Tone != tone || nb(s.Note) != "ждём данные" || !s.Show {
			t.Errorf("%s: %s", st, chain(v))
		}
	}
}

// A file whose estimate is under the cap still gets the stream box: the page
// shows it only at a real stall, and a stall while thp's limiter holds the
// requests is the cap's doing whatever the estimate said (The Knick, marked
// "fits" at 4.34, stalled four times in 180 s at 5M with no box on the
// page, 2026-09-26). Its line is the cap alone -- never "up to 5 Mbps, and
// this file needs 3", nor "needs 4.6".
func TestBuild_FileUnderTheCapStillHasTheStreamBox(t *testing.T) {
	for _, need := range []float64{3, 4.34, 5} {
		in := Input{Lang: "ru", Loc: loc("ru"), Torrent: caching(61, 31, 38), Viewer: atCap, ClaimCapMbps: 5, Offers: liveOffers(), SizeBytes: gb12, BitrateMbps: need}
		p := Build(in).Plan
		if p.Stream.Box == nil || nb(p.Stream.Box.Sub) != "Без подписки — до 5 Мбит/с" || p.Stream.Hint != "" || p.Download.Box == nil {
			t.Errorf("needs %v: %+v", need, p)
		}
		if got := nb(StallSub(loc("ru"), "ru", false, 5, need)); got != "Без подписки — до 5 Мбит/с" {
			t.Errorf("needs %v: stall sub %q", need, got)
		}
	}
	if p := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: caching(61, 31, 38), Viewer: atCap, ClaimCapMbps: 5, Offers: liveOffers()}).Plan; nb(p.Cap) != "Без подписки — до 5 Мбит/с" {
		t.Errorf("plan cap %q", p.Cap)
	}
}

// "Fits" is said only with FitsMargin to spare; "over" as the labels say
// them. Between the two -- a file within the margin of the cap -- neither:
// its player gets the fact while it plays and the box at a real stall, like
// a file of unknown bitrate.
func TestFitsCapAndOverCap(t *testing.T) {
	for _, c := range []struct {
		name       string
		cap, need  float64
		fits, over bool
	}{
		{"well under", 5, 3, true, false},
		{"Sintel from nginx-vod (1.11)", 5, 1.105, true, false},
		{"just in the margin (4.1 x 1.2 = 4.92)", 5, 4.1, true, false},
		{"in the margin (4.2 x 1.2 = 5.04)", 5, 4.2, false, false},
		{"The Knick, recorded at 1.16x its estimate", 5, 4.344, false, false},
		{"reads 5, the cap", 5, 4.96, false, false},
		{"reads 5.2", 5, 5.2, false, true},
		{"the owner's file", 5, 8.65, false, true},
		{"paid, in the margin", 20, 17, false, false},
		{"paid, over", 20, 30, false, true},
		{"paid, fits", 20, 16, true, false},
		{"unknown bitrate", 5, 0, false, false},
		{"no cap", 0, 3, false, false},
		{"no cap, heavy", 0, 8, false, false},
	} {
		if got := FitsCap(c.cap, c.need); got != c.fits {
			t.Errorf("%s: FitsCap(%v, %v) = %v", c.name, c.cap, c.need, got)
		}
		if got := OverCap(c.cap, c.need); got != c.over {
			t.Errorf("%s: OverCap(%v, %v) = %v", c.name, c.cap, c.need, got)
		}
	}
	// Never both, anywhere on the scale.
	for _, capMbps := range []float64{0.5, 1, 2.5, 5, 10, 20, 50, 100} {
		for need := 0.0; need <= 2*capMbps; need += capMbps / 400 {
			if FitsCap(capMbps, need) && OverCap(capMbps, need) {
				t.Fatalf("%v at a %v cap: fits and over", need, capMbps)
			}
		}
	}
}

// The ETA in every language, for every unit a wait can end in: one full
// stop where an abbreviation ends the sentence ("34 Min." not "34 Min..").
func TestBuild_ETAEndsOnce(t *testing.T) {
	const gib = int64(1) << 30
	for _, lang := range i18n.SupportedLangs {
		for _, size := range []int64{5 << 20, 123 << 20, gb12, 4 * gib, 43 * gib / 10, 101 * gib, 200 * gib} {
			v := Build(Input{Lang: lang, Loc: loc(lang), Torrent: caching(61, 31, 38), Viewer: atCap, ClaimCapMbps: 5, Offers: liveOffers(), SizeBytes: size})
			if sub := v.Plan.Download.Box.Sub; strings.Contains(strings.ReplaceAll(sub, "...", ""), "..") {
				t.Errorf("%s %d: %q", lang, size, sub)
			}
		}
	}
}

// The details popover of the design, row by row.
func TestBuild_Details(t *testing.T) {
	v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: caching(61, 31, 38), Viewer: atCap, ClaimCapMbps: 5, Offers: liveOffers(), SizeBytes: gb12})
	if v.Details.Title != "Где узкое место" {
		t.Errorf("title %q", v.Details.Title)
	}
	want := [4]Row{
		{Show: true, Key: "swarm", Label: "Из торрент-сети", Sub: "31 сид в раздаче", Value: "38 Мбит/с"},
		{Show: true, Key: "source", Label: "Кэш", Sub: "что уже скачано к нам", Value: "61%"},
		{Show: true, Key: "you", Label: "К вам", Tag: "потолок", Sub: "≈ 0,6 МБ/с — так же покажет загрузчик браузера", Value: "5 Мбит/с"},
		{Show: true, Key: "cap", Label: "Без подписки", Sub: "на все загрузки одновременно", Value: "до 5 Мбит/с"},
	}
	for i, r := range v.Details.Rows {
		r.Sub, r.Value = nb(r.Sub), nb(r.Value)
		if r != want[i] {
			t.Errorf("row %d\n got %+v\nwant %+v", i, r, want[i])
		}
	}
	// No cap (an unlimited plan) — no cap row; no reading — no "to you".
	v = Build(Input{Lang: "ru", Loc: loc("ru"), Tier: "sparkling", Torrent: caching(61, 31, 38)})
	if v.Details.Rows[3].Show || v.Details.Rows[2].Show {
		t.Errorf("rows %+v", v.Details.Rows)
	}
}

// The sticky mirror, and the details popover with it, exist only with the
// chain: something moves, or the viewer waits.
func TestBuild_Sticky(t *testing.T) {
	cases := []struct {
		name string
		in   Input
		want bool
	}{
		{"swarm moving", Input{Torrent: caching(43, 14, 38)}, true},
		{"viewer receiving from the cache", Input{Torrent: Torrent{State: "cached"}, Viewer: flowing(12)}, true},
		{"plan-limited", Input{Torrent: Torrent{State: "vaulted"}, Viewer: atCap}, true},
		{"the viewer waits", Input{Torrent: caching(43, 14, 0), Viewer: stalled}, true},
		{"paused, viewer idle", Input{Torrent: Torrent{State: "caching", Paused: true, Seeders: 3, SwarmKnown: true}, Viewer: zero}, false},
		{"cached, no reading", Input{Torrent: Torrent{State: "cached"}}, false},
		{"idle torrent", Input{Torrent: Torrent{State: "idle"}, Viewer: zero}, false},
	}
	for _, c := range cases {
		c.in.Lang, c.in.Loc = "ru", loc("ru")
		if got := Build(c.in).Sticky; got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// Precedence: the swarm's absence outranks the plan (no CTA on no seeders);
// a paused swarm while the viewer still receives is not "nobody downloads".
func TestBuild_KeyPrecedence(t *testing.T) {
	noseed := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: Torrent{State: "caching", NoSeeders: true, SwarmKnown: true}, Viewer: atCap, Offers: liveOffers()})
	if noseed.Key != KeyNoSeed || noseed.Plan != nil {
		t.Errorf("no seeders at the cap: %s", noseed.Key)
	}
	pausedButReceiving := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: Torrent{State: "caching", Paused: true, Seeders: 14, SwarmKnown: true}, Viewer: flowing(4)})
	if pausedButReceiving.Key != KeyActive || pausedButReceiving.Nodes[0].Show {
		t.Errorf("paused swarm, viewer receiving: %s %s", pausedButReceiving.Key, chain(pausedButReceiving))
	}
	// Many seeders but slow: not "few seeders".
	if k := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: caching(8, 14, 1.2), Viewer: flowing(1.2), ClaimCapMbps: 5}).Key; k != KeyActive {
		t.Errorf("14 slow seeders: %s", k)
	}
	// Few seeders, but faster than the cap: the swarm is not the limit.
	if k := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: caching(8, 2, 12), Viewer: flowing(4), ClaimCapMbps: 5}).Key; k != KeyActive {
		t.Errorf("2 fast seeders: %s", k)
	}
	// Few slow seeders and nothing to the viewer: nothing of theirs is slow.
	if k := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: caching(8, 2, 1.2), Viewer: zero, ClaimCapMbps: 5}).Key; k != KeyCachingOnly {
		t.Errorf("2 slow seeders, the viewer idle: %s", k)
	}
	// SSR before the first status: "checking", not "idle".
	if v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: Torrent{State: "idle", Pending: true}}); v.Key != KeyChecking || v.Nodes[0].Value != "" || v.Nodes[1].Value != "" || v.Mode != ModeBadge {
		t.Errorf("pending: %s %s", v.Key, chain(v))
	}
}

// English: a dot, "Mbps", the unit behind a no-break space.
func TestBuild_English(t *testing.T) {
	v := Build(Input{Lang: "en", Loc: loc("en"), Torrent: caching(8, 2, 1.2), Viewer: flowing(1.2), ClaimCapMbps: 5})
	if v.Segs[0].Speed != "1.2\u00a0Mbps" || v.Segs[0].Note != "few seeders" || v.Nodes[0].Value != "2 seeders" {
		t.Errorf("%q %q %q", v.Segs[0].Speed, v.Segs[0].Note, v.Nodes[0].Value)
	}
}

// Every locale renders every state without a raw key leaking through.
func TestBuild_AllLocalesAllStates(t *testing.T) {
	missing := holes("caching", 43)
	missing.ReaderMissing = 2
	states := []Input{
		{Torrent: caching(43, 14, 38), Viewer: flowing(12)},
		{Torrent: caching(43, 14, 38), Viewer: zero},
		{Torrent: caching(43, 14, 0), Viewer: zero},
		{Torrent: caching(61, 31, 38), Viewer: atCap, BitrateMbps: 8, SizeBytes: gb12},
		{Torrent: Torrent{State: "cached"}, Viewer: atCap, SizeBytes: gb12},
		{Torrent: Torrent{State: "cached"}, Viewer: flowing(3)},
		{Torrent: Torrent{State: "cached"}, Viewer: zero},
		{Torrent: Torrent{State: "vaulted"}, Viewer: atCap, SizeBytes: gb12},
		{Torrent: Torrent{State: "vaulted"}, Viewer: zero},
		{Torrent: caching(8, 2, 1.2), Viewer: flowing(1.2)},
		{Torrent: caching(43, 3, 38), Viewer: stalled},
		{Torrent: missing, Viewer: stalled},
		{Torrent: holes("caching", 43), Viewer: zero},
		{Torrent: holes("vaulting", 58), Viewer: zero},
		{Torrent: Torrent{State: "caching", Paused: true, SwarmKnown: true, Seeders: 1}, Viewer: zero},
		{Torrent: Torrent{State: "caching", NoSeeders: true, SwarmKnown: true}, Viewer: zero},
		{Torrent: Torrent{State: "caching", Checking: true}, Viewer: zero},
		{Torrent: Torrent{State: "vaulting", Progress: 3, Seeders: 1, SwarmKnown: true, RateBps: 1e6}, Viewer: zero},
		{Torrent: Torrent{State: "vaulting", Progress: 3, Seeders: 1, SwarmKnown: true, RateBps: 1e6}, Viewer: flowing(2)},
		{Torrent: Torrent{State: "vaulting", Progress: 3, Seeders: 1, SwarmKnown: true}, Viewer: zero},
		{Torrent: Torrent{State: "vault_waiting", SwarmKnown: true}, Viewer: zero},
		{Torrent: Torrent{State: "vault_failed", Progress: 4}, Viewer: zero},
		{Torrent: Torrent{State: "unknown"}, Viewer: zero},
		{Torrent: Torrent{State: "idle", SwarmKnown: true, Seeders: 21, Leechers: 2}, Viewer: zero},
	}
	for _, lang := range i18n.SupportedLangs {
		for _, in := range states {
			in.Lang, in.Loc, in.Offers, in.ClaimCapMbps = lang, loc(lang), liveOffers(), 5
			v := Build(in)
			b, _ := json.Marshal(v)
			if strings.Contains(string(b), "resource.status.") || strings.Contains(string(b), "offer.") || strings.Contains(string(b), "action.") {
				t.Errorf("%s %s: a raw key: %s", lang, v.Key, b)
			}
			if v.Badge.Label == "" {
				t.Errorf("%s %s: a badge without words", lang, v.Key)
			}
		}
	}
}

// The middle node is the cache (owner, 2026-09-25): "Рой ▸ Кэш ▸ Вы". Its
// share while caching, the green check once the whole torrent is there (no
// word next to it), and Vault's own node with its share while the torrent is
// saved to Vault -- with the viewer downloading too, not "Webtor → Vault".
// The details' row reads the same: "Кэш: 61%".
func TestBuild_CacheNode(t *testing.T) {
	build := func(tr Torrent, v Viewer) *View {
		return Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: v, ClaimCapMbps: 5})
	}
	cache := func(value string) Node {
		return Node{Show: true, Kind: "cache", Icon: "cloud", Name: "Кэш", Caption: "кэш", Value: value, Short: value}
	}
	whole := Node{Show: true, Kind: "cache", Icon: "check", Name: "Кэш", Caption: "кэш", Tone: "cached"}
	vault := func(value string) Node {
		return Node{Show: true, Kind: "vault", Icon: "vault", Name: "Vault", Caption: "Vault", Value: value, Short: value, Tone: "vault"}
	}
	vaulting := Torrent{State: "vaulting", Progress: 64, Seeders: 9, SwarmKnown: true, RateBps: mbpsBytes(22), Pieces: true}
	for _, c := range []struct {
		name  string
		v     *View
		node  Node
		value string // the details' source row
	}{
		{"caching", build(caching(43.4, 14, 38), flowing(12)), cache("43%"), "43%"},
		{"caching, nothing to the viewer", build(caching(61, 31, 38), zero), cache("61%"), "61%"},
		{"whole in the cache", build(Torrent{State: "cached", Progress: 100}, flowing(24)), whole, "100%"},
		{"whole in the cache, at the cap", build(Torrent{State: "cached", Progress: 100}, atCap), whole, "100%"},
		{"into Vault, the viewer downloading", build(vaulting, flowing(12)), vault("64%"), "64%"},
		{"into Vault only", build(vaulting, zero), vault("64%"), "64%"},
		{"Vault waits, the cache serves the viewer", build(Torrent{State: "vault_waiting", SwarmKnown: true, CacheProgress: 43.7}, flowing(3)), cache("43%"), "43%"},
		{"Vault retries, the whole torrent cached", build(Torrent{State: "vault_failed", Progress: 30, SwarmKnown: true, CacheProgress: 100}, flowing(3)), whole, "100%"},
	} {
		n := c.node
		if got := c.v.Nodes[1]; got != n {
			t.Errorf("%s: node\n got %+v\nwant %+v", c.name, got, n)
		}
		if r := c.v.Details.Rows[1]; r.Label != n.Name || r.Value != c.value {
			t.Errorf("%s: details row %q %q, want %q %q", c.name, r.Label, r.Value, n.Name, c.value)
		}
	}
	// Every language names it; none by the brand.
	for _, lang := range i18n.SupportedLangs {
		for _, v := range []*View{
			Build(Input{Lang: lang, Loc: loc(lang), Torrent: caching(43, 14, 38), Viewer: flowing(12)}),
			Build(Input{Lang: lang, Loc: loc(lang), Torrent: Torrent{State: "cached", Progress: 100}, Viewer: flowing(12)}),
		} {
			n := v.Nodes[1]
			if n.Name == "" || n.Caption == "" || n.Name == "Webtor" || n.Caption == "Webtor" || strings.Contains(n.Name+n.Caption, "resource.") {
				t.Errorf("%s: the cache node %+v", lang, n)
			}
			if n.Icon == "check" && n.Value != "" {
				t.Errorf("%s: whole in the cache, and a word next to the check: %q", lang, n.Value)
			}
		}
	}
	if n := Build(Input{Lang: "en", Loc: loc("en"), Torrent: caching(43, 14, 38), Viewer: flowing(12)}).Nodes[1]; n.Name != "Cache" || n.Caption != "cache" || n.Value != "43%" {
		t.Errorf("en: %+v", n)
	}
}

// The viewer is on the chain while their requests are open -- the proxy's
// connection count, debounced by the meter (Viewer.Present) -- and never by
// the speed: a reading that says no request of theirs is open draws nobody,
// whatever number the proxy's five-second window still carries after the
// requests closed, and an open request with no number yet draws the viewer
// with a dash.
func TestBuild_PresenceNotSpeed(t *testing.T) {
	build := func(tr Torrent, v Viewer) *View {
		return Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: v, ClaimCapMbps: 5, Offers: liveOffers(), SizeBytes: gb12})
	}
	cached := Torrent{State: "cached", Progress: 100}
	// Requests closed: the window's tail, and a plan verdict still in hand.
	for name, gone := range map[string]Viewer{
		"the window's tail":  {Known: true, Mbps: 19, CapMbps: 5},
		"a verdict in hand":  {Known: true, Mbps: 5, Limited: true, PlanBox: true, CapMbps: 5},
		"a held box in hand": {Known: true, Mbps: 2.5, PlanBox: true, CapMbps: 5},
		"a stall in hand":    {Known: true, Stalled: true, CapMbps: 5},
	} {
		for _, tr := range []Torrent{cached, {State: "vaulted"}, caching(43, 14, 0), caching(43, 14, 38)} {
			v, want := build(tr, gone), build(tr, zero)
			if v.Nodes[2].Show || v.Plan != nil || !reflect.DeepEqual(v, want) {
				t.Errorf("%s, %s: %s %s %s, want %s %s", name, tr.State, v.Key, v.Mode, chain(v), want.Key, chain(want))
			}
		}
	}
	// A request open, no number yet: on the chain, the number a dash.
	conn := Viewer{Known: true, Present: true, CapMbps: 5}
	for _, c := range []struct {
		tr    Torrent
		key   string
		chain string
	}{
		{cached, KeyCachedFlow, "Кэш (cached) [off — Мбит/с] Вы"},
		{caching(43, 14, 38), KeyActive, "Рой 14 сидов [flow+ 38 Мбит/с] Кэш 43% [off — Мбит/с] Вы"},
		{caching(43, 14, 0), KeyActive, "Кэш 43% [off — Мбит/с] Вы"},
		{Torrent{State: "vaulted"}, KeyVaulted, "Vault сохранено (vault) [off — Мбит/с] Вы"},
	} {
		v := build(c.tr, conn)
		if v.Key != c.key || v.Mode != ModeChain || chain(v) != c.chain {
			t.Errorf("%s, connected: %s %s %s, want %s %s", c.tr.State, v.Key, v.Mode, chain(v), c.key, c.chain)
		}
	}
	// A paused swarm, the viewer connected: someone is downloading this.
	paused := Torrent{State: "caching", Progress: 43, Seeders: 14, SwarmKnown: true, Paused: true, Pieces: true}
	if v := build(paused, stalled); v.Key != KeyStalled || v.Mode != ModeChain {
		t.Errorf("paused, the viewer waits: %s %s", v.Key, chain(v))
	}
}

// HLS fetches its segments in bursts, and between them the proxy counts no
// request of the viewer's open -- honestly. The server cannot see the page's
// player, so for a viewer whose requests read closed it sends, next to the
// view, the whole view with the viewer on the chain at their last reading
// (View.Playing): the page draws that while its own player plays or buffers,
// and the chain does not fall to the badge in every gap.
func TestBuild_PlayingAlternative(t *testing.T) {
	cached := Torrent{State: "cached", Progress: 100}
	base := func(tr Torrent, now, last Viewer) Input {
		return Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: now, LastViewer: last, ClaimCapMbps: 5, Offers: liveOffers(), SizeBytes: gb12}
	}
	in := base(cached, zero, flowing(24))
	v := Build(in)
	if v.Key != KeyCached || v.Mode != ModeBadge || v.Nodes[2].Show {
		t.Errorf("the view itself: nobody's requests are open: %s %s", v.Key, v.Mode)
	}
	p := v.Playing
	if p == nil || p.Key != KeyCachedFlow || p.Mode != ModeChain || !p.Sticky || chain(p) != "Кэш (cached) [flow+ 24 Мбит/с] Вы" {
		t.Fatalf("the player's view: %+v", p)
	}
	alt := in
	alt.Viewer, alt.LastViewer = flowing(24), Viewer{}
	if want := Build(alt); !reflect.DeepEqual(p, want) {
		t.Errorf("the player's view is not the view of the last reading:\n got %+v\nwant %+v", p, want)
	}
	if p.Playing != nil {
		t.Error("the alternative nests")
	}
	// The swarm moving: the view keeps its chain, the alternative adds the viewer.
	if v := Build(base(caching(43, 14, 38), zero, flowing(12))); v.Key != KeyCachingOnly || v.Playing == nil ||
		chain(v.Playing) != "Рой 14 сидов [flow+ 38 Мбит/с] Кэш 43% [flow+ 12 Мбит/с] Вы" {
		t.Errorf("caching: %s / %+v", v.Key, v.Playing)
	}
	// At the cap: the plan comes with it, for the page to pick its variant.
	if v := Build(base(cached, zero, atCap)); v.Plan != nil || v.Playing == nil || v.Playing.Key != KeyCachedTier || v.Playing.Plan == nil {
		t.Errorf("at the cap: %+v", v.Playing)
	}
	// Nothing to add, or nothing to say.
	for name, c := range map[string]Input{
		"on the chain now":                 base(cached, flowing(3), flowing(24)),
		"no reading: we do not know":       base(cached, Viewer{}, flowing(24)),
		"no last reading":                  base(cached, zero, Viewer{}),
		"the last reading had no number":   base(cached, zero, Viewer{Known: true, Present: true, CapMbps: 5}),
		"the last reading was not present": base(cached, zero, Viewer{Known: true, Mbps: 12, CapMbps: 5}),
	} {
		if v := Build(c); v.Playing != nil {
			t.Errorf("%s: an alternative %+v", name, v.Playing)
		}
	}
}
