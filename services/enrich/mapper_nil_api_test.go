package enrich

import "testing"

// Every mapper constructor here has to answer nil when its provider is not
// configured, because common.go decides whether to append a mapper purely by
// what the constructor returns. A mapper that comes back non-nil around a nil
// client joins the chain and dereferences it on the first lookup -- which is
// exactly what KinopoiskUnofficial did: an instance with no KINOPOISK key
// panicked inside the enrichment job, and the job runner's recover turned it
// into a log line that read like enrichment simply failing.
//
// Asserted for all of them, not just the one that broke: they are appended by
// the same rule, so the next mapper added inherits this contract whether or
// not its author reads it here.
func TestMapperConstructorsReturnNilWithoutAProvider(t *testing.T) {
	if got := NewKinopoiskUnofficial(nil, nil); got != nil {
		t.Errorf("NewKinopoiskUnofficial with no API returned %v; it would be appended to the mapper chain and panic on the first title search", got)
	}
	if got := NewOMDB(nil, nil); got != nil {
		t.Errorf("NewOMDB with no API returned %v; same failure as above", got)
	}
	if got := NewTMDBEpisodes(nil); got != nil {
		t.Errorf("NewTMDBEpisodes with no TMDB returned %v; same failure as above", got)
	}
}
