package common

import (
	"strconv"
	"strings"
)

// The body of a tool page is not free-form: every one of them is the same
// five-section shape — how it works, what the thing is, what you get, a
// comparison or a format list, and a word on safety — with a different i18n
// prefix. That was written out as one 130-line partial per page, which meant
// 19 copies of the same markup and a new copy for every landing page added.
//
// So the page is data: each tool declares its sections, and
// templates/partials/about/sections.html renders them. Adding a landing page
// is a Section list plus its locale keys.

// AboutKind is how a section is laid out. The kinds are deliberately few —
// a sixth one is a sign the page wants a bespoke partial, not a new flag.
type AboutKind string

const (
	// AboutSteps is the three numbered cards every page opens with.
	AboutSteps AboutKind = "steps"
	// AboutProse is a heading over one to three paragraphs.
	AboutProse AboutKind = "prose"
	// AboutChecklist is a heading, a subtitle and a grid of ticked items.
	AboutChecklist AboutKind = "checklist"
	// AboutCompare is two labelled columns of bullets, side by side.
	AboutCompare AboutKind = "compare"
)

// AboutSection is one section of a tool page.
//
// Key names the i18n sub-tree the section reads: with Key "benefits" on
// /torrent-to-mp4, the section renders tool.torrentToMp4.about.benefits.*.
// Prefix is the resolved "tool.<tool>.about" half and is stamped in at
// startup, so the literals below never repeat the tool name.
type AboutSection struct {
	Kind AboutKind
	Key  string
	// Badge is the tool.about.badges.<Badge> label above the heading.
	Badge string
	// Icon selects the badge glyph; empty means "same name as Badge".
	Icon string
	// Accent is the section's colour token: pink, cyan or purple.
	Accent string
	// Alt puts the section on the darker background. Sections alternate.
	Alt bool
	// Paras are the prose fields to render, in order ("p1", "p2", "text").
	Paras []string
	// Items is how many ticked items a checklist has.
	Items int
	// Cols are the two i18n sub-keys of a comparison's columns. The first
	// renders pink, the second cyan.
	Cols []string
	// Footer is a closing paragraph under the grid; Note is the smaller,
	// boxed variant of it.
	Footer bool
	Note   bool
	// Extra is an i18n sub-key rendered as a cyan callout under a
	// checklist — one page pairs its benefits with a note about indexers.
	Extra string
	// CTA names the call-to-action template closing the steps section.
	CTA string
	// Link is a cross-link closing a prose section: one landing page points
	// at another where the query overlaps.
	Link *AboutLink

	// Prefix is filled at startup from the owning tool. Not set in literals.
	Prefix string
}

// AboutKey is the i18n prefix of a tool's page copy. The convention — kebab
// URL, camel key — is what the locale guard in tools_test.go checks, so it is
// derived rather than repeated per tool.
func (t Tool) AboutKey() string {
	parts := strings.Split(t.Url, "-")
	out := parts[0]
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		out += strings.ToUpper(p[:1]) + p[1:]
	}
	return "tool." + out + ".about"
}

// IconName is the badge glyph to draw, defaulting to the badge's own name.
func (s AboutSection) IconName() string {
	if s.Icon != "" {
		return s.Icon
	}
	return s.Badge
}

// ItemKeys are the i18n keys of a checklist's items, resolved so the template
// does not have to build them.
func (s AboutSection) ItemKeys() []string {
	out := make([]string, 0, s.Items)
	for i := 1; i <= s.Items; i++ {
		out = append(out, s.FieldKey("item"+itoa(i)))
	}
	return out
}

// FieldKey resolves one field of this section, e.g. "title" →
// "tool.torrentToMp4.about.benefits.title".
func (s AboutSection) FieldKey(field string) string {
	return s.Prefix + "." + s.Key + "." + field
}

// AboutLink is a cross-link to another tool page. The label is that page's
// own title key, so the two never drift apart.
type AboutLink struct {
	Url      string
	TitleKey string
}

// AboutPara is one prose paragraph: its resolved key and whether it closes
// the section, which is what decides its bottom margin.
type AboutPara struct {
	Key  string
	Last bool
}

// ParaList resolves a prose section's paragraphs.
func (s AboutSection) ParaList() []AboutPara {
	out := make([]AboutPara, 0, len(s.Paras))
	for i, p := range s.Paras {
		out = append(out, AboutPara{Key: s.FieldKey(p), Last: i == len(s.Paras)-1})
	}
	return out
}

// ColItemKeys are the i18n keys of one comparison column's bullets.
func (s AboutSection) ColItemKeys(col string) []string {
	out := make([]string, 0, 4)
	for i := 1; i <= 4; i++ {
		out = append(out, s.FieldKey(col+".item"+itoa(i)))
	}
	return out
}

func itoa(i int) string { return strconv.Itoa(i) }

// stampAboutPrefixes fills every section's Prefix from its tool. Called from
// the package init below so no caller can forget it.
func stampAboutPrefixes() {
	for i := range Tools {
		prefix := Tools[i].AboutKey()
		for j := range Tools[i].Sections {
			Tools[i].Sections[j].Prefix = prefix
		}
	}
}

func init() { stampAboutPrefixes() }
