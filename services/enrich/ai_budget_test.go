package enrich

import (
	"testing"
)

func i16p(v int16) *int16 { return &v }

func TestCandidatesEqual(t *testing.T) {
	t.Run("same title same year — equal", func(t *testing.T) {
		a := []TitleCandidate{{Title: "Dragon Ball", Year: i16p(1986)}}
		b := []TitleCandidate{{Title: "Dragon Ball", Year: i16p(1986)}}
		if !candidatesEqual(a, b) {
			t.Fatal("expected equal")
		}
	})
	t.Run("title differs — not equal", func(t *testing.T) {
		a := []TitleCandidate{{Title: "Dragon Ball", Year: i16p(1986)}}
		b := []TitleCandidate{{Title: "Dragon Ball Z", Year: i16p(1986)}}
		if candidatesEqual(a, b) {
			t.Fatal("expected NOT equal")
		}
	})
	t.Run("year differs — not equal", func(t *testing.T) {
		a := []TitleCandidate{{Title: "Dragon Ball", Year: i16p(1986)}}
		b := []TitleCandidate{{Title: "Dragon Ball", Year: i16p(2009)}}
		if candidatesEqual(a, b) {
			t.Fatal("expected NOT equal")
		}
	})
	t.Run("both years nil — equal", func(t *testing.T) {
		a := []TitleCandidate{{Title: "Some Show"}}
		b := []TitleCandidate{{Title: "Some Show"}}
		if !candidatesEqual(a, b) {
			t.Fatal("expected equal when both years nil")
		}
	})
	t.Run("one year nil — not equal", func(t *testing.T) {
		a := []TitleCandidate{{Title: "X", Year: i16p(2020)}}
		b := []TitleCandidate{{Title: "X"}}
		if candidatesEqual(a, b) {
			t.Fatal("expected NOT equal when year nil mismatch")
		}
	})
	t.Run("language is ignored", func(t *testing.T) {
		a := []TitleCandidate{{Title: "X", Year: i16p(2020), Language: "ru"}}
		b := []TitleCandidate{{Title: "X", Year: i16p(2020), Language: "en"}}
		if !candidatesEqual(a, b) {
			t.Fatal("language should not affect equality")
		}
	})
	t.Run("different length — not equal", func(t *testing.T) {
		a := []TitleCandidate{{Title: "X"}}
		b := []TitleCandidate{{Title: "X"}, {Title: "Y"}}
		if candidatesEqual(a, b) {
			t.Fatal("expected NOT equal")
		}
	})
	t.Run("order matters", func(t *testing.T) {
		a := []TitleCandidate{{Title: "X"}, {Title: "Y"}}
		b := []TitleCandidate{{Title: "Y"}, {Title: "X"}}
		if candidatesEqual(a, b) {
			t.Fatal("Claude returns candidates in priority order; flipped order is a different answer")
		}
	})
}

// TestBudgetLocksOnDuplicate exercises the central Dragon-Ball case: the
// parser hands the enricher N "different" titles for one logical work,
// Claude returns the same set each time. After the SECOND identical
// answer the budget locks and remaining files reuse the cache without
// calling Claude.
func TestBudgetLocksOnDuplicate(t *testing.T) {
	b := &resourceAIBudget{}
	cands := []TitleCandidate{{Title: "Dragon Ball", Year: i16p(1986)}}

	// File 1 — first call seeds lastCandidates, doesn't lock.
	b.recordCall(cands)
	if b.locked != nil {
		t.Fatalf("locked too early after 1 call")
	}
	if b.uniqueCalls != 1 {
		t.Fatalf("uniqueCalls = %d, want 1", b.uniqueCalls)
	}

	// File 2 — identical answer locks.
	b.recordCall(cands)
	if b.locked == nil {
		t.Fatalf("expected lock after 2 identical answers")
	}
	if got := b.locked[0].Title; got != "Dragon Ball" {
		t.Fatalf("locked candidates wrong: %v", b.locked)
	}

	// File 3+ — additional recordCall mutates only the counter.
	b.recordCall(cands)
	if b.uniqueCalls != 3 {
		t.Fatalf("uniqueCalls = %d, want 3", b.uniqueCalls)
	}
	if b.locked[0].Title != "Dragon Ball" {
		t.Fatalf("lock got clobbered after 3rd recordCall")
	}
}

// Two different Claude responses on files 1 and 2 must NOT lock — that's
// a legit multi-work pack where every file is genuinely-different.
func TestBudgetDoesNotLockOnDifferentAnswers(t *testing.T) {
	b := &resourceAIBudget{}
	b.recordCall([]TitleCandidate{{Title: "Movie A", Year: i16p(2010)}})
	b.recordCall([]TitleCandidate{{Title: "Movie B", Year: i16p(2012)}})
	if b.locked != nil {
		t.Fatalf("must not lock when answers differ")
	}
	// lastCandidates rolls forward to the most recent response so a
	// matching 3rd answer still has a chance to lock.
	if b.lastCandidates == nil || b.lastCandidates[0].Title != "Movie B" {
		t.Fatalf("expected lastCandidates = Movie B, got %+v", b.lastCandidates)
	}
}

// Once locked, a subsequent identical answer doesn't change the lock
// (no-op). Non-identical follow-ups also leave the lock alone — the
// lock is "sticky" because it's strong evidence the parser is wrong.
func TestBudgetLockIsSticky(t *testing.T) {
	b := &resourceAIBudget{}
	cands := []TitleCandidate{{Title: "Дуплет", Year: i16p(2025)}}
	b.recordCall(cands)
	b.recordCall(cands) // lock here
	other := []TitleCandidate{{Title: "Что-то другое"}}
	b.recordCall(other)
	if b.locked == nil || b.locked[0].Title != "Дуплет" {
		t.Fatalf("lock should remain after non-matching follow-up, got %+v", b.locked)
	}
}

// available() must return false once we've hit the hard cap, even if
// the budget hasn't failed.
func TestBudgetCapAtFiveUniqueCalls(t *testing.T) {
	b := &resourceAIBudget{}
	for i := 0; i < maxUniqueAICalls; i++ {
		if !b.available() {
			t.Fatalf("available() returned false at call %d, before reaching cap", i)
		}
		b.recordCall([]TitleCandidate{{Title: "X"}})
	}
	if b.available() {
		t.Fatalf("available() must be false at exactly the cap (uniqueCalls=%d)", b.uniqueCalls)
	}
}

func TestBudgetMarkFailedShortCircuits(t *testing.T) {
	b := &resourceAIBudget{}
	b.recordCall([]TitleCandidate{{Title: "X"}})
	b.markFailed()
	if b.available() {
		t.Fatalf("available() must be false once failed")
	}
}

// nil budget is the LookupByTitleYear path — must not panic and must
// stay "available" (no tracking).
func TestBudgetNilSafe(t *testing.T) {
	var b *resourceAIBudget
	if !b.available() {
		t.Fatal("nil budget must be available")
	}
	b.recordCall([]TitleCandidate{{Title: "X"}})
	b.markFailed() // no-op
}
