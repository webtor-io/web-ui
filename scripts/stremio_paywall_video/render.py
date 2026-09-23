"""Render the Stremio paywall clips: pub/stremio/paywall-<lang>.mp4.

A free viewer who clicks a stream in the Stremio addon that only Webtor's
servers could play is redirected to this clip (handlers/stremio/paywall.go).
Two screens, 12 s, 1280x720:

  1. "This stream plays through Webtor's servers" / "To watch it, you need a
     paid Webtor plan";
  2. "Start a free trial", "webtor.io/trial" with a QR code for a phone, and
     the small print about using the same email or Patreon account.

Every word comes from the stremio.paywall.* keys in locales/<lang>.json, one
clip per locale file. The clip carries no numbers — trial length, speed,
price live in the offer catalog, and the clip cannot follow them.

Usage, from the web-ui root (see docs/stremio.md, "The paywall clip"):

    python3 -m venv /tmp/paywall-venv
    /tmp/paywall-venv/bin/pip install -r scripts/stremio_paywall_video/requirements.txt
    /tmp/paywall-venv/bin/python scripts/stremio_paywall_video/render.py

Options: --lang ru (one language), --frames DIR (also save the two screens as
PNG there, for review). ffmpeg is taken from $FFMPEG (a command prefix,
default "ffmpeg"); with Docker and nothing installed:

    FFMPEG="docker run --rm -v $PWD:$PWD -w $PWD jrottenberg/ffmpeg:8-alpine" \\
        /tmp/paywall-venv/bin/python scripts/stremio_paywall_video/render.py

Inter (the site's font) is downloaded once from its pinned release and
checked against a digest; the copy embedded in assets/src/styles/inter.css is
an ASCII subset and cannot draw Cyrillic or most accented letters. Comfortaa
for the wordmark is the site's own embedded subset.
"""

import argparse
import base64
import hashlib
import io
import json
import os
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import urllib.request
from pathlib import Path

import qrcode
from fontTools.ttLib import TTFont
from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
CACHE = HERE / ".cache"
OUT_DIR = ROOT / "pub" / "stremio"

INTER_URL = "https://raw.githubusercontent.com/rsms/inter/v4.1/docs/font-files/InterVariable.ttf"
INTER_SHA256 = "4989b125924991b90d05b2d16e0e388c48f7d5bb8b30539bbf9c755278d0ccaf"

# The keys, in the order the source digest reads them. handlers/stremio's
# test recomputes the digest with the same order and format, so a changed
# translation that was not re-rendered fails there.
KEYS = ["title", "needsPlan", "trial", "open", "sameAccount"]
DEFAULT_LANG = "en"
SITE = "https://webtor.io"
SHORT = "webtor.io/trial"
# Must match handlers/trial PaywallUTM.
UTM = "utm_source=stremio&utm_medium=video&utm_campaign=paywall"
DIGEST_MARKER = "webtor-paywall-src:"

W, H = 1280, 720
S = 2  # everything but the QR code is drawn at 2x and scaled down
FPS = 25
SCREEN1 = 4.5  # seconds before the cross-fade
XFADE = 0.5
TOTAL = 12.0
FADE_IN = 0.3
MAX_BYTES = 600 * 1024

# tailwind.config.js -> colors.w (night theme)
BG = (0x0A, 0x0E, 0x1A)
TEXT = (0xF1, 0xF5, 0xF9)
SUB = (0x94, 0xA3, 0xB8)
PINK = (0xE8, 0x43, 0x93)
PURPLE_L = (0xA2, 0x9B, 0xFE)
LOGO_PINK = (0xF6, 0x70, 0xB3)  # assets/src/images/logo-night.svg
LOGO_DARK = (0x0F, 0x17, 0x2A)


def qr_url(lang: str) -> str:
    prefix = "" if lang == DEFAULT_LANG else "/" + lang
    return f"{SITE}{prefix}/trial?{UTM}"


def source_digest(texts: dict, lang: str) -> str:
    h = hashlib.sha256()
    for k in KEYS:
        h.update(f"stremio.paywall.{k}={texts[k]}\n".encode())
    h.update(f"qr={qr_url(lang)}\n".encode())
    return h.hexdigest()


# --- fonts -----------------------------------------------------------------

def inter_path() -> Path:
    p = CACHE / "InterVariable-4.1.ttf"
    if not p.exists() or hashlib.sha256(p.read_bytes()).hexdigest() != INTER_SHA256:
        CACHE.mkdir(parents=True, exist_ok=True)
        print(f"downloading {INTER_URL}", file=sys.stderr)
        with urllib.request.urlopen(INTER_URL, timeout=60) as r:
            data = r.read()
        got = hashlib.sha256(data).hexdigest()
        if got != INTER_SHA256:
            sys.exit(f"Inter digest mismatch: got {got}, want {INTER_SHA256}")
        p.write_bytes(data)
    return p


def comfortaa_bytes() -> bytes:
    """The site's wordmark font: base64 WOFF2 in comfortaa.css -> TTF."""
    css = (ROOT / "assets/src/styles/comfortaa.css").read_text()
    woff2 = base64.b64decode(re.search(r"base64,([A-Za-z0-9+/=]+)", css).group(1))
    f = TTFont(io.BytesIO(woff2))
    f.flavor = None
    out = io.BytesIO()
    f.save(out)
    return out.getvalue()


_INTER = None
_COMFORTAA = None


def inter(size: float, weight: int) -> ImageFont.FreeTypeFont:
    """Inter at size (1x px); optical size follows the pixel size."""
    f = ImageFont.truetype(str(_INTER), round(size * S))
    f.set_variation_by_axes([max(14, min(32, size)), weight])
    return f


def comfortaa(size: float) -> ImageFont.FreeTypeFont:
    return ImageFont.truetype(io.BytesIO(_COMFORTAA), round(size * S))


def check_glyphs(texts: dict, lang: str):
    """Fail on a character Inter cannot draw rather than render a box."""
    cmap = TTFont(str(_INTER)).getBestCmap()
    for k in KEYS:
        for ch in texts[k]:
            if ch not in " \u00a0" and ord(ch) not in cmap:
                sys.exit(f"{lang}: stremio.paywall.{k} has {ch!r} (U+{ord(ch):04X}), which Inter has no glyph for")


# --- drawing ---------------------------------------------------------------

def canvas() -> Image.Image:
    """Night background with the og-card glow (pub/og-card.png), off-centre up."""
    q = 8
    w, h = W * S // q, H * S // q
    glow = Image.new("RGBA", (w, h))
    px = glow.load()
    cx, cy, r = w / 2, h * 0.38, w * 0.5
    stops = [(0.0, (232, 67, 147, 40)), (0.35, (108, 92, 231, 20)), (0.75, (108, 92, 231, 0)), (1.0, (0, 0, 0, 0))]
    for y in range(h):
        for x in range(w):
            d = min(1.0, ((x - cx) ** 2 + (y - cy) ** 2) ** 0.5 / r)
            for (p0, c0), (p1, c1) in zip(stops, stops[1:]):
                if p0 <= d <= p1:
                    t = (d - p0) / (p1 - p0) if p1 > p0 else 0
                    px[x, y] = tuple(round(c0[i] + (c1[i] - c0[i]) * t) for i in range(4))
                    break
    im = Image.new("RGBA", (W * S, H * S), BG + (255,))
    im.alpha_composite(glow.resize((W * S, H * S), Image.BICUBIC))
    return im


def logo(size: int) -> Image.Image:
    """assets/src/images/logo-night.svg (170x170 viewBox)."""
    k = size / 170
    im = Image.new("RGBA", (size, size))
    d = ImageDraw.Draw(im)
    outer = [(56, 0), (0, 0), (0, 170), (170, 170), (170, 0), (113, 0), (113, 89), (156, 89), (85, 156), (14, 89), (56, 89)]
    arrow = [(56, 0), (170, 0), (113, 0), (113, 89), (156, 89), (85, 156), (14, 89), (56, 89)]
    d.polygon([(x * k, y * k) for x, y in outer], fill=LOGO_PINK)
    d.polygon([(x * k, y * k) for x, y in arrow], fill=LOGO_DARK)
    return im


def lockup(im: Image.Image, x: float, top: float, icon_px: int, word_px: int, centred: bool):
    """Logo + "web" + pink "tor"; x is the left edge, or the centre."""
    d = ImageDraw.Draw(im)
    wm = comfortaa(word_px)
    icon = logo(icon_px * S)
    gap = icon_px * 0.34 * S
    w_web = d.textlength("web", font=wm)
    w_tor = d.textlength("tor", font=wm)
    group = icon.width + gap + w_web + w_tor
    x0 = x * S - group / 2 if centred else x * S
    y0 = top * S
    im.alpha_composite(icon, (round(x0), round(y0)))
    xh = wm.getbbox("w")  # x-height box: centre it on the icon
    base_y = y0 + icon.height / 2 - (xh[1] + xh[3]) / 2
    tx = x0 + icon.width + gap
    d.text((tx, base_y), "web", font=wm, fill=TEXT)
    d.text((tx + w_web, base_y), "tor", font=wm, fill=PINK)


def tokens(text: str) -> list:
    """Words, with a word of one or two letters kept on the line of the word
    after it (Russian, Polish and Czech typesetting: no "в", "z", "a" left
    hanging at a line end), and a lone dash kept with the word before it."""
    out = []
    for w in text.split(" "):
        if out and w in ("—", "–"):
            out[-1] += " " + w
        elif out and len(out[-1]) <= 2 and out[-1].isalpha():
            out[-1] += " " + w
        else:
            out.append(w)
    return out


def wrap(d: ImageDraw.ImageDraw, text: str, font, max_w: float, n: int):
    """Split text into at most n lines no wider than max_w, as evenly as
    possible (a headline of one long line and one word reads badly on a
    TV); None when it does not fit."""
    words = tokens(text)
    width = lambda ws: d.textlength(" ".join(ws), font=font)
    if width(words) <= max_w:
        return [" ".join(words)]
    best = None
    if n >= 2:
        for i in range(1, len(words)):
            a, b = words[:i], words[i:]
            if width(a) > max_w:
                break
            if width(b) <= max_w:
                cost = max(width(a), width(b))
                if best is None or cost < best[0]:
                    best = (cost, [" ".join(a), " ".join(b)])
    return best[1] if best else None


def fit(text: str, weight: int, max_w: float, max_lines: int, start: int, smallest: int, what: str):
    """The largest size from start down to smallest that fits max_lines."""
    assert max_lines <= 2
    d = ImageDraw.Draw(Image.new("L", (1, 1)))
    for size in range(start, smallest - 1, -1):
        f = inter(size, weight)
        lines = wrap(d, text, f, max_w * S, max_lines)
        if lines:
            return f, size, lines
    sys.exit(f"{what}: {text!r} does not fit {max_lines} line(s) of {max_w}px even at {smallest}px")


def gradient(size, c0, c1) -> Image.Image:
    """135deg linear gradient, like .gradient-text."""
    w, h = size
    g = Image.new("RGB", (w, h))
    px = g.load()
    span = max(1, w + h)
    for y in range(h):
        for x in range(w):
            t = (x + y) / span
            px[x, y] = tuple(round(c0[i] + (c1[i] - c0[i]) * t) for i in range(3))
    return g


def text_block(im, lines, font, size, x, top, fill, align="center", width=None, lh=1.22, grad=None) -> float:
    """Draw lines from top (1x px); returns the bottom (1x px)."""
    d = ImageDraw.Draw(im)
    step = size * lh * S
    y = top * S
    asc, desc = font.getmetrics()
    for line in lines:
        tw = d.textlength(line, font=font)
        if align == "center":
            lx = x * S - tw / 2
        else:
            lx = x * S
        if grad is None:
            d.text((lx, y), line, font=font, fill=fill)
        else:
            mask = Image.new("L", im.size)
            ImageDraw.Draw(mask).text((lx, y), line, font=font, fill=255)
            bbox = mask.getbbox()
            layer = Image.new("RGBA", im.size)
            layer.paste(gradient((bbox[2] - bbox[0], bbox[3] - bbox[1]), *grad), bbox[:2])
            layer.putalpha(mask)
            im.alpha_composite(layer)
        y += step
    return (y - step + asc + desc) / S


def block_height(n, size, font, lh=1.22) -> float:
    asc, desc = font.getmetrics()
    return ((n - 1) * size * lh * S + asc + desc) / S


def screen1(texts) -> Image.Image:
    im = canvas()
    width = W - 2 * 112
    tf, ts, tl = fit(texts["title"], 800, width, 2, 62, 40, "title")
    nf, ns, nl = fit(texts["needsPlan"], 800, width, 2, 50, 34, "needsPlan")
    icon, word = 64, 76
    gap1, gap2 = 76, 26
    total = icon + gap1 + block_height(len(tl), ts, tf) + gap2 + block_height(len(nl), ns, nf)
    top = (H - total) / 2 - 10
    lockup(im, W / 2, top, icon, word, centred=True)
    y = top + icon + gap1
    y = text_block(im, tl, tf, ts, W / 2, y, TEXT)
    text_block(im, nl, nf, ns, W / 2, y + gap2, None, grad=(PINK, PURPLE_L))
    return im.convert("RGB").resize((W, H), Image.LANCZOS)


def qr_card(url: str, target: int) -> Image.Image:
    """White card with the QR code, drawn at 1x with whole-pixel modules so
    no module edge is resampled."""
    q = qrcode.QRCode(error_correction=qrcode.constants.ERROR_CORRECT_M, border=0, box_size=1)
    q.add_data(url)
    q.make(fit=True)
    m = q.get_matrix()
    n = len(m)
    quiet = 4  # the spec's quiet zone, in modules
    px = max(1, target // (n + 2 * quiet))
    side = px * (n + 2 * quiet)
    card = Image.new("RGBA", (side, side), (0, 0, 0, 0))
    d = ImageDraw.Draw(card)
    d.rounded_rectangle([0, 0, side - 1, side - 1], radius=px * 2, fill=(255, 255, 255, 255))
    for r, row in enumerate(m):
        for c, on in enumerate(row):
            if on:
                x0, y0 = (c + quiet) * px, (r + quiet) * px
                d.rectangle([x0, y0, x0 + px - 1, y0 + px - 1], fill=BG + (255,))
    return card


def screen2(texts, lang) -> Image.Image:
    im = canvas()
    margin = 96
    card = qr_card(qr_url(lang), 344)
    card_x = W - margin - card.width
    col_w = card_x - margin - 64
    lockup_top, lockup_icon = 72, 44

    nf_note, ns_note, nl_note = fit(texts["sameAccount"], 500, W - 2 * margin, 2, 27, 21, "sameAccount")
    note_h = block_height(len(nl_note), ns_note, nf_note, lh=1.35)
    note_top = H - 58 - note_h
    # The QR code and the column beside it sit in the middle of what is
    # left between the wordmark and the small print.
    mid = (lockup_top + lockup_icon + note_top) / 2
    card_y = round(mid - card.height / 2)

    tf, ts, tl = fit(texts["trial"], 800, col_w, 2, 58, 38, "trial")
    of, os_, ol = fit(texts["open"], 500, col_w, 2, 30, 22, "open")
    uf, us, ul = fit(SHORT, 800, col_w, 1, 72, 48, "short link")
    gap_t, gap_u = 28, 10
    left_h = block_height(len(tl), ts, tf) + gap_t + block_height(len(ol), os_, of) + gap_u + block_height(1, us, uf)
    y = mid - left_h / 2
    y = max(y, lockup_top + lockup_icon + 36)
    lockup(im, margin, lockup_top, lockup_icon, 52, centred=False)
    y = text_block(im, tl, tf, ts, margin, y, None, align="left", grad=(PINK, PURPLE_L))
    y = text_block(im, ol, of, os_, margin, y + gap_t, SUB, align="left")
    # "webtor.io/" white, "trial" pink, like the wordmark.
    d = ImageDraw.Draw(im)
    uy = (y + gap_u) * S
    head, tail = SHORT.rsplit("/", 1)
    d.text((margin * S, uy), head + "/", font=uf, fill=TEXT)
    d.text((margin * S + d.textlength(head + "/", font=uf), uy), tail, font=uf, fill=PINK)
    if note_top < card_y + card.height + 24:
        sys.exit(f"{lang}: the small print would overlap the QR code")
    text_block(im, nl_note, nf_note, ns_note, margin, note_top, SUB, align="left", lh=1.35)

    out = im.convert("RGB").resize((W, H), Image.LANCZOS)
    out.paste(card, (card_x, card_y), card)
    return out


# --- encoding --------------------------------------------------------------

def ffmpeg_cmd() -> list:
    return shlex.split(os.environ.get("FFMPEG", "ffmpeg"))


def encode(s1: Path, s2: Path, out: Path, digest: str, lang: str):
    graph = (
        f"[0:v]fps={FPS},format=gbrp[a];[1:v]fps={FPS},format=gbrp[b];"
        f"[a][b]xfade=transition=fade:duration={XFADE}:offset={SCREEN1},"
        f"fade=t=in:st=0:d={FADE_IN},"
        "scale=out_color_matrix=bt709:out_range=tv,format=yuv420p,"
        "setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709:range=tv,setsar=1[v]"
    )
    tmp = out.with_suffix(".tmp.mp4")
    cmd = ffmpeg_cmd() + [
        "-hide_banner", "-loglevel", "error", "-y",
        "-loop", "1", "-framerate", str(FPS), "-t", str(SCREEN1 + XFADE), "-i", str(s1),
        "-loop", "1", "-framerate", str(FPS), "-t", str(TOTAL - SCREEN1), "-i", str(s2),
        "-f", "lavfi", "-t", str(TOTAL), "-i", "anullsrc=channel_layout=stereo:sample_rate=48000",
        "-filter_complex", graph, "-map", "[v]", "-map", "2:a",
        # H.264 High@3.1 yuv420p and AAC-LC: what ExoPlayer (Android TV),
        # libmpv (desktop) and AVPlayer (Apple TV, iOS) all decode in hardware.
        "-c:v", "libx264", "-profile:v", "high", "-level:v", "3.1", "-pix_fmt", "yuv420p",
        # aq-mode=3 spends more bits on dark flat areas: the glow is a dark
        # gradient, and 8-bit video bands there. Dithering it away instead
        # quadrupled the size.
        "-preset", "veryslow", "-tune", "stillimage", "-crf", "20", "-x264-params", "aq-mode=3",
        # A keyframe every 5 s: nothing seeks in a 12 s still, and each
        # keyframe of this picture costs ~40 KB.
        "-g", str(FPS * 5), "-keyint_min", str(FPS),
        "-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-color_range", "tv",
        "-c:a", "aac", "-b:a", "48k", "-ac", "2", "-ar", "48000",
        "-t", str(TOTAL), "-shortest",
        "-map_metadata", "-1", "-fflags", "+bitexact", "-flags:v", "+bitexact", "-flags:a", "+bitexact",
        "-metadata", f"comment={DIGEST_MARKER}{digest}",
        "-metadata:s:a:0", f"language={lang_639_2(lang)}",
        # moov first: the player can start before the whole file is in.
        "-movflags", "+faststart",
        str(tmp),
    ]
    subprocess.run(cmd, check=True)
    size = tmp.stat().st_size
    if size > MAX_BYTES:
        tmp.unlink()
        sys.exit(f"{out.name}: {size} bytes, over the {MAX_BYTES} budget")
    tmp.replace(out)
    return size


def lang_639_2(lang: str) -> str:
    return {"en": "eng", "ru": "rus", "es": "spa", "de": "deu", "fr": "fra", "pt": "por", "it": "ita",
            "pl": "pol", "tr": "tur", "nl": "nld", "cs": "ces"}.get(lang, "und")


def locales() -> dict:
    out = {}
    for p in sorted((ROOT / "locales").glob("??.json")):
        d = json.loads(p.read_text(encoding="utf-8"))
        texts = {}
        for k in KEYS:
            v = d.get("stremio.paywall." + k)
            if not isinstance(v, str) or not v.strip():
                sys.exit(f"{p.name}: stremio.paywall.{k} is missing")
            texts[k] = v
        out[p.stem] = texts
    return out


def main():
    global _INTER, _COMFORTAA
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--lang", action="append", help="render only this language (repeatable)")
    ap.add_argument("--frames", type=Path, help="also save each screen as <lang>-1.png / <lang>-2.png here")
    args = ap.parse_args()
    if shutil.which(ffmpeg_cmd()[0]) is None:
        sys.exit(f"{ffmpeg_cmd()[0]} not found; set FFMPEG (see the docstring)")
    _INTER = inter_path()
    _COMFORTAA = comfortaa_bytes()
    all_texts = locales()
    langs = args.lang or list(all_texts)
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    if args.frames:
        args.frames.mkdir(parents=True, exist_ok=True)
    # Frames go under the repository (.cache is ignored), so a dockerised
    # ffmpeg that mounts the working tree sees them at the same path.
    CACHE.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=CACHE) as tmp:
        for lang in langs:
            texts = all_texts[lang]
            check_glyphs(texts, lang)
            s1, s2 = Path(tmp) / f"{lang}-1.png", Path(tmp) / f"{lang}-2.png"
            screen1(texts).save(s1)
            screen2(texts, lang).save(s2)
            if args.frames:
                shutil.copy(s1, args.frames / s1.name)
                shutil.copy(s2, args.frames / s2.name)
            out = OUT_DIR / f"paywall-{lang}.mp4"
            size = encode(s1, s2, out, source_digest(texts, lang), lang)
            print(f"{out.relative_to(ROOT)}: {size} bytes")


if __name__ == "__main__":
    main()
