package models

import "testing"

func strPtrT(s string) *string { return &s }

// TestGetNameDistinguishesIndexers is the point of the label: a user running
// three Jackett indexers gets `<server title>` = "Jackett" for all three, so
// the name has to carry the part of the feed URL that actually differs.
func TestGetNameDistinguishesIndexers(t *testing.T) {
	for _, tt := range []struct {
		name    string
		indexer TorznabIndexer
		want    string
	}{
		{
			name: "jackett feed names its tracker",
			indexer: TorznabIndexer{
				Name: strPtrT("Jackett"),
				Url:  "https://jackett.example.com/api/v2.0/indexers/rutracker-ru/results/torznab",
			},
			want: "Jackett · rutracker-ru",
		},
		{
			name: "a second tracker on the same Jackett is a different label",
			indexer: TorznabIndexer{
				Name: strPtrT("Jackett"),
				Url:  "https://jackett.example.com/api/v2.0/indexers/nnmclub/results/torznab",
			},
			want: "Jackett · nnmclub",
		},
		{
			name: "the all-indexers aggregate names no tracker",
			indexer: TorznabIndexer{
				Name: strPtrT("Jackett"),
				Url:  "https://jackett.example.com/api/v2.0/indexers/all/results/torznab",
			},
			want: "Jackett",
		},
		{
			name: "prowlarr numbers its indexers",
			indexer: TorznabIndexer{
				Name: strPtrT("Prowlarr"),
				Url:  "https://prowlarr.example.com/api/v1/indexer/3/newznab",
			},
			want: "Prowlarr · #3",
		},
		{
			name: "a bare feed falls back to the host",
			indexer: TorznabIndexer{
				Url: "https://tracker.example.com/rss/torznab",
			},
			want: "tracker.example.com",
		},
		{
			name: "no caps probe, but the path still names the tracker",
			indexer: TorznabIndexer{
				Url: "https://jackett.example.com/api/v2.0/indexers/rutracker-ru/results/torznab",
			},
			want: "rutracker-ru",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.indexer.GetName(); got != tt.want {
				t.Errorf("GetName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTrackerFromFeedURL(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"https://j.example.com/api/v2.0/indexers/rutracker-ru/results/torznab", "rutracker-ru"},
		{"https://j.example.com/api/v2.0/indexers/all/results/torznab", ""},
		{"https://p.example.com/api/v1/indexer/12/newznab", "#12"},
		{"https://t.example.com/torznab/api", ""},
		{"not a url at all", ""},
		{"", ""},
	} {
		if got := TrackerFromFeedURL(tt.in); got != tt.want {
			t.Errorf("TrackerFromFeedURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
