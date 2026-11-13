// Package private_torrent tells a private torrent (BEP 27, private=1 in the
// info dict) and counts the ones that come into the store through web-ui.
// The count is a measurement only: it changes nothing about the upload. See
// docs/private_torrent_logs.md.
package private_torrent

import (
	"bytes"
	"net/url"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/services/auth"
)

// LogUpload writes one line when torrent is a private .torrent: who uploaded
// it (auth.LogHash, "anon" without an account), its infohash, its trackers
// and the door it came through (site, embed, api, webdav, s3). Anything that
// is not a private torrent -- a public one, a magnet, junk -- is no line: the
// upload's own path reports a bad file, not this one.
//
// A tracker is named by scheme, host and port alone. A private tracker's
// passkey is in the rest of its URL (?passkey=, ?pk=, a path segment), and
// userinfo is dropped with it.
func LogUpload(torrent []byte, u *auth.User, source string) {
	mi := loadPrivate(torrent)
	if mi == nil {
		return
	}
	user := "anon"
	if u != nil {
		if h := auth.LogHash(u.ID); h != "" {
			user = h
		}
	}
	log.WithFields(log.Fields{
		"event":    "private_torrent_upload",
		"user":     user,
		"infohash": mi.HashInfoBytes().HexString(),
		"trackers": strings.Join(trackerHosts(mi.UpvertedAnnounceList().DistinctValues()), ","),
		"source":   source,
	}).Info("private torrent upload")
}

// IsPrivate reports whether torrent is a .torrent with private=1 in its info
// dict. Anything that does not parse is not.
func IsPrivate(torrent []byte) bool {
	return loadPrivate(torrent) != nil
}

// loadPrivate is torrent's metainfo when it is a private .torrent, nil
// otherwise.
func loadPrivate(torrent []byte) *metainfo.MetaInfo {
	mi, err := metainfo.Load(bytes.NewReader(torrent))
	if err != nil {
		return nil
	}
	info, err := mi.UnmarshalInfo()
	if err != nil || info.Private == nil || !*info.Private {
		return nil
	}
	return mi
}

// trackerHosts is each tracker's scheme://host[:port], lower-cased, once,
// in the order the torrent lists them. A URL that does not parse, or has no
// host, is left out rather than quoted.
func trackerHosts(trackers []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, tr := range trackers {
		p, err := url.Parse(tr)
		if err != nil || p.Host == "" {
			continue
		}
		h := strings.ToLower(p.Scheme) + "://" + strings.ToLower(p.Host)
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}
