package stremio

import (
	"testing"

	"github.com/webtor-io/web-ui/models"
	ptn "github.com/webtor-io/web-ui/services/parse_torrent_name"
)

// newTestPreferredStream builds a PreferredStream with only the resolution
// parser wired up. filterByPreferredResolutions touches nothing else (no DB,
// no inner service), so this is enough to exercise the filtering logic.
func newTestPreferredStream() *PreferredStream {
	return &PreferredStream{
		parser: ptn.NewCompoundParser([]ptn.Parser{
			ptn.GetFieldParser(ptn.FieldTypeResolution),
		}),
	}
}

// libraryStream builds a stream that isLibraryStream recognises (webtorio|
// bingeGroup prefix) — i.e. a torrent the user added to their own Vault.
func libraryStream(name, hash string) StreamItem {
	return StreamItem{
		Name:     name,
		InfoHash: hash,
		BehaviorHints: &StreamBehaviorHints{
			BingeGroup: "webtorio|" + hash,
		},
	}
}

// addonStream builds an external-addon stream (non-library bingeGroup).
func addonStream(name, hash string) StreamItem {
	return StreamItem{
		Name:     name,
		InfoHash: hash,
		BehaviorHints: &StreamBehaviorHints{
			BingeGroup: "torrentio|" + hash,
		},
	}
}

// defaultPrefs mirrors models.GetDefaultStremioSettings: 1080p/720p/other
// enabled, 4k disabled. "other" is the bucket for names with no parseable
// resolution token.
func defaultPrefs() []models.ResolutionSetting {
	return []models.ResolutionSetting{
		{Resolution: "4k", Enabled: false},
		{Resolution: "1080p", Enabled: true},
		{Resolution: "720p", Enabled: true},
		{Resolution: "other", Enabled: true},
	}
}

func streamNames(streams []StreamItem) []string {
	out := make([]string, len(streams))
	for i, s := range streams {
		out[i] = s.Name
	}
	return out
}

// TestFilterByPreferredResolutions_LibraryStreamSurvivesWhenOtherDisabled is
// the core binge-watching regression guard. A library episode whose file name
// carries no resolution token parses to "other"; with "other" disabled it used
// to be dropped, so its sibling episodes (whose names *did* carry a token)
// survived while it vanished — leaving Stremio with no matching bingeGroup
// stream for that episode and bouncing the viewer to source-selection.
func TestFilterByPreferredResolutions_LibraryStreamSurvivesWhenOtherDisabled(t *testing.T) {
	s := newTestPreferredStream()
	prefs := []models.ResolutionSetting{
		{Resolution: "1080p", Enabled: true},
		{Resolution: "other", Enabled: false}, // user opted out of "other"
	}
	streams := []StreamItem{
		libraryStream("Webtor.io", "hashNoRes"), // no resolution token → "other"
	}

	got, err := s.filterByPreferredResolutions(streams, prefs)
	if err != nil {
		t.Fatalf("filterByPreferredResolutions() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected library stream to survive, got %d streams: %v", len(got), streamNames(got))
	}
	if got[0].InfoHash != "hashNoRes" {
		t.Errorf("wrong stream survived: %v", got[0].InfoHash)
	}
}

// TestFilterByPreferredResolutions_LibraryStreamSurvivesDisabledResolution
// guards the related bug: a 4k library title stayed invisible under default
// settings (4k disabled) even though the user explicitly added it.
func TestFilterByPreferredResolutions_LibraryStreamSurvivesDisabledResolution(t *testing.T) {
	s := newTestPreferredStream()
	streams := []StreamItem{
		libraryStream("Webtor.io\n2160p", "hash4k"), // 4k → disabled in defaults
	}

	got, err := s.filterByPreferredResolutions(streams, defaultPrefs())
	if err != nil {
		t.Fatalf("filterByPreferredResolutions() error = %v", err)
	}
	if len(got) != 1 || got[0].InfoHash != "hash4k" {
		t.Fatalf("expected 4k library stream to survive, got %v", streamNames(got))
	}
}

// TestFilterByPreferredResolutions_BingeConsistencyAcrossEpisodes is the
// end-to-end shape of the bug: two episodes of the same library torrent (same
// webtorio| bingeGroup) where one file name carries a resolution token and the
// other does not. Both must survive so Stremio finds the matching bingeGroup
// stream for whichever episode plays next.
func TestFilterByPreferredResolutions_BingeConsistencyAcrossEpisodes(t *testing.T) {
	s := newTestPreferredStream()
	prefs := []models.ResolutionSetting{
		{Resolution: "1080p", Enabled: true},
		{Resolution: "other", Enabled: false},
	}
	// Same torrent/bingeGroup, two episodes, inconsistent naming.
	ep1 := libraryStream("Webtor.io\n1080p", "seasonHash")
	ep2 := libraryStream("Webtor.io", "seasonHash") // no token → "other"

	for _, ep := range []StreamItem{ep1, ep2} {
		got, err := s.filterByPreferredResolutions([]StreamItem{ep}, prefs)
		if err != nil {
			t.Fatalf("filterByPreferredResolutions() error = %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("episode %q dropped — binge would break, got %v", ep.Name, streamNames(got))
		}
		if bg := got[0].BehaviorHints.BingeGroup; bg != "webtorio|seasonHash" {
			t.Errorf("bingeGroup changed: %v", bg)
		}
	}
}

// TestFilterByPreferredResolutions_AddonStreamsStillFiltered confirms the
// exemption is scoped to library streams: external-addon streams are still
// filtered by resolution (4k disabled → dropped, 1080p enabled → kept).
func TestFilterByPreferredResolutions_AddonStreamsStillFiltered(t *testing.T) {
	s := newTestPreferredStream()
	streams := []StreamItem{
		addonStream("Some.Show.S01E01.2160p.WEB-DL", "addon4k"),   // dropped
		addonStream("Some.Show.S01E01.1080p.WEB-DL", "addon1080"), // kept
	}

	got, err := s.filterByPreferredResolutions(streams, defaultPrefs())
	if err != nil {
		t.Fatalf("filterByPreferredResolutions() error = %v", err)
	}
	if len(got) != 1 || got[0].InfoHash != "addon1080" {
		t.Fatalf("expected only the 1080p addon stream, got %v", streamNames(got))
	}
}

// TestFilterByPreferredResolutions_LibraryStreamsEmittedFirst verifies library
// streams are placed ahead of resolution-ordered addon streams.
func TestFilterByPreferredResolutions_LibraryStreamsEmittedFirst(t *testing.T) {
	s := newTestPreferredStream()
	streams := []StreamItem{
		addonStream("Some.Show.1080p.WEB-DL", "addon1080"),
		libraryStream("Webtor.io\n720p", "libHash"),
	}

	got, err := s.filterByPreferredResolutions(streams, defaultPrefs())
	if err != nil {
		t.Fatalf("filterByPreferredResolutions() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 streams, got %v", streamNames(got))
	}
	if got[0].InfoHash != "libHash" {
		t.Errorf("expected library stream first, got order %v", []string{got[0].InfoHash, got[1].InfoHash})
	}
}
