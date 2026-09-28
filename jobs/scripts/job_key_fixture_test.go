package scripts

import (
	"time"

	"golang.org/x/text/language"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/web"
)

// jobKeyAt is the instant every job-key fixture is computed at.
var jobKeyAt = time.Date(2026, 9, 27, 21, 37, 0, 0, time.UTC)

// jobKeyContext is an anonymous visitor, as the key sees one.
func jobKeyContext() *web.Context {
	return &web.Context{ApiClaims: &api.Claims{Role: "free", SessionID: "sess-1"}, Lang: "en"}
}

func jobKeyVSUD() *models.VideoStreamUserData {
	v := models.NewVideoStreamUserData("08ada5a7a6183aae1e09d831df6748d566095a10", "item-1", &models.StreamSettings{})
	v.AcceptLangTags = []language.Tag{language.English, language.Russian}
	return v
}

func jobKeyEmbedSettings() *models.EmbedSettings {
	return &models.EmbedSettings{Magnet: "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10", Path: "Sintel/Sintel.mp4", Referer: "https://example.com/page"}
}
