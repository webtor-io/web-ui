package action

import (
	"sort"
	"strings"

	"github.com/webtor-io/web-ui/services/stremio"
)

// maxVisibleLangChips is how many language chips the subtitle row shows
// before the rest collapse behind a "+N" disclosure (controller ruling on
// the track-picker plan: 6, not the brief's 4 — the design was redrawn
// wider). The sort below always puts the active language first and the
// viewer's preferred language right after it, so both are guaranteed a
// visible slot — the "+N" only ever hides languages the viewer has not
// expressed any preference for.
const maxVisibleLangChips = 6

// LangGroup is one chip of the subtitle language row: a language, how many
// tracks it has, and whether the track currently playing is one of them.
type LangGroup struct {
	stremio.LangDisplay
	Count int
	// Active is "the track playing right now is in this language". The chip
	// carries a dot for it, so the selection stays visible even while the
	// viewer is browsing another language's tracks.
	Active bool
	// Overflow chips are rendered but hidden behind the "+N" button.
	Overflow bool
}

// LangRow is the whole row plus the two things the template would otherwise
// have to recompute by looping: which language opens expanded, and how many
// chips sit behind "+N". One helper call instead of three.
type LangRow struct {
	Groups   []LangGroup
	Expanded string
	Overflow int
}

// SubtitleLangGroups groups the subtitle list by base language for the
// picker's language row.
//
// Order: the language of the track playing first, then the viewer's
// preferred language, then by track count, then by the order GetSubtitles
// produced (a stable sort keeps the ladder as the tie-break rather than a
// map's iteration). Because Active and Preferred are each true for at most
// one group, this ordering is also what keeps both languages out of the
// "+N" overflow (see maxVisibleLangChips): they always land in the first
// two slots, ahead of every group sorted purely by count. The client
// recomputes this order in assets/src/js/lib/player/track-picker.js
// (groupByLang) after an upload changes the counts — the two
// implementations must stay identical, and the table in
// TestSubtitleLangGroupsOrdersActiveThenPreferredThenCount is mirrored by
// the same fixture in track-picker.test.js.
//
// The "None" item is not a language and gets no chip: it is the "Off" chip
// at the head of the track row, not the language row, so it never enters
// grouping here in the first place.
func (s *Helper) SubtitleLangGroups(lis []ListItem, preferredLang string) LangRow {
	preferred := stremio.NewLangDisplay(preferredLang).Lang
	var order []string
	byLang := map[string]*LangGroup{}
	for _, li := range lis {
		if li.ID == "none" {
			continue
		}
		d := stremio.NewLangDisplay(li.SrcLang)
		g, ok := byLang[d.Lang]
		if !ok {
			g = &LangGroup{LangDisplay: d}
			byLang[d.Lang] = g
			order = append(order, d.Lang)
		}
		g.Count++
		if li.Default {
			g.Active = true
		}
	}
	out := make([]LangGroup, 0, len(order))
	for _, l := range order {
		out = append(out, *byLang[l])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		if pi, pj := out[i].Lang == preferred, out[j].Lang == preferred; pi != pj {
			return pi
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return false
	})
	row := LangRow{Groups: out}
	for i := range row.Groups {
		if i >= maxVisibleLangChips {
			row.Groups[i].Overflow = true
			row.Overflow++
		}
	}
	if len(row.Groups) > 0 {
		row.Expanded = row.Groups[0].Lang
	}
	return row
}

// originCodes maps an origin badge to the fixed two-letter code the picker
// shows. Codes never localize (docs/uikit.html §19): they are codes, and
// their meaning is carried by title= and by the legend line, both built
// from the action.stream.badge.* keys.
var originCodes = map[string]string{
	"user":     "MY",
	"embedded": "EM",
	"sidecar":  "IN",
	"os":       "OS",
	"ai":       "AI",
}

// OriginCode is the code for where a track came from.
//
// Derived from Provider through badgeFor(provider, false), not from
// li.Badge: a forced track's Badge is "forced", which says what kind of
// track it is rather than where it came from, and a chip that showed
// "forced" in the origin slot would leave the viewer with no way to tell an
// embedded signs track from one shipped in the torrent.
func (s *Helper) OriginCode(li ListItem) string {
	return originCodes[badgeFor(li.Provider, false)]
}

// OriginCodeForBadge is OriginCode for a badge string rather than an item —
// the AI track's SourceBadge, which names the origin it was translated from
// ("· from EM"). Returns "" for "forced", which is not an origin.
func (s *Helper) OriginCodeForBadge(badge string) string {
	return originCodes[badge]
}

// OriginKey is the i18n key that explains the code in the viewer's
// language: the chip's title and the legend line both use it.
func (s *Helper) OriginKey(li ListItem) string {
	b := badgeFor(li.Provider, false)
	if _, ok := originCodes[b]; !ok {
		return ""
	}
	return "action.stream.badge." + b
}

// PropertyTags are what kind of track this is, as opposed to where it came
// from: lowercase codes rendered after the file name, secondary to the
// origin badge. Only "forced" exists today; "sdh" is drawn in the uikit and
// waits for content-prober to expose ffprobe's disposition flags.
func (s *Helper) PropertyTags(li ListItem) []string {
	if li.Forced {
		return []string{"forced"}
	}
	return nil
}

// AudioSuffix is the part of an audio track's label that the language chip
// does not already say: "Dub", "Commentary", "Audio (5.1) #2". Empty when
// the label is just the language name, so "🇬🇧 English · English" never
// happens.
func (s *Helper) AudioSuffix(li ListItem) string {
	label := strings.TrimSpace(li.Label)
	d := stremio.NewLangDisplay(li.SrcLang)
	if d.Name != "" && strings.EqualFold(label, d.Name) {
		return ""
	}
	return label
}
