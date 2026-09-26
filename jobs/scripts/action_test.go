package scripts

import (
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"slices"
	"testing"

	claimsproto "github.com/webtor-io/claims-provider/proto"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/web"
)

func ctxWith(rate, tier string) *web.Context {
	c := &web.Context{}
	if rate != "" {
		c.ApiClaims = &api.Claims{Rate: rate}
	}
	if tier != "" {
		c.Claims = &claims.Data{Context: &claimsproto.Context{Tier: &claimsproto.Tier{Name: tier}}}
	}
	return c
}

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestParseRateLimit(t *testing.T) {
	cases := map[string]int64{
		"":    0,
		"10M": 10_000_000,
		"1M":  1_000_000,
		"10":  0,
		"10K": 0,
		"xM":  0,
	}
	for in, want := range cases {
		if got := parseRateLimit(in); got != want {
			t.Errorf("parseRateLimit(%q) = %d, want %d", in, got, want)
		}
	}
}

// The warm-up measures the swarm (what the seeder fetched from its peers), not
// the viewer's link: a swarm at the plan's cap that still falls short of the
// file is the swarm's shortfall, and a faster plan would not fix it -- the
// modal says so and sells nothing (owner, 2026-09-26). Before, a swarm within
// 90% of the cap read as "rate-limited" and got the trial button.
func TestBuildSlowDownloadData_SwarmAtTheCapIsNotTheCap(t *testing.T) {
	c := ctxWith("10M", "free")
	// The swarm gave 1.2 MB/s ≈ 9.6 Mbit/s: 96% of the 10 Mbit/s cap, and
	// short of a 12 Mbit/s file.
	sdd := buildSlowDownloadData(c, 1_200_000, 12_000_000)
	if sdd.IsRateLimited {
		t.Fatal("a swarm at the cap is still the swarm: IsRateLimited must be false")
	}
	if sdd.RateLimitMbps != 0 {
		t.Errorf("RateLimitMbps = %v, want 0 (the cap is not what this modal speaks of)", sdd.RateLimitMbps)
	}
	if !almostEqual(sdd.MeasuredSpeedMbps, 9.6) {
		t.Errorf("MeasuredSpeedMbps = %v, want 9.6", sdd.MeasuredSpeedMbps)
	}
	if !almostEqual(sdd.RequiredSpeedMbps, 12) {
		t.Errorf("RequiredSpeedMbps = %v, want 12 (= bitrate)", sdd.RequiredSpeedMbps)
	}
	if sdd.TierName != "free" {
		t.Errorf("TierName = %q, want free", sdd.TierName)
	}
}

func TestBuildSlowDownloadData_SlowNotCappedFreeFallback(t *testing.T) {
	c := &web.Context{}
	// No claims at all: slow peers, no cap. IsRateLimited must be false, tier defaults to "free".
	sdd := buildSlowDownloadData(c, 500_000, 8_000_000)
	if sdd.IsRateLimited {
		t.Error("no claims should not flag IsRateLimited")
	}
	if sdd.TierName != "free" {
		t.Errorf("TierName = %q, want free fallback", sdd.TierName)
	}
	if sdd.RateLimitMbps != 0 {
		t.Errorf("RateLimitMbps = %v, want 0", sdd.RateLimitMbps)
	}
}

func TestBuildSlowDownloadData_CapPresentButNotSaturated(t *testing.T) {
	// Cap=20M, measured 500KB/s (4Mbit/s) — slow for other reasons, not rate limit.
	c := ctxWith("20M", "free")
	sdd := buildSlowDownloadData(c, 500_000, 8_000_000)
	if sdd.IsRateLimited {
		t.Error("measured speed far below cap should not be classified as rate-limited")
	}
}

func TestCheckCachedRateLimit_NoCap(t *testing.T) {
	c := &web.Context{}
	if _, limited := checkCachedRateLimit(c, 8_000_000); limited {
		t.Error("no ApiClaims => not limited")
	}
	c = ctxWith("", "premium")
	if _, limited := checkCachedRateLimit(c, 8_000_000); limited {
		t.Error("empty Rate => not limited")
	}
}

func TestCheckCachedRateLimit_CapSufficient(t *testing.T) {
	// Cap=20M, bitrate=8M. Cap > bitrate => not limited.
	c := ctxWith("20M", "basic")
	if _, limited := checkCachedRateLimit(c, 8_000_000); limited {
		t.Error("cap above bitrate should not raise warning")
	}
}

func TestCheckCachedRateLimit_CapInsufficient(t *testing.T) {
	// Cap=5M, bitrate=8M. Cap < bitrate => warn.
	c := ctxWith("5M", "free")
	sdd, limited := checkCachedRateLimit(c, 8_000_000)
	if !limited {
		t.Fatal("cap below bitrate should raise warning")
	}
	if !sdd.IsRateLimited {
		t.Error("SlowDownloadData.IsRateLimited should be true on cached path")
	}
	if !almostEqual(sdd.RateLimitMbps, 5) {
		t.Errorf("RateLimitMbps = %v, want 5", sdd.RateLimitMbps)
	}
	// For cached path the "measured" speed equals the cap.
	if !almostEqual(sdd.MeasuredSpeedMbps, 5) {
		t.Errorf("MeasuredSpeedMbps = %v, want 5 (== cap)", sdd.MeasuredSpeedMbps)
	}
	if !almostEqual(sdd.RequiredSpeedMbps, 8) {
		t.Errorf("RequiredSpeedMbps = %v, want 8 (= bitrate)", sdd.RequiredSpeedMbps)
	}
	if sdd.TierName != "free" {
		t.Errorf("TierName = %q, want free", sdd.TierName)
	}
}

func TestCheckCachedRateLimit_CapEqualsRequirement(t *testing.T) {
	// Boundary: cap exactly at bitrate — treat as sufficient.
	c := ctxWith("8M", "basic")
	if _, limited := checkCachedRateLimit(c, 8_000_000); limited {
		t.Error("cap == bitrate should not raise warning")
	}
}

// The bandwidth gate holds the plan's cap against what the player pulls
// (playedBitrate), the number the transfer status's "…and this file needs N"
// is made of -- not the file's own rate with every dub in it -- and falls
// back to the file's rate only where what the player pulls is not known.
func TestCapGateBitrate(t *testing.T) {
	const aac48 = 128 * 48000 / 44
	dts := probeJSON(t, probeTwoDTSDubs)
	cases := []struct {
		name       string
		mp         *api.MediaProbe
		transcoded bool
		want       int64
	}{
		{"two dubs: the stream, not the file", dts, true, 3500000 + aac48},
		{"the owner's file: the video and one dub", probeJSON(t, probeOwner), true, 8934213 + aac48},
		{"re-encoded video, not known: the file's rate",
			probeJSON(t, `{"format":{"bit_rate":"5650625"},"streams":[{"codec_type":"video","codec_name":"hevc","tags":{"BPS":"4999862"}},
			{"codec_type":"audio","codec_name":"eac3","bit_rate":"640000","channels":6,"sample_rate":"48000"}]}`), true, 5650625},
		{"stale tags, not known: the file's rate", probeJSON(t, probeStale), true, 718649},
		{"no rate of the file's either: its streams' sum",
			probeJSON(t, `{"format":{},"streams":[{"codec_type":"video","codec_name":"hevc","bit_rate":"4000000"},
			{"codec_type":"audio","codec_name":"aac","bit_rate":"128000","channels":2,"sample_rate":"48000"}]}`), true, 4128000},
	}
	for _, c := range cases {
		if got := capGateBitrate(c.mp, c.transcoded); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
	// The same number as the status's line: the cap modal and the status
	// speak of one stream.
	owner := probeJSON(t, probeOwner)
	if capGateBitrate(owner, true) != playedBitrate(owner, true) {
		t.Error("the cap gate and the status's marks read different rates")
	}
	// At a 5 Mbps cap the two-dub file streams at 3.6: no cap modal for it.
	// By the file's 6.6 it had one.
	capped := ctxWith("5M", "free")
	if _, limited := checkCachedRateLimit(capped, capGateBitrate(dts, true)); limited {
		t.Error("a 3.6 Mbps stream at a 5 Mbps cap got the cap modal")
	}
	if sdd, limited := checkCachedRateLimit(capped, capGateBitrate(probeJSON(t, probeOwner), true)); !limited || !almostEqual(sdd.RequiredSpeedMbps, float64(8934213+aac48)/1_000_000) {
		t.Errorf("the owner's file over a 5 Mbps cap: limited %v, file needs %v", limited, sdd.RequiredSpeedMbps)
	}
}

// 3.5 Mbps H.264 with two 1.5 Mbps DTS dubs: 6.6 Mbps as a file, 3.64
// played (the video and one dub re-encoded to AAC).
const probeTwoDTSDubs = `{"format":{"bit_rate":"6600000"},"streams":[
	{"codec_type":"video","codec_name":"h264","tags":{"BPS":"3500000"}},
	{"codec_type":"audio","codec_name":"dts","bit_rate":"1500000","channels":6,"sample_rate":"48000"},
	{"codec_type":"audio","codec_name":"dts","bit_rate":"1500000","channels":6,"sample_rate":"48000"}]}`

// The swarm is held against the file's own rate: the seeder fetches every
// track to play one. A swarm between the played rate and the file's still
// gets the BT-slow modal, the full measure first when the quick warm-up's
// lower bound is under the file's rate; by the played rate it passed at once
// and stalled (2026-09-26 review, F1).
func TestSwarmGate_HoldsTheFileRate(t *testing.T) {
	c := ctxWith("", "free")
	cases := []struct {
		name       string
		probe      string
		lowerBound float64 // bytes a second: the quick warm-up's
		measured   float64 // bytes a second: the full measure's
		fileNeeds  float64 // Mbps, the modal's
	}{
		// 4.8 Mbps lower bound, 5 Mbps measured: over 3.64 played, under
		// 6.6 of file.
		{"two DTS dubs", probeTwoDTSDubs, 600_000, 625_000, 6.6},
		// FLAC at ~900 kbps transcodes to 139.6 kbps of AAC; a 0.5 Mbps
		// swarm cannot fetch the FLAC.
		{"transcoded FLAC", `{"format":{"bit_rate":"900000"},"streams":[
			{"codec_type":"audio","codec_name":"flac","channels":2,"sample_rate":"48000"}]}`, 62_500, 62_500, 0.9},
	}
	for _, tc := range cases {
		mp := probeJSON(t, tc.probe)
		fileRate, streamRate := getVideoBitrate(mp), capGateBitrate(mp, true)
		if !(float64(streamRate) < tc.measured*8 && tc.measured*8 < float64(fileRate)) {
			t.Fatalf("%s: the case needs a swarm between the played rate %d and the file's %d", tc.name, streamRate, fileRate)
		}
		if !needsFullMeasure(tc.lowerBound, fileRate, streamWarmupSize, bandwidthTestSize) {
			t.Errorf("%s: a %.1f Mbps lower bound passed a %.1f Mbps file unmeasured", tc.name, tc.lowerBound*8/1e6, float64(fileRate)/1e6)
		}
		if !swarmTooSlow(tc.measured, fileRate) {
			t.Errorf("%s: a %.1f Mbps swarm passed a %.1f Mbps file", tc.name, tc.measured*8/1e6, float64(fileRate)/1e6)
		}
		sdd := buildSlowDownloadData(c, tc.measured, fileRate)
		if sdd.IsRateLimited || !almostEqual(sdd.RequiredSpeedMbps, tc.fileNeeds) {
			t.Errorf("%s: modal rate-limited %v, file needs %v, want the swarm's modal at %v", tc.name, sdd.IsRateLimited, sdd.RequiredSpeedMbps, tc.fileNeeds)
		}
	}
	if swarmTooSlow(0, 6_600_000) {
		t.Error("an unmeasured swarm (0) got the BT-slow modal")
	}
	if swarmTooSlow(825_000, 6_600_000) {
		t.Error("a swarm at exactly the file's rate got the BT-slow modal")
	}
}

// streamContent is not testable without the whole stream start behind it,
// so this holds the wiring of Step 3: the swarm's checks (needsFullMeasure,
// swarmTooSlow, the BT-slow modal's buildSlowDownloadData) read the file's
// own rate (getVideoBitrate), the cap's (checkCachedRateLimit) the played
// stream's (capGateBitrate), and nothing else reads either.
func TestGateBitrate_WhichRateEachBranchReads(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "action.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	callee := func(e ast.Expr) string {
		if c, ok := e.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok {
				return id.Name
			}
		}
		return ""
	}
	calls := map[string][]string{} // callee -> the functions calling it
	var gate *ast.FuncDecl
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if fn.Name.Name == "streamContent" {
			gate = fn
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if name := callee(asExpr(n)); name == "getVideoBitrate" || name == "capGateBitrate" {
				calls[name] = append(calls[name], fn.Name.Name)
			}
			return true
		})
	}
	if got := calls["getVideoBitrate"]; len(got) != 2 || !slices.Contains(got, "capGateBitrate") || !slices.Contains(got, "streamContent") {
		t.Errorf("getVideoBitrate called from %v, want capGateBitrate's fallback and the swarm's gate in streamContent", got)
	}
	if got := calls["capGateBitrate"]; len(got) != 1 || got[0] != "streamContent" {
		t.Errorf("capGateBitrate called from %v, want the cap's gate in streamContent", got)
	}
	if gate == nil {
		t.Fatal("no streamContent")
	}
	// The variable each rate is bound to in streamContent.
	source := map[string]string{} // variable -> the function it was assigned from
	ast.Inspect(gate.Body, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok && len(a.Lhs) == len(a.Rhs) {
			for i, l := range a.Lhs {
				if id, ok := l.(*ast.Ident); ok {
					if name := callee(a.Rhs[i]); name != "" {
						source[id.Name] = name
					}
				}
			}
		}
		return true
	})
	want := []struct {
		fn   string
		arg  int
		rate string
	}{
		{"needsFullMeasure", 1, "getVideoBitrate"},
		{"swarmTooSlow", 1, "getVideoBitrate"},
		{"buildSlowDownloadData", 2, "getVideoBitrate"},
		{"checkCachedRateLimit", 1, "capGateBitrate"},
	}
	seen := map[string]int{}
	ast.Inspect(gate.Body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, w := range want {
			if callee(c) != w.fn {
				continue
			}
			seen[w.fn]++
			id, ok := c.Args[w.arg].(*ast.Ident)
			if !ok || source[id.Name] != w.rate {
				t.Errorf("%s reads %s, want the rate from %s", w.fn, describeArg(c.Args[w.arg], source), w.rate)
			}
		}
		return true
	})
	for fn, n := range map[string]int{"needsFullMeasure": 1, "swarmTooSlow": 2, "buildSlowDownloadData": 2, "checkCachedRateLimit": 2} {
		if seen[fn] != n {
			t.Errorf("streamContent calls %s %d times, want %d: the wiring changed, check this test still holds it", fn, seen[fn], n)
		}
	}
}

func asExpr(n ast.Node) ast.Expr {
	if e, ok := n.(ast.Expr); ok {
		return e
	}
	return nil
}

// describeArg names an argument for a failure: the variable and where it
// came from.
func describeArg(e ast.Expr, source map[string]string) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name + " (from " + source[id.Name] + ")"
	}
	return "an expression"
}

func makePieces(n int, complete []int) []struct {
	Position int  `json:"position"`
	Complete bool `json:"complete"`
	Priority int  `json:"priority"`
} {
	set := map[int]bool{}
	for _, i := range complete {
		set[i] = true
	}
	out := make([]struct {
		Position int  `json:"position"`
		Complete bool `json:"complete"`
		Priority int  `json:"priority"`
	}, n)
	for i := 0; i < n; i++ {
		out[i].Position = i
		out[i].Complete = set[i]
	}
	return out
}

func TestPiecesCoverRange(t *testing.T) {
	// 10 pieces, file = 100MB → 10MB/piece.
	const fileSize = 100 * 1024 * 1024
	allComplete := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	t.Run("all complete, head only", func(t *testing.T) {
		ev := api.EventData{Pieces: makePieces(10, allComplete)}
		if !piecesCoverRange(ev, fileSize, 30*1024*1024, 0) {
			t.Error("all-complete should cover 30MB head")
		}
	})

	t.Run("head missing one", func(t *testing.T) {
		// First 3 pieces cover 30MB but piece 1 missing.
		ev := api.EventData{Pieces: makePieces(10, []int{0, 2, 3, 4, 5, 6, 7, 8, 9})}
		if piecesCoverRange(ev, fileSize, 30*1024*1024, 0) {
			t.Error("missing head piece must fail coverage")
		}
	})

	t.Run("tail missing", func(t *testing.T) {
		// All except the last piece.
		ev := api.EventData{Pieces: makePieces(10, []int{0, 1, 2, 3, 4, 5, 6, 7, 8})}
		if piecesCoverRange(ev, fileSize, 30*1024*1024, 500*1024) {
			t.Error("missing tail piece must fail coverage")
		}
	})

	t.Run("head+tail covered but middle missing", func(t *testing.T) {
		ev := api.EventData{Pieces: makePieces(10, []int{0, 1, 2, 8, 9})}
		if !piecesCoverRange(ev, fileSize, 30*1024*1024, 20*1024*1024) {
			t.Error("middle gap is irrelevant when only head+tail are checked")
		}
	})

	t.Run("empty pieces", func(t *testing.T) {
		if piecesCoverRange(api.EventData{}, fileSize, 1024, 0) {
			t.Error("empty pieces should never report cached")
		}
	})

	t.Run("head exceeds file", func(t *testing.T) {
		ev := api.EventData{Pieces: makePieces(10, allComplete)}
		if !piecesCoverRange(ev, fileSize, fileSize*10, 0) {
			t.Error("head > size should clamp to full file (all complete here)")
		}
	})

	t.Run("ceil rounding pulls in extra piece", func(t *testing.T) {
		// 30MB+1 byte needs piece 3 too — and piece 3 is missing.
		ev := api.EventData{Pieces: makePieces(10, []int{0, 1, 2, 4, 5, 6, 7, 8, 9})}
		if piecesCoverRange(ev, fileSize, 30*1024*1024+1, 0) {
			t.Error("ceil should pull in piece 3, which is missing")
		}
	})

	t.Run("overlap small file", func(t *testing.T) {
		// 4 pieces, head wants 2 pieces, tail wants 3 pieces → overlap. Only
		// "all complete" should pass; one gap anywhere fails.
		ev := api.EventData{Pieces: makePieces(4, []int{0, 1, 2, 3})}
		if !piecesCoverRange(ev, 40*1024*1024, 20*1024*1024, 30*1024*1024) {
			t.Error("overlapping head+tail with full coverage should pass")
		}
		ev2 := api.EventData{Pieces: makePieces(4, []int{0, 1, 3})}
		if piecesCoverRange(ev2, 40*1024*1024, 20*1024*1024, 30*1024*1024) {
			t.Error("overlap covering piece 2 should fail — it's missing")
		}
	})
}
