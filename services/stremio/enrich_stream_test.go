package stremio

import "testing"

func lib(name string, cached bool) StreamItem {
	return StreamItem{
		Name:   name,
		Cached: cached,
		BehaviorHints: &StreamBehaviorHints{
			BingeGroup: libraryBingeGroupPrefix + "deadbeef",
		},
	}
}

func addon(name string, cached bool) StreamItem {
	return StreamItem{Name: name, Cached: cached}
}

func assertOrder(t *testing.T, got []StreamItem, want []string) {
	t.Helper()
	g := streamNames(got)
	if len(g) != len(want) {
		t.Fatalf("order = %v, want %v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("order = %v, want %v", g, want)
		}
	}
}

// A non-cached library stream must stay above a cached addon stream when they
// share a resolution bucket. Sorting on Cached alone (the old behavior) let
// the cached addon overtake it, re-inverting PreferredStream's library-first
// ordering.
func TestSortVaultFirstByResolution_LibraryBeatsCachedAddon(t *testing.T) {
	streams := []StreamItem{
		lib("lib 1080p", false),
		addon("addon-cached 1080p", true),
		addon("addon-plain 1080p", false),
	}
	sortVaultFirstByResolution(streams)
	assertOrder(t, streams, []string{"lib 1080p", "addon-cached 1080p", "addon-plain 1080p"})
}

// Within the same origin tier, cached items still float ahead of non-cached.
func TestSortVaultFirstByResolution_CachedFirstWithinTier(t *testing.T) {
	streams := []StreamItem{
		addon("addon-plain 720p", false),
		addon("addon-cached 720p", true),
	}
	sortVaultFirstByResolution(streams)
	assertOrder(t, streams, []string{"addon-cached 720p", "addon-plain 720p"})
}

// Streams in different resolution buckets are not reordered across buckets —
// only the global resolution order set upstream is preserved.
func TestSortVaultFirstByResolution_NoCrossBucketMove(t *testing.T) {
	streams := []StreamItem{
		addon("addon 1080p", false),
		lib("lib 720p", false),
	}
	sortVaultFirstByResolution(streams)
	// Different resolutions → each is its own singleton bucket → no movement.
	assertOrder(t, streams, []string{"addon 1080p", "lib 720p"})
}
