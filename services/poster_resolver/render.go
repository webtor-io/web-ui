package poster_resolver

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/pkg/errors"
)

// jpegQuality matches the existing poster/og handlers — 85 is a good
// trade-off between size and visible artefacts for photographic
// posters and HLS-decoded video frames alike.
const jpegQuality = 85

// jpegQualityBlur is reduced for adult-blur output — heavy Gaussian
// kills high-frequency content anyway, so the encoder has nothing to
// preserve. Drops payload to ~5-10 KB for a 240px card and ~20-30 KB
// for the 1200x630 OG canvas (vs ~30 KB / ~150 KB unblurred).
const jpegQualityBlur = 60

// OG canvas dimensions. 1200x630 is the cross-platform safe target —
// Telegram, Twitter (card=summary_large_image), Facebook and iMessage
// all render this aspect ratio cleanly. The blur/darken pair turns the
// source into a stained-glass background behind a centred foreground
// fit — same look across IMDb posters, thumbnails and brand defaults.
const (
	ogCanvasWidth     = 1200
	ogCanvasHeight    = 630
	ogCanvasBlurSigma = 30.0
	ogCanvasDarken    = -25.0
)

// adultBlurRatio scales the Gaussian-blur sigma with image dimension
// so a 240px card and a 1200px OG canvas look equally censored.
// imaging.Blur takes sigma in PIXELS — a fixed value would over-blur
// thumbnails and under-blur large posters. 5% of the smallest side
// hits the "shapes visible, detail killed" sweet spot across all
// sizes we render.
const adultBlurRatio = 0.05

// adultBlurMin is the floor sigma so tiny images (24x24 favicon-style
// edge cases) still get a meaningful blur.
const adultBlurMin = 5.0

// adultDarken slightly darkens the blurred output so a thin "18+"
// badge rendered on top by the client stays legible across bright
// sources (snow / explosions / bright skin tones).
const adultDarken = -10.0

// Mode is what the request asked the service to produce. Parsed from
// the `file` route parameter via ParseFileMode; the resolver may
// force Blur=true at miss time when resource_metadata.is_adult=true.
type Mode struct {
	OG    bool // true → render in 1200x630 OG canvas
	Width int  // resize-mode target width (only used when OG=false)
	Blur  bool // true → heavy Gaussian + darken (adult/sport)
}

// CacheTag is a short stable string for the cache key. Two requests
// with the same Mode share the same cached object for the same source.
// Blur is encoded as a `blur-` prefix so unblurred and blurred renders
// of the same source live as distinct objects — flipping is_adult on
// a resource doesn't invalidate the non-adult cache.
func (m Mode) CacheTag() string {
	tag := "og"
	if !m.OG {
		tag = strconv.Itoa(m.Width)
	}
	if m.Blur {
		tag = "blur-" + tag
	}
	return tag
}

// ParseFileMode unpacks the `file` route parameter into a Mode. Accepts
//
//	og.jpg            → OG canvas
//	<width>.jpg       → resize to <width>
//
// width is bounded (32..1600) so a hostile caller can't burn Lanczos
// CPU asking for 99999.jpg.
func ParseFileMode(file string) (Mode, error) {
	parts := strings.SplitN(file, ".", 2)
	if len(parts) != 2 || parts[1] != "jpg" {
		return Mode{}, errors.Errorf("bad poster file %q (want <stem>.jpg)", file)
	}
	if parts[0] == "og" {
		return Mode{OG: true}, nil
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil || w < 32 || w > 1600 {
		return Mode{}, errors.Errorf("bad poster width %q", parts[0])
	}
	return Mode{Width: w}, nil
}

// render produces the JPEG bytes for one (source-image, mode) pair.
// Pure function — no I/O, deterministic, safe to share results
// across callers.
//
// Blur is applied AFTER OG-canvas composition / resize so the cached
// payload is already at the final display dimensions. The 18+ badge
// itself is drawn client-side via CSS overlay (template emits the
// data-adult attribute), not baked into the image — keeps the
// rendering pipeline free of font/glyph dependencies.
func render(src image.Image, mode Mode) (*bytes.Buffer, error) {
	var out image.Image
	if mode.OG {
		out = composeOGCanvas(src)
	} else {
		out = imaging.Resize(src, mode.Width, 0, imaging.Lanczos)
	}
	quality := jpegQuality
	if mode.Blur {
		out = imaging.Blur(out, adultBlurSigmaFor(out))
		out = imaging.AdjustBrightness(out, adultDarken)
		quality = jpegQualityBlur
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: quality}); err != nil {
		return nil, errors.Wrap(err, "failed to encode jpeg")
	}
	return &buf, nil
}

// adultBlurSigmaFor scales sigma to the image's smaller dimension so
// the censor effect is visually constant across thumbnail and OG
// canvas. Floored at adultBlurMin to keep meaningful blur on tiny
// edge-case images.
func adultBlurSigmaFor(img image.Image) float64 {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	minSide := w
	if h < minSide {
		minSide = h
	}
	sigma := float64(minSide) * adultBlurRatio
	if sigma < adultBlurMin {
		sigma = adultBlurMin
	}
	return sigma
}

// composeOGCanvas wraps a source image in the 1200x630 OG canvas with
// a blurred/darkened "stained-glass" fill behind a centred foreground.
// Vertical posters cover the full 630px height, landscape sources
// cover the full 1200px width — the foreground sits on top so the
// blurred background is barely visible for landscape inputs.
func composeOGCanvas(src image.Image) image.Image {
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()
	var fg image.Image
	if srcH >= srcW {
		fg = imaging.Resize(src, 0, ogCanvasHeight, imaging.Lanczos)
	} else {
		fg = imaging.Resize(src, ogCanvasWidth, 0, imaging.Lanczos)
	}
	bg := imaging.Fill(src, ogCanvasWidth, ogCanvasHeight, imaging.Center, imaging.Linear)
	bg = imaging.Blur(bg, ogCanvasBlurSigma)
	bg = imaging.AdjustBrightness(bg, ogCanvasDarken)
	x := (ogCanvasWidth - fg.Bounds().Dx()) / 2
	y := (ogCanvasHeight - fg.Bounds().Dy()) / 2
	return imaging.Overlay(bg, fg, image.Pt(x, y), 1.0)
}

// cacheKey is the S3 object path for a (source-content, mode) pair.
// Resources that resolve to the same source share the same key — this
// is where cross-resource dedup happens.
func cacheKey(src *Source, mode Mode) string {
	return fmt.Sprintf("poster/%s/%s.jpg", src.CacheID, mode.CacheTag())
}
