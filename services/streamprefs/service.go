package streamprefs

import (
	"context"
	"net/url"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	tm "github.com/webtor-io/web-ui/models/tmdb"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/stremio"
	"golang.org/x/text/language"
)

const (
	FlagEnabled = "subtitle-translate-enabled"
	FlagFree    = "subtitle-translate-free"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.BoolFlag{Name: FlagEnabled, Usage: "offer AI-translated subtitle tracks (needs the ~tr mod in torrent-http-proxy)", EnvVar: "SUBTITLE_TRANSLATE_ENABLED"},
		cli.BoolFlag{Name: FlagFree, Usage: "offer AI translation to every viewer, not only paid tiers (deployments without claims-provider)", EnvVar: "SUBTITLE_TRANSLATE_FREE"},
	)
}

type Service struct {
	pg      *cs.PG
	enabled bool
	free    bool
}

func New(c *cli.Context, pg *cs.PG) *Service {
	return &Service{pg: pg, enabled: c.Bool(FlagEnabled), free: c.Bool(FlagFree)}
}

func (s *Service) TranslateEnabled() bool { return s != nil && s.enabled }
func (s *Service) FreeForAll() bool       { return s != nil && s.free }

// ResolvePreferred: the profile setting wins when it names a known
// language; otherwise the UI language, reduced to its base.
func ResolvePreferred(setting, uiLang string) string {
	if code := strings.TrimSpace(setting); code != "" && stremio.LanguageByCode(code) != nil {
		return code
	}
	t, err := language.Parse(uiLang)
	if err != nil {
		return ""
	}
	if b, conf := t.Base(); conf != language.No {
		return b.String()
	}
	return ""
}

func (s *Service) PreferredContentLang(ctx context.Context, user *auth.User, uiLang string) string {
	setting := ""
	if s != nil && s.pg != nil && user != nil && user.HasAuth() {
		if db := s.pg.Get(); db != nil {
			if data, err := models.GetUserStremioSettingsData(ctx, db, user.ID); err == nil && data != nil {
				setting = data.PreferredLanguage
			}
		}
	}
	return ResolvePreferred(setting, uiLang)
}

// IsAdultResource answers "may this resource get an AI subtitle track":
// NSFW resources never do (spec decision 13). Unknown resources (no
// metadata row yet) and DB errors read as not adult — the flag only
// ever removes the track, it never grants anything.
func (s *Service) IsAdultResource(ctx context.Context, resourceID string) bool {
	if s == nil || s.pg == nil || resourceID == "" {
		return false
	}
	db := s.pg.Get()
	if db == nil {
		return false
	}
	rm, err := models.GetResourceMetadataByResourceID(ctx, db, resourceID)
	if err != nil {
		log.WithError(err).WithField("resource", resourceID).Warn("failed to read resource metadata for subtitle gate")
		return false
	}
	return rm != nil && rm.IsAdult
}

// CastNames returns up to limit cast names from the TMDB credits stored
// by enrichment, for the translator's glossary. Best effort: empty on
// any miss.
func (s *Service) CastNames(ctx context.Context, videoID string, limit int) []string {
	if s == nil || s.pg == nil || !strings.HasPrefix(videoID, "tt") {
		return nil
	}
	db := s.pg.Get()
	if db == nil {
		return nil
	}
	info, err := tm.GetInfoByIMDBID(ctx, db, videoID)
	if err != nil || info == nil {
		return nil
	}
	return castNamesFromMetadata(info.Metadata, limit)
}

// castNameMaxRunes bounds a single glossary entry. The names ride on every
// translated subtitle URL as a `names=` query parameter, so the count alone
// does not bound the URL: TMDB credits are free text and one absurd entry
// is enough to push the request past what the proxy chain will carry. Runes,
// not bytes -- a 40-character Cyrillic or CJK name must survive whole rather
// than be cut mid-character.
const castNameMaxRunes = 40

// castNamesMaxEncodedBytes bounds the glossary in aggregate, which the
// per-name cap and the count do not: 30 names of 40 CJK runes each is
// 3600 bytes of UTF-8, and url.Values.Encode percent-encodes that to about
// 10.8 KB -- appended to an already-signed proxy URL. Nginx's default
// large_client_header_buffers line budget is 8 KB, so the AI <track> comes
// back 414 and the player reports subtitle-translate-error {code:414} with
// no shorter-glossary fallback. 1 KB encoded leaves the rest of the chain
// its room; a glossary is a hint, and the names that do not fit are the
// ones the credits ranked last.
const castNamesMaxEncodedBytes = 1024

func castNamesFromMetadata(md map[string]any, limit int) []string {
	credits, _ := md["credits"].(map[string]any)
	cast, _ := credits["cast"].([]any)
	var out []string
	for _, c := range cast {
		m, _ := c.(map[string]any)
		if name, _ := m["name"].(string); strings.TrimSpace(name) != "" {
			name = capRunes(strings.TrimSpace(name), castNameMaxRunes)
			// Measured on the joined value, the way TranslateURL sends it,
			// and the whole candidate list is not abandoned on the first
			// name that does not fit: a single long entry must not cut the
			// glossary short for the shorter names behind it.
			if len(url.QueryEscape(strings.Join(append(out, name), ","))) > castNamesMaxEncodedBytes {
				continue
			}
			// Measured on the joined value, the way TranslateURL sends it,
			// and the whole candidate list is not abandoned on the first
			// name that does not fit: a single long entry must not cut the
			// glossary short for the shorter names behind it.
			out = append(out, name)
		}
		if len(out) == limit {
			break
		}
	}
	return out
}

func capRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
