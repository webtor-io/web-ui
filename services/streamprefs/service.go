package streamprefs

import (
	"context"
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

func castNamesFromMetadata(md map[string]any, limit int) []string {
	credits, _ := md["credits"].(map[string]any)
	cast, _ := credits["cast"].([]any)
	var out []string
	for _, c := range cast {
		m, _ := c.(map[string]any)
		if name, _ := m["name"].(string); strings.TrimSpace(name) != "" {
			out = append(out, strings.TrimSpace(name))
		}
		if len(out) == limit {
			break
		}
	}
	return out
}
