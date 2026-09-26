package offer

import (
	"math"
	"regexp"

	"github.com/webtor-io/web-ui/helpers"
)

// Translate renders an i18n key with template data in a language.
type Translate func(lang, key string, data map[string]any) string

// Helper exposes offers to templates: promoOffer, hasPlans, freeRateMbps,
// downloadPitch.
type Helper struct {
	s  *Service
	tr Translate
}

func NewHelper(s *Service, tr Translate) *Helper {
	return &Helper{s: s, tr: tr}
}

// PromoOffer is the plan upsells sell, nil when there is nothing to sell.
// Template usage: {{ with promoOffer }}…{{ end }}
func (h *Helper) PromoOffer() *Offer {
	return h.s.Promo()
}

// HasPlans: the storefront has plans — an "upgrade" link leads somewhere.
func (h *Helper) HasPlans() bool {
	return h.s.HasPlans()
}

// FreeRateMbps is the free plan's speed cap in Mbps, 0 when there is none to
// state. Template usage: {{ with freeRateMbps }}…{{ end }}
func (h *Helper) FreeRateMbps() int {
	return h.s.FreeRateMbps()
}

// SpeedUp is how many times faster the promo plan downloads than rateMbps,
// rounded down; 0 when that is not worth saying (under 2×, an unknown rate or
// an unlimited plan). The CTA quotes it: "Download up to 10× faster".
// Template usage: {{ $x := speedUp .Data.RateMbps }}
func (h *Helper) SpeedUp(rateMbps int) int {
	return speedUp(h.s.Promo(), rateMbps)
}

// SpeedUp is Helper.SpeedUp for Go code that already holds the offer it
// quotes (the resource status, services/statusview), so its button and its
// ETA read one snapshot of the catalog.
func SpeedUp(o *Offer, rateMbps int) int {
	return speedUp(o, rateMbps)
}

func speedUp(o *Offer, rateMbps int) int {
	if o == nil || o.RateMbps <= 0 || rateMbps <= 0 {
		return 0
	}
	if x := o.RateMbps / rateMbps; x >= 2 {
		return x
	}
	return 0
}

// Pitch is the download nudge in numbers: this file at the user's cap and at
// the promo plan's. ETA is the sentence (action.download.eta) in the
// viewer's language, the one way both the nudge and the status's plan box
// say it.
type Pitch struct {
	Size     string
	Slow     string
	Fast     string
	FastRate int
	ETA      string
}

// DownloadPitch prices a download in time, for any file whose size is known:
// the difference is meant to be felt on every download, a 3-minute one
// included ("about 3 min. With a subscription — about 20 s"). nil when there
// is no promo plan, the size or a rate is unknown/unlimited, or the plan is
// not faster.
// Template usage: {{ with downloadPitch $.Lang .Data.SizeBytes .Data.RateMbps }}
func (h *Helper) DownloadPitch(lang string, sizeBytes int64, rateMbps int) *Pitch {
	o := h.s.Promo()
	if o == nil {
		return nil
	}
	return pitch(o, sizeBytes, rateMbps, func(key string, data map[string]any) string { return h.tr(lang, key, data) })
}

// PitchWith is DownloadPitch for Go code that already holds the offer: tr
// renders the duration keys (offer.eta.*) in the viewer's language. nil for
// a nil offer, and whenever DownloadPitch would be nil.
func PitchWith(o *Offer, sizeBytes int64, rateMbps int, tr func(key string, data map[string]any) string) *Pitch {
	if o == nil || tr == nil {
		return nil
	}
	return pitch(o, sizeBytes, rateMbps, tr)
}

func pitch(o *Offer, sizeBytes int64, rateMbps int, tr func(key string, data map[string]any) string) *Pitch {
	if sizeBytes <= 0 || rateMbps <= 0 || o.RateMbps <= rateMbps {
		return nil
	}
	format := func(sec float64) string {
		key, data := durationParts(sec)
		return tr(key, data)
	}
	p := &Pitch{
		Size:     helpers.Bytes(uint64(sizeBytes)),
		Slow:     format(transferSeconds(sizeBytes, rateMbps)),
		Fast:     format(transferSeconds(sizeBytes, o.RateMbps)),
		FastRate: o.RateMbps,
	}
	p.ETA = oneStop(tr("action.download.eta", map[string]any{"Size": p.Size, "Slow": p.Slow, "Fast": p.Fast}))
	return p
}

// doubleStop is a sentence's full stop right after an abbreviation's
// ("34 Min.." in German, "2 дн.." in Russian): the ETA sentence ends after
// {{.Slow}}, and in de, pl and ru a duration can end in an abbreviated unit.
// Not an ellipsis: the character before it is not a stop.
var doubleStop = regexp.MustCompile(`([^.])\.\.(\s|$)`)

// oneStop keeps one of the two stops, as those orthographies do.
func oneStop(s string) string { return doubleStop.ReplaceAllString(s, "$1.$2") }

// rateBits is how many bits a second one Mbps of a cap is: thp's limiter
// reads the rate claim with bytefmt, whose "M" is 2^20 (5M = 5·2^20 bit/s,
// the same megabit services/statusview labels speeds in). Pricing at 10^6
// quoted every wait about 5% longer than the cap delivers.
const rateBits = 1 << 20

// transferSeconds is the best case at a cap: the swarm may deliver slower,
// never faster, which is why the copy says "about".
func transferSeconds(sizeBytes int64, rateMbps int) float64 {
	return float64(sizeBytes) * 8 / (float64(rateMbps) * rateBits)
}

// durationParts picks the two most significant units, rounded to the nearest
// minute (hour for multi-day waits). Under a minute it counts seconds in
// fives: rounding 20 s up to "1 min" made a 10× plan read as 3× next to a
// 3-minute wait.
func durationParts(sec float64) (string, map[string]any) {
	if sec < 57.5 {
		s := int(math.Round(sec/5)) * 5
		if s < 5 {
			s = 5
		}
		return "offer.eta.sec", map[string]any{"S": s}
	}
	min := int(math.Round(sec / 60))
	if min < 1 {
		min = 1
	}
	if min < 60 {
		return "offer.eta.min", map[string]any{"M": min}
	}
	if min < 24*60 {
		h, m := min/60, min%60
		if m == 0 {
			return "offer.eta.h", map[string]any{"H": h}
		}
		return "offer.eta.hMin", map[string]any{"H": h, "M": m}
	}
	hours := int(math.Round(sec / 3600))
	d, h := hours/24, hours%24
	if h == 0 {
		return "offer.eta.d", map[string]any{"D": d}
	}
	return "offer.eta.dH", map[string]any{"D": d, "H": h}
}
