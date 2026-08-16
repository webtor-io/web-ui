package stremio

import (
	"github.com/webtor-io/web-ui/models"
	ptn "github.com/webtor-io/web-ui/services/parse_torrent_name"
)

// This file is the one place a release name becomes a resolution bucket.
// Three Go call sites (PreferredStream's grouping, the enrich stream's
// vault-first sort, the subscription poller's preference filter) and the
// Discover client (streamPrefs.js resolutionOf) all speak the profile's
// vocabulary — 4k / 1080p / 720p / other — and they have to agree: the note
// Discover renders next to a stream and the filter the poller applies to
// the same release must reach the same verdict. Before this helper existed
// the Go copies passed the parser's output through verbatim, so a 480p or
// 1440p release fell into no bucket at all while the client filed it under
// "other".

// bucketParser is shared and read-only: GetFieldParser hands out one global
// parser per field.
var bucketParser = ptn.NewCompoundParser([]ptn.Parser{ptn.GetFieldParser(ptn.FieldTypeResolution)})

// bucketVocabulary is the profile's resolution vocabulary, from the same
// defaults the settings page renders.
var bucketVocabulary = func() map[string]bool {
	out := map[string]bool{}
	for _, r := range models.GetDefaultStremioSettings().PreferredResolutions {
		out[r.Resolution] = true
	}
	return out
}()

// ResolutionBucket names a release's resolution in the profile's vocabulary.
// 2160p is what the parser says and "4k" is what the profile calls it;
// everything the parser cannot place — and everything it places outside the
// vocabulary, 480p and 576p and 1440p included — is "other", which the
// profile offers as its own explicit choice.
func ResolutionBucket(name string) string {
	ms := ptn.Matches{}
	ms, err := bucketParser.Parse(name, ms)
	if err != nil {
		return "other"
	}
	ti := &ptn.TorrentInfo{}
	ti.Map(ms)
	switch ti.Resolution {
	case "":
		return "other"
	case "2160p":
		return "4k"
	}
	if !bucketVocabulary[ti.Resolution] {
		return "other"
	}
	return ti.Resolution
}
