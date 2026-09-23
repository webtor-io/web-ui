package stremio

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
)

// The paywall clips: a short video a free viewer's Stremio player plays in
// place of a stream Webtor would have to serve — "this stream plays through
// Webtor's servers, that takes a plan, start a free trial at webtor.io/trial".
// Without it the player got a 404 and showed a bare error, 354 times a week
// (2026-09-16..23), to 74 addon tokens.
//
// The files are rendered from the stremio.paywall.* locale keys by
// scripts/stremio_paywall_video (see docs/stremio.md) and live in pub/, which
// handlers/static serves at /pub.
const (
	paywallDir     = "pub/stremio"
	paywallURLPath = "/pub/stremio/"
)

var paywallFile = regexp.MustCompile(`^paywall-([a-z]{2})\.mp4$`)

// promoSource is the one question resolve asks the offers: what is on sale.
type promoSource interface {
	Promo() *offer.Offer
}

// paywallClips are the clips found on disk at startup, by language.
type paywallClips struct {
	// base is the absolute URL prefix of the clips, on the site's domain.
	base string
	// paths maps a language to its clip's file name plus a content digest
	// (?v=), so a re-rendered clip is a new URL for every cache on the way.
	paths map[string]string
}

// loadPaywallClips reads the clips in dir. A missing directory is not an
// error: the deployment simply has no clips, and free viewers get the 404
// they always got.
func loadPaywallClips(dir, domain string) (*paywallClips, error) {
	p := &paywallClips{
		base:  strings.TrimSuffix(domain, "/") + paywallURLPath,
		paths: map[string]string{},
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to list paywall clips")
	}
	for _, e := range entries {
		m := paywallFile.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read paywall clip %s", e.Name())
		}
		sum := sha256.Sum256(b)
		p.paths[m[1]] = e.Name() + "?v=" + hex.EncodeToString(sum[:])[:8]
	}
	return p, nil
}

// Langs lists the languages a clip exists for, sorted.
func (p *paywallClips) Langs() []string {
	out := make([]string, 0, len(p.paths))
	for l := range p.paths {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// pick is where a free viewer's click goes instead of a 404: the clip in the
// account's language, English when there is none in it. ok=false keeps the
// 404, and that happens in three cases:
//
//   - nothing is on sale (no catalog, no promo plan) — a deployment without
//     a storefront must not advertise someone else's;
//   - the promo plan has no trial the checkout can start (TrialDays == 0).
//     The clip is static and says "start a free trial"; offer.Offer only
//     reports a trial someone can actually begin, and the clip follows the
//     same rule rather than promise one;
//   - no clip was rendered for either language.
func (p *paywallClips) pick(o *offer.Offer, accountLang string) (url, lang string, ok bool) {
	if o == nil || o.TrialDays <= 0 || p == nil {
		return "", "", false
	}
	lang = accountLang
	path, found := p.paths[lang]
	if !found {
		lang = i18n.DefaultLang
		path, found = p.paths[lang]
	}
	if !found {
		return "", "", false
	}
	return p.base + path, lang, true
}
