package offer

import (
	"math"

	"github.com/webtor-io/web-ui/helpers"
)

// minPitchSeconds: below ten minutes at the free cap the wait is not what
// makes anyone pay — "3 min instead of 20 s" sells nothing, "2 h instead of
// 12 min" does. Smaller files still get the nudge, without the clock.
const minPitchSeconds = 600

// Translate renders an i18n key with template data in a language.
type Translate func(lang, key string, data map[string]any) string

// Helper exposes offers to templates: promoOffer, hasPlans, downloadPitch.
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

// SpeedUp is how many times faster the promo plan downloads than rateMbps,
// rounded down; 0 when that is not worth saying (under 2×, an unknown rate or
// an unlimited plan). The CTA quotes it: "Download up to 10× faster".
// Template usage: {{ $x := speedUp .Data.RateMbps }}
func (h *Helper) SpeedUp(rateMbps int) int {
	return speedUp(h.s.Promo(), rateMbps)
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
// the promo plan's.
type Pitch struct {
	Size     string
	Slow     string
	Fast     string
	FastRate int
}

// DownloadPitch prices a download in time: nil when there is no promo plan,
// the user's or the plan's rate is unknown/unlimited, the plan is not faster,
// or the file is too small for the wait to matter.
// Template usage: {{ with downloadPitch $.Lang .Data.SizeBytes .Data.RateMbps }}
func (h *Helper) DownloadPitch(lang string, sizeBytes int64, rateMbps int) *Pitch {
	o := h.s.Promo()
	if o == nil {
		return nil
	}
	return pitch(o, sizeBytes, rateMbps, func(sec float64) string { return h.duration(lang, sec) })
}

func pitch(o *Offer, sizeBytes int64, rateMbps int, format func(float64) string) *Pitch {
	if sizeBytes <= 0 || rateMbps <= 0 || o.RateMbps <= rateMbps {
		return nil
	}
	slow := transferSeconds(sizeBytes, rateMbps)
	if slow < minPitchSeconds {
		return nil
	}
	return &Pitch{
		Size:     helpers.Bytes(uint64(sizeBytes)),
		Slow:     format(slow),
		Fast:     format(transferSeconds(sizeBytes, o.RateMbps)),
		FastRate: o.RateMbps,
	}
}

// transferSeconds is the best case at a cap: the swarm may deliver slower,
// never faster, which is why the copy says "about".
func transferSeconds(sizeBytes int64, rateMbps int) float64 {
	return float64(sizeBytes) * 8 / (float64(rateMbps) * 1e6)
}

func (h *Helper) duration(lang string, sec float64) string {
	key, data := durationParts(sec)
	return h.tr(lang, key, data)
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
