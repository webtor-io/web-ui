package statusview

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"

	"github.com/webtor-io/web-ui/services/i18n"
)

// mbit is the megabit the plans are sold and limited in. thp reads a token's
// rate claim with bytefmt (hybrid_bucket.go rateBytesPerSec): "5M" is 5·1024²
// bits a second. Every speed on the chain — the swarm's, the viewer's, the
// cap — is counted in the same unit, so a viewer held at a "5M" cap reads
// exactly "5 Mbps" next to a cap that says 5, and the swarm's number compares
// with both.
const mbit = 1 << 20

// BytesToMbps converts bytes a second to megabits a second (see mbit).
func BytesToMbps(bytesPerSec float64) float64 {
	return bytesPerSec * 8 / mbit
}

// BitsToMbps converts bits a second (a media bitrate, as ffprobe reports it)
// to the same megabits (see mbit), so "the file needs 8" compares with the cap.
func BitsToMbps(bitsPerSec float64) float64 {
	return bitsPerSec / mbit
}

// Quantize rounds a speed to what its label shows: tenths below 10 Mbps,
// whole numbers from there. Anything that would read "0" is 0 — no flow, the
// floor below which a speed is noise rather than a transfer (half a tenth,
// ~6.5 KB/s). Two samples that print the same label quantize the same, so
// the status stream, which drops a message identical to the last, sends
// nothing while nothing visible changed.
func Quantize(mbps float64) float64 {
	if !(mbps > 0) || math.IsInf(mbps, 0) {
		return 0
	}
	if q := math.Round(mbps*10) / 10; q < 10 {
		return q
	}
	return math.Round(mbps)
}

// RateMbps reads a token's rate claim ("5M", "20M", "2.5M") the way thp's
// limiter does and returns it in Mbps (see mbit): "5M" → 5. 0 for an absent
// or unreadable claim — no cap.
func RateMbps(rate string) float64 {
	s := strings.ToUpper(strings.TrimSpace(rate))
	i := strings.IndexFunc(s, unicode.IsLetter)
	if i <= 0 {
		return 0
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil || n <= 0 || math.IsInf(n, 0) {
		return 0
	}
	var unit float64
	switch s[i:] {
	case "B":
		unit = 1
	case "K", "KB", "KIB":
		unit = 1 << 10
	case "M", "MB", "MIB":
		unit = 1 << 20
	case "G", "GB", "GIB":
		unit = 1 << 30
	case "T", "TB", "TIB":
		unit = 1 << 40
	default:
		return 0
	}
	return n * unit / mbit
}

// RateBytesPerSec is the claim in bytes a second, the unit thp reports
// delivery in.
func RateBytesPerSec(rate string) float64 {
	return RateMbps(rate) * mbit / 8
}

var printers sync.Map // language → *message.Printer

func printer(lang string) *message.Printer {
	if p, ok := printers.Load(lang); ok {
		return p.(*message.Printer)
	}
	tag, err := language.Parse(lang)
	if err != nil {
		tag = language.English
	}
	p, _ := printers.LoadOrStore(lang, message.NewPrinter(tag))
	return p.(*message.Printer)
}

// FormatNumber prints a quantized speed the way the language writes numbers:
// "1.2" in English, "1,2" in Russian and the other nine; no trailing ",0".
// The one number formatter of the chain — swarm, viewer, cap and bitrate all
// go through it, so no two of them can disagree about a separator.
func FormatNumber(lang string, v float64) string {
	return printer(lang).Sprint(number.Decimal(v, number.MaxFractionDigits(1)))
}

// dash stands for a speed that is not flowing: "— Mbps".
const dash = "—"

// speedLabel is a speed with its unit, "38 Mbps" / "1,2 Мбит/с"; v must be
// quantized. The unit and the no-break space before it come from the locale
// (resource.status.mbps), so the unit is written once per language.
func speedLabel(loc *goi18n.Localizer, lang string, v float64) string {
	return i18n.TranslateWithLocalizerData(loc, "resource.status.mbps", map[string]any{"N": FormatNumber(lang, v)})
}

// dashLabel is "— Mbps": the segment is drawn, nothing moves on it.
func dashLabel(loc *goi18n.Localizer) string {
	return i18n.TranslateWithLocalizerData(loc, "resource.status.mbps", map[string]any{"N": dash})
}
