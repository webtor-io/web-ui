package statusview

import (
	"testing"
	"time"
)

// A long gap between pieces must not collapse the chain while the viewer
// still downloads or streams an incomplete torrent. The existing hold may
// smooth the speed, but its expiry must only stop the swarm's arrow.
func TestBuild_SwarmStaysUntilSourceComplete(t *testing.T) {
	for _, source := range []struct{ filling, complete string }{
		{"caching", "cached"}, {"vaulting", "vaulted"},
	} {
		for _, streaming := range []bool{false, true} {
			name := source.filling + "/download"
			if streaming {
				name = source.filling + "/stream-between-segments"
			}
			t.Run(name, func(t *testing.T) {
				var hold Hold
				start := time.Unix(1000, 0)
				for _, step := range []struct {
					at     time.Duration
					rate   float64
					moving bool
				}{
					{0, 22, true},
					{5 * time.Second, 0, true},
					{30 * time.Second, 0, false},
					{time.Minute, 0, false},
					{61 * time.Second, 22, true},
				} {
					tr := caching(43, 14, step.rate)
					tr.State = source.filling
					in := Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: flowing(12),
						HeldBps: hold.Swarm(mbpsBytes(step.rate), start.Add(step.at))}
					if streaming {
						in.Viewer, in.LastViewer = zero, flowing(12)
					}
					v := Build(in)
					if streaming {
						v = v.Playing
					}
					if v == nil {
						t.Fatalf("+%v: no view for the playing video", step.at)
					}
					if v.Mode != ModeChain || !v.Nodes[0].Show || !v.Nodes[1].Show || !v.Nodes[2].Show || !v.Segs[0].Show || !v.Segs[1].Show || !v.Details.Rows[0].Show {
						t.Errorf("+%v: incomplete source lost a participant: %s", step.at, chain(v))
					}
					if v.Segs[0].On != step.moving || (!step.moving && nb(v.Segs[0].Speed) == "22 Мбит/с") {
						t.Errorf("+%v: swarm motion/speed does not follow the gap: %+v", step.at, v.Segs[0])
					}
				}
				// Completion removes the swarm even while its old rate is held.
				in := Input{Lang: "ru", Loc: loc("ru"), Torrent: Torrent{State: source.complete, Progress: 100},
					Viewer: flowing(12), HeldBps: mbpsBytes(22)}
				if streaming {
					in.Viewer, in.LastViewer = zero, flowing(12)
				}
				v := Build(in)
				if streaming {
					v = v.Playing
				}
				if v == nil || v.Mode != ModeChain || v.Nodes[0].Show || v.Segs[0].Show || !v.Nodes[2].Show {
					t.Fatalf("complete source: want only source and viewer, got %+v", v)
				}
				// A stopped download or a paused player uses the ordinary view:
				// presence is not latched by the stable swarm layout.
				in.Torrent = Torrent{State: source.filling, Progress: 43}
				in.Viewer, in.HeldBps = zero, 0
				v = Build(in)
				if v.Mode != ModeBadge || v.Nodes[0].Show || v.Nodes[2].Show || v.Plan != nil {
					t.Errorf("viewer left: want idle badge, got %+v", v)
				}
			})
		}
	}
}

func TestBuild_PageViewerPhasesIgnoreTransferNoise(t *testing.T) {
	for _, source := range []string{"caching", "vaulting", "cached", "vaulted"} {
		for _, reading := range []Viewer{zero, flowing(12), {Known: true, Present: true, Limited: true, PlanBox: true, Mbps: 5, CapMbps: 5}} {
			v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: Torrent{State: source, Progress: 43}, Viewer: reading, LastViewer: flowing(12)})
			alt := v.Resting
			if alt == nil {
				t.Fatal("missing resting view")
			}
			if alt.Mode != ModeChain || !alt.Nodes[2].Show || alt.Nodes[0].Show != (source == "caching" || source == "vaulting") {
				t.Errorf("%s: route %+v", source, alt.Nodes)
			}
			if alt.Plan != nil || alt.Segs[1].On || alt.Segs[1].Note != "" || alt.Details.Rows[2].Sub != "" || alt.Details.Rows[2].Tag != "" {
				t.Errorf("%s: invented traffic or cap: %+v", source, alt)
			}
			if alt.Segs[1].Speed != "нет передачи" || alt.Details.Rows[2].Value != "нет передачи" || v.PausedLabel != "пауза" || v.PreparingLabel != "ждём данные" {
				t.Errorf("wrong phase labels: %+v", v)
			}
			if alt.Playing != nil || alt.Resting != nil {
				t.Fatal("nested alternatives")
			}
		}
	}
}

// Before the first piece, the page already has a viewer waiting for the
// action. Its route needs a swarm even before stats reach "caching".
func TestBuild_PageViewerWaitsForInitialSwarm(t *testing.T) {
	for _, tr := range []Torrent{
		{State: "idle", Seeders: 14, SwarmKnown: true},
		{State: "idle", Pending: true},
		{State: "unknown"},
	} {
		v := Build(Input{Lang: "ru", Loc: loc("ru"), Torrent: tr, Viewer: zero})
		if v.Nodes[0].Show || v.Nodes[2].Show || v.Mode != ModeBadge {
			t.Fatalf("ordinary idle page changed: %+v", v)
		}
		r := v.Resting
		if !r.Nodes[0].Show || !r.Segs[0].Show || !r.Details.Rows[0].Show || !r.Nodes[2].Show {
			t.Errorf("%+v: incomplete page route: %+v", tr, r.Nodes)
		}
		if r.Segs[0].Speed != "ждём данные" || r.Segs[0].On || r.Segs[0].Dots || r.Segs[0].Note != "" || r.Details.Rows[0].Value != r.Segs[0].Speed {
			t.Errorf("%+v: expected waiting without invented speed: %+v", tr, r.Segs[0])
		}
	}
}
