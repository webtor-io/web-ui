package statusview

import "testing"

func BenchmarkBuild_Tier(b *testing.B) {
	in := Input{Lang: "ru", Loc: loc("ru"), Torrent: caching(61, 31, 38), Viewer: atCap, ClaimCapMbps: 5, Offers: liveOffers(), SizeBytes: gb12}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Build(in)
	}
}

func BenchmarkBuild_Active(b *testing.B) {
	in := Input{Lang: "ru", Loc: loc("ru"), Torrent: caching(43, 14, 38), Viewer: flowing(12), ClaimCapMbps: 5, Offers: liveOffers()}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Build(in)
	}
}
