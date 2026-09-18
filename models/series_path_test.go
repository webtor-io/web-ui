package models

import "testing"

// pickSeriesRow mirrors pickMovieRow: one resource can hold several shows,
// and naming the wrong one is worse than naming none. See
// movie_path_test.go for the movie half's rationale.
func TestPickSeriesRow(t *testing.T) {
	p := func(s string) *string { return &s }
	a := &Series{Episodes: []*Episode{{Path: p("/A/S01E01.mkv")}, {Path: p("/A/S01E02.mkv")}}}
	b := &Series{Episodes: []*Episode{{Path: p("/B/S01E01.mkv")}}}

	if got := pickSeriesRow([]*Series{a, b}, "/B/S01E01.mkv"); got != b {
		t.Fatal("the series owning the episode at the path wins")
	}
	if got := pickSeriesRow([]*Series{a, b}, "/C/other.mkv"); got != nil {
		t.Fatal("two shows and a path neither owns: no answer beats the wrong show")
	}
	if got := pickSeriesRow([]*Series{a}, "/C/other.mkv"); got != a {
		t.Fatal("a lone series is the answer whatever the path")
	}
	if got := pickSeriesRow(nil, "/x"); got != nil {
		t.Fatal("nothing enriched, nothing answered")
	}
}
