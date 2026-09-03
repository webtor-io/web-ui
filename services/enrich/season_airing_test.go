package enrich

import "testing"

func ep(season int) map[string]any { return map[string]any{"season_number": float64(season)} }

// Silo, 2026-09: status Returning Series, last aired episode in S02, no
// next episode scheduled. S01 and S02 are finished; S03 is announced.
func TestSeasonAiring(t *testing.T) {
	returning := map[string]any{"status": "Returning Series", "in_production": true, "last_episode_to_air": ep(2)}
	midSeason := map[string]any{"status": "Returning Series", "last_episode_to_air": ep(3), "next_episode_to_air": ep(3)}
	betweenSeasonsScheduled := map[string]any{"status": "Returning Series", "last_episode_to_air": ep(2), "next_episode_to_air": ep(3)}
	ended := map[string]any{"status": "Ended", "in_production": false, "last_episode_to_air": ep(5)}
	returningNoEpisodes := map[string]any{"status": "Returning Series"}
	cases := []struct {
		name   string
		meta   map[string]any
		season int
		want   bool
	}{
		{"returning series, finished S01 → no", returning, 1, false},
		{"returning series, last aired S02 → no", returning, 2, false},
		{"returning series, announced S03 → yes", returning, 3, true},
		{"mid-season S03 → yes", midSeason, 3, true},
		{"mid-season, earlier S02 → no", midSeason, 2, false},
		{"next episode is S03, asking S02 → no", betweenSeasonsScheduled, 2, false},
		{"next episode is S03, asking S03 → yes", betweenSeasonsScheduled, 3, true},
		{"next episode is S03, asking S04 → no (next decides)", betweenSeasonsScheduled, 4, false},
		{"ended series, any season → no", ended, 5, false},
		{"ended series, future season → no", ended, 6, false},
		{"returning, no episode stubs → series answer", returningNoEpisodes, 1, true},
		{"season 0 → no", returning, 0, false},
		{"empty metadata → no", map[string]any{}, 1, false},
	}
	for _, c := range cases {
		if got := seasonAiring(c.meta, c.season); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
