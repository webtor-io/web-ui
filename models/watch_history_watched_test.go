package models

import "testing"

func TestIsWatched(t *testing.T) {
	const d = 3000 // a 50-minute episode
	cases := []struct {
		name              string
		pos, dur, credits float32
		want              bool
	}{
		{"the old rule: 90%", 2700, d, 0, true},
		{"below 90%, credits unknown", 2500, d, 0, false},
		{"ten minutes of credits: watched when they begin, at 80%", 2405, d, 2400, true},
		{"still talking", 2390, d, 2400, false},
		{"whichever comes first: 90% before late credits", 2700, d, 2950, true},
		{"credits claimed earlier than credits can be: not believed", 1000, d, 900, false},
		{"credits claimed in the last 25 s: nothing the 90% rule lacks", 2980, d, 2990, true},
		{"a bogus claim does not mark an unwatched file", 10, d, 1, false},
		{"unknown duration", 100, 0, 50, false},
	}
	for _, c := range cases {
		if got := IsWatched(c.pos, c.dur, c.credits); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
