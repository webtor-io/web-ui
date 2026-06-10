package enrich

import "testing"

func TestReviewHasLink(t *testing.T) {
	cases := []struct {
		content string
		want    bool
	}{
		{"Great movie, loved the pacing.", false},
		{"Full review at https://myblog.example/reviews/123", true},
		{"check it out: http://bit.ly/xyz", true},
		{"More on www.spamsite.com right now", true},
		{"S.W.A.T. was better than I expected", false},
		{"They visit amazon.com in one scene", false}, // bare domain — deliberately kept
		{"WWW just like the wrestling promo", false},
	}
	for _, c := range cases {
		if got := reviewHasLink(c.content); got != c.want {
			t.Errorf("reviewHasLink(%q) = %v, want %v", c.content, got, c.want)
		}
	}
}
