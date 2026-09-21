package cache_index

import "testing"

func TestAvailabilityCached(t *testing.T) {
	a := &Availability{
		torrents: map[string]bool{"vaulted": true},
		files:    map[string]map[int]bool{"pack": {3: true}},
	}
	cases := []struct {
		name string
		hash string
		idx  int
		want bool
	}{
		{"a vaulted torrent answers for every file", "vaulted", 7, true},
		{"and for a release that names no file", "vaulted", -1, true},
		{"the very file the index saw", "pack", 3, true},
		{"another file of the same torrent is not cached", "pack", 4, false},
		{"no file named: something of it is here", "pack", -1, true},
		{"hashes are compared case-insensitively", "PACK", 3, true},
		{"an unknown torrent", "other", 0, false},
		{"an unknown torrent with no file named", "other", -1, false},
	}
	for _, c := range cases {
		if got := a.Cached(c.hash, c.idx); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	var none *Availability
	if none.Cached("x", 0) {
		t.Error("a nil answer knows nothing")
	}
}
