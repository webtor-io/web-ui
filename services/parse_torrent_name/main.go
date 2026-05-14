package parsetorrentname

import (
	"regexp"
	"strconv"
	"strings"
)

// adultStudioRe is a compiled anchored form of adultStudioAlternation,
// used by the Website→Studio promotion fixup in Parse.
var adultStudioRe = regexp.MustCompile(`(?i)^(?:` + adultStudioAlternation + `)$`)

// qualityTransformer canonicalises Quality field values. The regex
// alternation matches many surface forms ("WEB-DL", "WEBDL",
// "WEB-DLRip", "BluRay", "BLURAY", "DvDScr", "telesync", ...); the
// stored value is the canonical token from this table so downstream
// consumers don't branch on case/punctuation variants.
//
// Keys are case-folded by MapTransformer at construction time; write
// them lowercase here for readability. Values are the canonical form.
//
// "PPV"-prefixed broadcast forms ("PPV.HDTV", "PPV WEB-DL") fold
// down to the base source — wrestling/sports PPV is a delivery
// channel, not a quality tier. The Sport flag (FieldTypeSport)
// already carries the broadcast context.
// codecTransformer canonicalises Codec field values to lowercase
// tokens regardless of source casing. h.264/H264/AVC all describe
// the same codec — store "x264". HEVC/H265 → "x265". The explicit
// "X264"/"XViD"/"xvid" entries exist so MapTransformer's case-fold
// lookup turns mixed-case source forms into the canonical lowercase
// values (entries with the same key:value are kept for documentation).
var codecTransformer = NewMapTransformer(map[string]string{
	"avc":   "x264",
	"h264":  "x264",
	"h.264": "x264",
	"hevc":  "x265",
	"h265":  "x265",
	"h.265": "x265",
	"x264":  "x264",
	"x265":  "x265",
	"xvid":  "xvid",
	"divx":  "divx",
	"av1":   "av1",
})

var qualityTransformer = NewMapTransformer(map[string]string{
	// Disc-based rips
	"bd":     "BluRay",
	"bluray": "BluRay",
	"bdrip":  "BDRip",
	"brrip":  "BRRip",
	"dvdrip": "DVDRip",
	"dvdscr": "DVDScr",

	// Internet/streaming sources
	"web-dl":    "WEB-DL",
	"webdl":     "WEB-DL",
	"web-dlrip": "WEB-DLRip",
	"webdlrip":  "WEB-DLRip",
	"webrip":    "WEBRip",
	"wbbrip":    "WEBRip",
	"hdrip":     "HDRip",

	// Off-air capture (PPV-prefix forms now go through FieldTypePPV)
	"hdtv": "HDTV",
	"pdtv": "PDTV",

	// Other physical sources
	"satrip": "SATRip",
	"tvrip":  "TVRip",
	"camrip": "CamRip",

	// Cam / telesync (in-theatre recordings)
	"cam":      "CAM",
	"hdcam":    "HDCAM",
	"ts":       "TS",
	"hdts":     "HDTS",
	"hd-ts":    "HDTS",
	"telesync": "TS",
})

// Release-quality keywords, split into two groups so the Episode
// `.NN.<Quality>` anchor (Russian DVDRip pack convention) can opt into
// the SAFE subset while the Quality field-parser keeps the full list.
//
// qualityRipForms — encode-from-source labels (DVDRip, BluRay, WEB-DL,
// SATRip, etc.). These never appear after an event/sport id in real
// torrent names, so they're safe to anchor episode digits against.
//
// qualityBroadcastForms — broadcast/cam labels (HDTV, PPV.HDTV, CAM,
// TS, Telesync). PPV events ("UFC.179.PPV.HDTV") put the event id
// right before these markers, so anchoring `.NN.<broadcast>` would
// false-fire episode=179 — broadcast forms are deliberately excluded
// from the episode anchor.
//
// qualityAlternation is the union, used by the Quality field-parser.
// Always referenced inside `(?i)\b(...)\b` or `(?i)(?:...)` to keep
// case-insensitivity + word boundaries explicit at each call site.
const (
	// `PPV` prefix is intentionally dropped from these alternations —
	// it's its own field now (FieldTypePPV) so Quality keeps a clean
	// source-tier value. See ppvMatcher below.
	qualityRipForms       = `DVDRip|DVDRIP|BluRay|B[DR]Rip|WEB-?DL(?:Rip)?|HDRip|W[EB]BRip|CamRip|DvDScr|SATRip|TVRip`
	qualityBroadcastForms = `[HP]DTV|(?:HD)?CAM|(?:HD-?)?TS|telesync`
	qualityAlternation    = qualityRipForms + `|` + qualityBroadcastForms

	// adultStudioAlternation — curated adult-studio / cam-site names
	// from ai_enrich.query telemetry. Shared by adultMatcher (transient
	// flag) and the FieldTypeStudio parser (extracts the name into
	// Studio so the title parser doesn't keep the prefix). Add new
	// names here in lowercase; the `(?i)` modifier handles mixed-case.
	//
	// Multi-word brands embed `\.?` between tokens so both the dotted
	// form ("Facial.Abuse") and the concatenated form ("FacialAbuse")
	// match — the parser doesn't strip dots before field matching.
	adultStudioAlternation = `blackedraw|blacked|brazzers|naughtyamerica|mylf|milfy|mylfx|hegre|onlyfans|only[\s.-]+fans|manyvids|pornstarwife|wowgirls|spankmonster|momswapped|latinpapixxl|latinpapi|allover30|gilfaf|edgedandbound|maturenl|mofos|ersties|hgshequ|hhd800|fakehub|bangbros|realitykings|teamskeet|atkgalleria|atkhairy|czechcasting|fc2ppv|heyzo|10musume|1pondo|s-cute|stickam|voyeur-russian|julesjordan|nubilesporn|exploitedcollegegirls|kink\.com|milflicious|wankzvr|tushy|deeper\.com|vixen\.com|strippers4k|rkprime|backroomcastingcouch|angelslove|beautyangels|cockyboys|facial\.?abuse|ghetto\.?gaggers|pure\.?taboo|enature|family\.?therapy\.?xxx|slr\s+originals|slroriginals|color[\s.-]?climax|1by[\s.-]?day|tamedteens|legalporno|mtcang|madoubt|argentinacasting|defloration|hookuphotshot|vrlatina|pornolab|blacksonblondes|sexuallybroken|faketaxi|adorable[\s.-]?teens|missax|naturistin|xxxviciosaszt|prime[\s.-]?revolution|stripchat|fansly\.com|sandra[\s.-]?flame|youngperps|momcomesfirst|mollyredwolf|tonightsgirlfriend|migoto[\s.-]?vr|eporner\.com|evil[\s.-]?angel|virtual[\s.-]?taboo|kinkvr|slr[\s_-]+vr[a-z]+|bonkge|cameel`

	// kindAlternation — anime release-segment tags. Shared by the Kind
	// field-parser (captures the tag word) and the Episode anchor that
	// extracts the digit trailing the tag ("<show> - ONA 01"). Same
	// reuse pattern as qualityAlternation.
	kindAlternation = `ONA|OVA|OAD|NCOP|NCED`
)

var fieldParsers = FieldParsers{
	{FieldTypeExtended, NewRegexpMatcher(`(?i)\b((EXTENDED(?:.CUT)?))\b`), nil},
	{FieldTypeHardcoded, NewRegexpMatcher(`(?i)\b((HC))\b`), nil},
	{FieldTypeProper, NewRegexpMatcher(`(?i)\b((PROPER))\b`), nil},
	{FieldTypeRepack, NewRegexpMatcher(`(?i)\b((REPACK))\b`), nil},
	{FieldTypeWidescreen, NewRegexpMatcher(`(?i)\b((WS))\b`), nil},
	{FieldTypeUnrated, NewRegexpMatcher(`(?i)\b((UNRATED))\b`), nil},
	{FieldType3D, NewRegexpMatcher(`(?i)\b((3D))\b`), nil},
	{FieldTypeAVC, NewRegexpMatcher(`(?i)\b((AVC))\b`), nil},
	{FieldTypeDubbing, NewRegexpMatcher(`(?i)\b(([ADM]?VO|DUB))\b`), nil},
	{FieldTypeSplitScenes, NewRegexpMatcher(`(?i)\b((SPLIT.?SCENES))\b`), nil},
	// Preview-clip marker. Real release torrents commonly bundle a 30-60s
	// "sample" preview file (e.g. "Movie.2015.sample.mkv", "1 Min Sample.mkv",
	// "Movie (Sample).mkv"). Downstream enricher drops these from the
	// movies/series list when the same torrent carries a non-sample file —
	// otherwise a single torrent produces two movies (the real film and the
	// preview), each calling AI/TMDB independently.
	{FieldTypeSample, NewRegexpMatcher(`(?i)\b((sample))\b`), nil},
	// FieldTypeAdult lives outside the fieldParsers slice — see
	// adultMatcher below. Detected via TransientFieldParser so the
	// matched keyword is reported (Adult=true) but its span is NOT
	// consumed, otherwise "Bang My Tranny Ass" yields Title="Bang My"
	// because `tranny` sits mid-string.
	// PPV (Pay-Per-View) — wrestling/UFC/boxing event-delivery marker.
	// Non-transient: consumes the span so the keyword doesn't leak
	// into Title/Extra (different from Adult/Sport which keep the
	// matched word visible because their tokens are usually part of
	// the show title — "Brazzers Scene", "NBA 2026..."). "PPV" is
	// always a standalone tag, never the title itself.
	{FieldTypePPV, NewRegexpMatcher(`(?i)\b((PPV))\b`), nil},
	{FieldTypeSize, NewRegexpMatcher(`(?i)\b((\d+(?:\.\d+)?(?:GB|MB)))\b`), nil},
	// Quality. Second alternative handles the BD-prefix combined
	// marker "BD1080p" / "BD2160p" — common in anime fansub releases.
	// Captures just the "BD" span (the resolution part is left for
	// FieldTypeResolution below). The MapTransformer below canonical-
	// ises every Quality value to a stable token so downstream
	// consumers don't have to handle case/format variants.
	{FieldTypeQuality, NewRegexpMatcher(
		`(?i)\b((` + qualityAlternation + `))\b`,
		`(?i)\b((BD))(?:[0-9]{3,4}p|[248][Kk])\b`,
	), qualityTransformer},
	// Resolution. First alternative handles the BD/UHD-prefix anime
	// release convention: "BD1080p", "UHD2160p". The standard
	// `\b\d{3,4}p\b` form misses these because there's no word/non-word
	// boundary between "D" and "1" (both word chars), so "1080p" inside
	// "BD1080p" leaks. Inner capture stays "1080p" so Resolution.Content
	// is consistent regardless of source prefix; the outer span eats
	// the "BD"/"UHD" prefix too, keeping it out of Title/Extra.
	{FieldTypeResolution, NewRegexpMatcher(
		`(?i)\b((?:BD|UHD|HD)([0-9]{3,4}p|[248][Kk]))\b`,
		`\b(([0-9]{3,4}p|[248][Kk]))\b`,
	), NewLowercaseTransformer()},
	{FieldTypeBitrate, NewRegexpMatcher(`(?i)\b(([0-9]+[KMGT]bps))\b`), nil},
	// ColorDepth covers SDR/HDR variants plus the "N-bit" / "Nbit"
	// suffix common on anime encodes ("10bit", "10-bit", "8-bit").
	{FieldTypeColorDepth, NewRegexpMatcher(`(?i)(([HS]DR(?:[0-9]{0,2})?\+?|(?:8|10|12)[\s-]?bit))`), nil},
	// Codec — added HEVC (H.265 alias) and AV1.
	// Codec. First two alternatives consume the combined "x265.HEVC" /
	// "x264.AVC" / "HEVC.x264" forms so the alias half doesn't leak
	// into Extra (test 159 = Sicario "...x265.HEVC-PSA.mkv" used to
	// stash HEVC in Extra). Inner capture stays the canonical name.
	// MapTransformer normalises h.264 / H265 / HEVC / AVC variants
	// to "x264" / "x265" so downstream consumers see one stable token.
	{FieldTypeCodec, NewRegexpMatcher(
		`(?i)\b(([hx]\.?26[45])[\s.-]?(?:HEVC|AVC))\b`,
		`(?i)\b((?:HEVC|AVC)[\s.-]?([hx]\.?26[45]))\b`,
		`(?i)\b((xvid|divx|[hx]\.?26[45]|hevc|av1))\b`,
	), codecTransformer},
	// Audio — extended with Atmos/TrueHD/EAC3/FLAC/DDP and channel
	// counts (7.1/5.1/2CH/6ch) so newer encodes don't leak these
	// markers into Title or Extra. Order: longer / more specific
	// alternations first so a "DTS-HD MA" match doesn't get cut
	// short by the bare "DTS" alternative.
	{FieldTypeAudio, NewRegexpMatcher(`(?i)\b((DTS[\s.-]?HD(?:[\s.-]?MA)?|TrueHD|Atmos|E[\s.-]?AC3|FLAC|MP3|DDP[\s.]?[57]\.[01]|DDP|DD\+?5\.?1|DD5\.?1|Dual[\- ]Audio|LiNE|DTS|AAC[.-]LC|AAC(?:\.?2\.0)?|AC3(?:(?:[\s-]+)?\.?5\.1)?|[5-9]\.1|[5-9]ch|2CH))\b`), nil},
	{FieldTypeWebsite, NewRegexpMatcher(`^((www\.[a-zA-Z0-9][a-zA-Z0-9-]{1,61}[a-zA-Z0-9]\.[a-zA-Z]{2,}))`, `^(\[ ?([^\]]+?) ?\])`), nil},
	// Scene-release date. Runs BEFORE Year + Episode so the year-shaped
	// trailing group in "DD.MM.YYYY" doesn't get split between Year and
	// Date, and so the 2-digit groups in "YY.MM.DD" don't get eaten by
	// the Episode `\d{2,3}` patterns.
	//
	// Adult releases dominate this pattern (Blacked.18.03.21.Lana,
	// hegre 23 08 22 allie, Studio - Title (27.02.2026)). The 2-digit
	// year is unambiguous because `\b` denies matches inside 4-digit
	// year tokens (so "2014 11 10" wrestling broadcast dates stay
	// untouched and reach the Year/Episode pipeline).
	//
	// The DateTransformer normalizes all three input shapes to
	// "YYYY-MM-DD" and validates month/day ranges; out-of-range
	// triplets fall through unmatched.
	{FieldTypeDate, NewRegexpMatcher(
		// YYYY-MM-DD / "YYYY MM DD" — ISO-style and broadcast convention
		// (wrestling, NBA, NHL releases all use 4-digit year first).
		// Anchored to 19/20-prefix year to keep "1080 12 34" resolution
		// + size combos from false-matching.
		`(\b((?:19|20)\d{2}[.\s_\-]\d{1,2}[.\s_\-]\d{1,2})\b)`,
		// DD-MM-YYYY unwrapped (European broadcast convention, e.g.
		// Russian sports rips "Хоккей НХЛ ... 04.05.2026.mkv").
		// Anchored to END of filename (optionally followed by a
		// container extension) so the Russian SATRip episode-pack
		// convention "Show.NN.YYYY.SATRip.avi" — where NN is the
		// episode index, not a day-of-month — keeps reading as
		// Episode + Year via the existing `.NN.YYYY.<rip>` pattern.
		// Year anchor on 19/20 prefix prevents matches like
		// "12.04.5060".
		`(\b(\d{1,2}[.\s_\-]\d{1,2}[.\s_\-](?:19|20)\d{2})\b)(?:\.[a-z0-9]{2,4})?$`,
		// YY-MM-DD (scene-release shorthand: "Blacked.18.03.21").
		`(\b(\d{2}[.\s_\-]\d{2}[.\s_\-]\d{2})\b)`,
		// DD.MM.YYYY in parens (European convention, common inside
		// adult-release tags: "Studio - Title (27.02.2026) rq.mp4").
		`(\((\d{1,2}[.\s_\-]\d{1,2}[.\s_\-]\d{4})\))`,
	), NewDateTransformer()},
	// Year is matched BEFORE Season/Episode so 4-digit year-shaped numbers
	// are consumed first. Otherwise the `(-\s+\d+)` episode pattern below
	// would happily eat "- 1997)" out of "(1990 - 1997)" and write it into
	// Episode, leaving Year to pick up the leftover. Same for "2026x29"
	// where the season/episode regex would otherwise match "26x29" against
	// the year's last two digits.
	//
	// Year-range patterns (e.g. "S01-S12.2007-2019" on long-running series)
	// must be consumed before the single-year matcher — its `last` policy
	// would otherwise pick the END of the run as the canonical year, which
	// neither TMDB nor OMDB indexes (the show is filed under its premiere
	// year). Group 2 captures the FIRST year in the range; group 1 captures
	// the whole "YYYY-YYYY" segment so it gets stripped from the title.
	{FieldTypeYear, NewRegexpMatcherLast(
		`\b((19\d{2}|20\d{2})\s*[-–—]\s*(?:19\d{2}|20\d{2}))\b`,
		`\b(((?:19[0-9]|20[0-9])[0-9]))\b`,
	), nil},
	// First alternative captures season ranges like "S01-S12" so they
	// get stripped from the title — otherwise long-running series leak
	// the range into the title ("The Big Bang Theory S01-S12") and
	// TMDB Search is run on garbage. Group 2 is the FIRST season number.
	//
	// Last alternative is the "bare season tag" form: a release like
	// "Shingeki.No.Kyojin.S3.WEB-DL.1080p.mkv" carries the season as
	// just "S3" with no following episode marker. Without this pattern,
	// "S3" would leak into the title (TMDB then misses because the show
	// is indexed as "Shingeki no Kyojin", not "Shingeki No Kyojin S3"),
	// AND the missing season trip hasSeasones=false → media-type
	// classifier falls into MovieMultiple for what is really one season
	// per torrent. The `\b` boundaries on both sides prevent matching
	// codec tags like "x264-S0E5" mid-word; the rule lives LAST so the
	// existing S01E01 / 01x05 forms still win when an episode marker
	// is present.
	{FieldTypeSeason, NewRegexpMatcher(
		`(?i)\b(s(\d{1,2})\s*[-–—]\s*s\d{1,2})\b`,
		`(?i)(s?([0-9]{1,2}))[ex]`,
		`(?i)(s?([0-9]{1,2}))\se`,
		`(?i)\b(s([0-9]{1,2}))\b`,
	), nil},
	{FieldTypeScene, NewRegexpMatcher(`(?i)(^S([0-9]{2}))`, `(?i)(Scene([0-9]{2}))`), nil},
	// Anime release-segment kind tag — runs BEFORE Episode so the Kind
	// word gets its own short span. Episode pattern then sees the Kind
	// span as already-taken and trims its own match to just the trailing
	// digit. Restricted to unambiguous anime abbreviations
	// (ONA/OVA/OAD/NCOP/NCED) so movie titles containing "Special" /
	// "Movie" / "Trailer" don't false-fire.
	{FieldTypeKind, NewRegexpMatcher(`(?i)\b((` + kindAlternation + `))\b`), nil},
	// Episode digit count capped at 3. Anything longer (4+ digits) is
	// always a year, size, codec tag, or some other false-positive — there
	// are no real shows with 1000+ episodes packaged into a single torrent.
	// Without the cap, "- 1997" / "- 1046" / "- 1080p" off a movie filename
	// would write into Episode and flip the whole torrent into series.
	//
	// Third alternative covers the Russian SATRip/dotted-format convention:
	// "ShowName.NN.YYYY.Quality.ext" (e.g. "Svati-2.01.2015.SATRip.avi") —
	// a 20-file release pack that without this pattern produced 20 distinct
	// movies with no episode, falling into MovieMultiple and firing one AI
	// call per file. Two-digit minimum prevents "Saw.7.2010" / "Rocky.4.1985"
	// movie-sequel filenames from false-matching (single-digit sequels are
	// not zero-padded; episode numbers conventionally are).
	//
	// Pattern 2 stays a strict `[ex]NN` form so codec/group letters
	// trailed by digits (e.g. "hegre 23" — `e` + space + 23) don't
	// false-fire. The verbose `epNN` / `Episode NN` / `ep 07` anime
	// conventions get their own alternative (pattern 5) below where the
	// longer-prefix anchor (`ep` / `episode`) provides enough context
	// to allow an optional separator. Digit count widened to 2-3 so
	// long anime runs ("ep123") parse.
	//
	// Fourth alternative is the "<NN> - <Title>" form: the episode number
	// precedes the dash with the episode title trailing
	// ("Onigashima 20 - Straw Hat Luffy", "Anna Karenina 02 - Серия").
	// Allows both space- and dot-separated filenames; requires a Unicode
	// letter (\p{L}) after the dash so any digit-NN-dash-NN combos (e.g.
	// "11.10.WS") can't false-fire.
	{FieldTypeEpisode, NewRegexpMatcher(
		`(-\s+([0-9]{1,3})(?:[^0-9]|$))`,
		`(?i)([ex]([0-9]{2,3})(?:[^0-9]|$))`,
		`(\.([0-9]{2,3})\.(?:19|20)[0-9]{2}\b)`,
		// Order matters: the `ep`/`episode` arm runs BEFORE the generic
		// "NN-dash-Letter" arm so a release like "ep 07 - Escape from
		// Side 1" consumes the full "ep 07 " span (leaving title clean)
		// instead of bottom-up "07 - E" leaving "ep" attached to title.
		`(?i)((?:episode|ep|chapter|глава|серия)[\s.\-_]?([0-9]{1,4})(?:[^0-9]|$))`,
		// Outer capture deliberately STOPS at the trailing dash —
		// the `\p{L}` letter requirement stays in the overall regex
		// to anchor "NN - <Letter>" form but does NOT get consumed
		// into the match span. Otherwise the first letter of the
		// following word (e.g. "P" of "Pokémon, I Choose You") got
		// pulled into Episode's consumed range and corrupted Title/
		// Extra downstream.
		`(\b([0-9]{2,4})[\s.]+-)[\s.]+\p{L}`,
		// Russian "NN серия <show>" naming. Aleksan55 rips and similar
		// Cyrillic SATRip/DVDRip releases put the episode number before
		// the word "серия" (= "episode"). Pattern matches the digit run
		// + separator + "серия"; the trailing word is consumed so the
		// title parser doesn't keep "серия" attached to the show name.
		`(?i)(\b([0-9]{1,3})[\s._]+серия)`,
		// Show.NN.<Quality> dotted Russian DVDRip convention without a
		// year anchor — "Грозовые ворота.01.DVDRip-SVAT.avi". The
		// trailing token must be a rip-form quality keyword
		// (qualityRipForms — shared with the Quality field-parser but
		// excluding broadcast/cam variants). Broadcast forms are
		// omitted because PPV events ("UFC.179.PPV.HDTV") put the
		// event id right before the broadcast marker, which would
		// false-fire episode=179. Two-digit minimum keeps single-digit
		// movie sequels ("Saw.7.BluRay") from matching.
		`(?i)(\.([0-9]{2,3})\.(?:` + qualityRipForms + `)\b)`,
		// Anime sub-episode types: digit AFTER a Kind tag (ONA/OVA/etc.).
		// Kind word itself is captured by FieldTypeKind below — this just
		// extracts the trailing index. Real torrent:
		// "[Cleo]Dies_Irae_-_ONA_01_(Dual Audio…).mkv" (underscores → spaces
		// before parsing, so matcher sees "Dies Irae - ONA 01 …").
		`(?i)(\b(?:` + kindAlternation + `)[\s.]+([0-9]{1,3})(?:[^0-9]|$))`,
		// Title-trailing 3-digit episode index. Catches the
		// "<Show> NNN [<Quality>]" convention used by Brazilian-PT
		// anime fansub releases — real torrent 9dd0bf1eaf (Dragon Ball
		// Clássico 001…153) had 153 files of shape "[AT] Dragon Ball
		// Clássico NNN [480p] (Legendado).mkv", none of which any prior
		// pattern caught, producing 153 distinct MovieMultiple rows
		// and 153 redundant Claude calls.
		//
		// Strictly 3-digit. Two-digit zero-padded forms (0N, 0NN) were
		// considered but regressed adult release-date packs where the
		// filename embeds `YY MM DD`: "hegre 23 08 22 allie" would
		// match " 08 " and corrupt Episode and Title. Real-world
		// episode packs reach 100+ entries anyway (anime/long-runners),
		// so the 3-digit restriction keeps the high-volume cases while
		// dropping the FP-prone short ones — those still get caught
		// by the existing `\b\d{2}[\s.]+-[\s.]+\p{L}` pattern when the
		// release format follows the "- 01 - Title" convention.
		// Anchored on `\s+` both sides so dotted-separator filenames
		// ("Some.Movie.300.x264.mkv") and immediately-trailing markers
		// ("1080p" → has 'p' after, no space) are immune.
		`(\s+([0-9]{3})\s+)`,
		// "<NN>. <Title>" prefix form used by Russian "Files-x" release
		// group convention (real torrents 1974978dd6 "Дуплет.2025" and
		// e123b05b68 "Сломанная стрела.2025" — 8 and 4 files respectively
		// of shape "NN. Title.Year.Quality.Files-x.ext"). The dot-then-
		// SPACE separator after the digits is the distinctive signal —
		// dotted-only filenames ("12.Years.a.Slave.2013") have NO space
		// after the leading-digit dot and stay immune.
		`(^([0-9]{2})\.\s+)`,
	), nil},
	{FieldTypeRegion, NewRegexpMatcher(`(?i)\b(R([0-9]))\b`), nil},
	{FieldTypeLanguage, NewRegexpMatcher(`(?i)\b((rus\.eng|ita\.eng))\b`), nil},
	{FieldTypeSBS, NewRegexpMatcher(`(?i)\b(((?:Half-)?SBS))\b`), nil},
	{FieldTypeContainer, NewRegexpMatcher(`(?i)\b((MKV|AVI|MP4|WEBM))\b`), nil},
	// FieldTypeStudio captures:
	//   1. Streaming-platform tags (AMZN/NF) — dropped from title before
	//      TMDB search.
	//   2. Studio-with-year inside brackets ("[Studio YYYY]") for
	//      anime-fansub release annotations.
	//   3. Adult studios (shared alternation with the Adult detector —
	//      see `adultStudioAlternation` above). Extracting these into
	//      Studio cleans the title and makes the studio name available
	//      as structured metadata downstream.
	{FieldTypeStudio, NewRegexpMatcher(
		`(?i)\b((AMZN|NF))\b`,
		`(\[ ?([^\]]+?)[\s.]?[0-9]{4}\])`,
		`(?i)\b((` + adultStudioAlternation + `))\b`,
	), titleTransformer},
	// "rip by <Name>" Russian release attribution. Aleksan55 SATRip and
	// similar fan-encoded packs trail every filename with "rip by X",
	// which otherwise leaks into Title and confuses TMDB/AI matching
	// ("Партизаны rip by Aleksan55" vs the real show "Партизаны").
	// Captures the attribution + ripper handle and stores it on RipBy.
	// Must run AFTER Container so the regex's `\S+` tail doesn't steal
	// "Aleksan55.mkv" — Container's span trims us back via getAvailable.
	{FieldTypeRipBy, NewRegexpMatcher(`(?i)(\brip\s+by\s+(\S+))`), nil},
}

// adultMatcher carries the FieldTypePorn detection regexes. Each pattern
// is a separate regex so the matcher's "first hit wins" semantic still
// works; only the bool flag matters — the captured content itself is
// discarded.
//
// Cyrillic and CJK patterns use a non-capturing `(?:^|[^...])` prefix
// guard instead of `\b` because Go's RE2 `\b` only recognizes ASCII
// word characters. Without the guard, `трах[...]` would false-match
// "страх" (fear) and similar.
//
// Used downstream to skip Claude-backed enrichment for the ~30-40%
// of negative-cache traffic that is porn/JAV/cam content (see
// ai_enrich.query telemetry 2026-05-11).
var adultMatcher = NewRegexpMatcher(
	// Explicit XXX scene tag. Kept first for backwards compatibility
	// — existing golden_file_083 expects this exact match.
	`(?i)\b((X{3}))\b`,
	// English single-occurrence keywords. All of these are
	// effectively never found in non-adult release names.
	`(?i)\b((porn(?:o|hub|star)?|hentai|gangbang|bukkake|deepthroat|fisting|cums?hot|cum(?:ming)?|blowjob|handjob|footjob|threesome|creampie|squirter|squirting|cuckold|stepmom|stepdad|stepsis|stepson|stepbro|stepsister|stepdaughter|stepbrother|stepfather|stepmother|hotwife|pawg|gloryhole|nudism|nudist|camgirl|camslut|masturbat[a-z]*|fingering|titties|titty|fetish|fuckermate|blackzilla|tranny|trannys|trannies|twink|pmv|anal|pussy))\b`,
	// Adult studios / sites (case-insensitive). Curated from
	// ai_enrich.query — every name here was observed dominating
	// the negative cache (milfy alone: 106 rows). The list itself
	// lives in `adultStudioAlternation` so FieldTypeStudio can
	// extract the matched name into Studio without duplicating
	// the alternation.
	`(?i)\b((` + adultStudioAlternation + `))\b`,
	// Bestiality phrases — explicit "dog/zoo/horse + sex/fuck/porn"
	// and the "art of zoo" series of bestiality torrents.
	`(?i)\b((art\s+of\s+zoo|(?:dog|zoo|horse|animal)\s+(?:sex|fuck|porn|cum)))\b`,
	// "bate" cam-girl convention: handle + "bate" + 6+ digit
	// timestamp ("alinajellybeana bate 090607 stickam"). Plain
	// "bate" alone would clash with "Bates Motel" etc., so require
	// the trailing date.
	`(?i)((bate)[\s\-_]\d{6,})`,
	// "of - " / "of – " — OnlyFans abbreviation at the start of a
	// title. Standalone "of" is too common to match unanchored.
	`(?i)^((of\s*[\-–]\s))`,
	// BBC ("Big Black Cock") paired with an adult anchor word.
	// Excludes "BBC News", "BBC Earth", etc. — those have neither
	// these verbs nor matching nouns.
	`(?i)\b((bbc))\s+(?:cock|fuck|treat|surprise|crave|hungry|addict|obsess|loves?|monster|stretch|hung|breed|breeding|destroy|destroys|stretching|inches|fan|goddess|hotwife|wife|stud|stallion)`,
	// JAV studio code prefix + numeric serial. Prefix list pruned
	// to combinations unlikely to collide with English words / years
	// (so no bare "sw", "jul", "mum", "stars"). The required
	// trailing `\d{2,5}` is what disambiguates ambiguous prefixes
	// like APNS (Apple Push Notification Service) — only
	// "APNS-410" form fires, "APNS notifications" stays clean.
	//
	// Separator between prefix and serial is `[\s\-_.]*` (zero or
	// more) — JAV codes appear as "ABP-123", "ABP_123", "ABP.123",
	// or after parser normalisation "ABP 123" with a single space.
	// Dropped from this list intentionally: `md`, `roe`, `shc` —
	// 2-3 char prefixes that collide too easily with legitimate
	// non-adult content ("MD-80" airliner, "Roe v Wade", "SHC" generic
	// initialism). Acceptable FN cost: each accounted for ≤1 hit/day
	// in the 2026-05-13 audit.
	`(?i)\b((aarm|abp|abw|adn|apns|atid|beaf|cawd|dasd|dldss|dpvr|dvaj|ebod|fsdss|getchu|gmem|hbad|hmn|hnd|huntc|imoe|ipvr|ipx|ipz|ipzz|jera|jufd|jufe|jur|juvr|juy|kbd|kbpd|lafbd|maxvr|mdhr|meyd|mgnl|miaa|mide|midv|mird|mism|mmus|mudr|nhdtb|niks|onez|pred|prtd|rbd|rct|real|sdab|savr|sdde|sdmu|shkd|snis|snos|sone|ssis|ssni|start|svace|tyhpj|venu|venx|wanz)[\s\-_.]*\d{2,5}(?:-?[a-z]{1,3})?)\b`,
	// Russian explicit markers. (?i) lets uppercase forms ("Трахаю")
	// match the lowercase alternation. Non-Cyrillic prefix guard
	// prevents false matches like "страх" (fear) → "трах".
	// `сводн<case>\s+(сестр|брат)` — Russian "stepsibling" phrase,
	// near-exclusively used in incest-themed adult releases. Plain
	// `сводн` would FP on "сводный закон" / "сводная таблица" (sum
	// chart) so we require the explicit family-member follow-up.
	`(?i)((?:^|[^а-яА-Я])(трах[аеёиоунюя]|еб[аеёилоутю]|инцест|шлюх|минет|дрочи|кримпай|пизд|сперм|порно|сводн(?:ая|ый|ого|ой|ому)[\s_]+(?:сестр|брат)))`,
	// Chinese adult markers — uncensored / leaked / explicit body
	// terms / adult-BBS shorthand. No CJK prefix guard: these tokens
	// don't appear inside other common Chinese words, and Chinese
	// titles concatenate ideographs (so "[^CJK]" would block real
	// hits like "极品...内射" mid-string).
	`((无码|無碼|中文字幕|流出|探花|美穴|馒头|内射|中出|偷拍|啪啪|淫|网黄|網黃))`,
	// Chinese / paywall-rip filename prefix: `<prefix>.<tld><sep>` —
	// signature shape of paywall-scraped adult content from CN
	// forums (4k2.com@, 2048.vip-, big2048.com@, 489155.com@,
	// aosogo.cc@). The site-id + glyph (`@`, `-`) is a content-
	// pirate convention and effectively never appears in legitimate
	// releases. Earlier version required digit-only prefix + `@`;
	// observed alphabetic prefixes ("aosogo.cc@") and `2048.vip-`
	// (hyphen instead of `@`) so the alternation now covers both —
	// but with `[a-z0-9]{3,}` minimum to keep accidents like a stray
	// `it.com-X` shape from matching.
	`(?i)(([a-z0-9]{3,}\.(?:com|vip|net|cc|me)[@-]))`,
)

// sportMatcher detects FieldTypeSport — used downstream to skip AI
// enrichment on sports broadcasts (TMDB/OMDB/KPU don't index them
// and Claude has nothing to add).
//
// Three pattern groups:
//
//   1. League abbreviations as standalone tokens (NBA, NHL, WWE, ...).
//      The `\b...\b` word boundary keeps "Mr. NBA" / "NBA documentary"
//      flagged but skips substrings like "NB" inside "NBNA".
//   2. Multi-word competition names ("Premier League", "Champions
//      League", "World Cup"). The `\s+` accommodates the parser's
//      "underscore→space" preprocessing.
//   3. Russian Cyrillic markers (НХЛ, КХЛ, РПЛ, хоккей, футбол).
//      Non-Cyrillic prefix guard prevents false matches inside
//      compound words.
//
// Wrestling-specific show names (Monday Night Raw, SmackDown, NXT,
// Dynamite, Collision, Rampage) are included because they're 1:1
// with WWE/AEW programming with no overlap with non-sport titles.
var sportMatcher = NewRegexpMatcher(
	`(?i)\b((NBA|NHL|NFL|MLB|MLS|WNBA|WWE|AEW|UFC|ATP|WTA|PGA|MotoGP|NASCAR|F1|IndyCar))\b`,
	`(?i)\b((Monday\s+Night\s+Raw|SmackDown|NXT|Dynamite|Collision|Rampage|WrestleMania|SummerSlam|Royal\s+Rumble|Survivor\s+Series))\b`,
	`(?i)\b((Premier\s+League|Champions\s+League|Europa\s+League|La\s+Liga|Bundesliga|Serie\s+A|Ligue\s+1|World\s+Cup|FIFA|UEFA|UEFA\s+Euro|Euro\s*20\d{2}|Copa\s+Am[eé]rica|African\s+Cup|Stanley\s+Cup|Super\s+Bowl|Eurocup|Euroleague|IPL\s*20\d{2}|Royal\s+Challengers|Knight\s+Riders|Mumbai\s+Indians|Chennai\s+Super\s+Kings|Sunrisers\s+Hyderabad|Delhi\s+Capitals|Punjab\s+Kings|Rajasthan\s+Royals|Gujarat\s+Titans|Lucknow\s+Super))\b`,
	// Cycling grand tours — "109th Giro d'Italia 2026 Stage 05" /
	// "Tour de France 2024" / "Vuelta a España". The apostrophe in
	// "d'Italia" is `['\x{2019}]?` so both ASCII and Unicode curly
	// quotes match. Pattern is `(?:...)` outer non-capture wrapping
	// the alternation so the `?` is allowed.
	`(?i)\b((Giro\s+d['\x{2019}]?Italia|Tour\s+de\s+France|Vuelta\s+a\s+Espa(?:ñ|n)a))\b`,
	`(?i)((?:^|[^а-яА-Я])(НХЛ|КХЛ|РФПЛ|РПЛ|хоккей|футбол|баскетбол|теннис|биатлон|формула[\s\-]*1|велогонк[а-я]+|велотур))`,
)

// courseMatcher detects FieldTypeCourse — pirated-course / tutorial /
// e-learning content that has nothing useful to retrieve from
// TMDB/OMDB/KPU. Downstream enrichment skips both AI fallback and
// path-title fallback when this flag fires.
//
// Five pattern groups, all high-confidence:
//
//   1. Mainstream e-learning platforms ("Udemy", "Coursera",
//      "Pluralsight", "MasterClass", ...). These names are
//      essentially never used as movie / TV titles.
//   2. Pirate-aggregator bracket prefixes that pirate-course
//      torrents universally carry — "[FreeCourseSite.com]",
//      "[TutsNode]", "[DevCourseWeb]", etc. The bracket-with-domain
//      shape itself is the marker, paired with one of the known
//      aggregator domains.
//   3. Bracketless aggregator suffix — "-Paracourse.webm" filename
//      convention used by some course-rip aggregators (the bracket
//      shape is absent, the marker is the brand name attached as a
//      file-name suffix).
//   4. ".courses" TLD — Russian / niche course platforms
//      ("karpov.courses", "geekbrains.courses"). Treating the TLD
//      itself as the marker covers future-proof additions without
//      maintaining a brand list.
//   5. Game DLC tutorials and storefront-rip indicators — game
//      vendor-rip releases ("Ubisoft.Connect.Rip", "GOG.Rip",
//      "Steam.Rip") or DLC-tutorial file conventions
//      ("DLC_AbilityTutorial_..."). Per-user request, these are
//      treated as courses since they are interactive-software
//      tutorials with no movie/TV metadata.
var courseMatcher = NewRegexpMatcher(
	`(?i)\b((udemy|coursera|pluralsight|udacity|skillshare|linkedin\s*learning|edx\.org|teamtreehouse|frontendmasters|datacamp|codecademy|egghead\.io|tutsplus|packt|oreilly|safari\s*books|master[\s.-]?class|medcurso|paracourse|gnomon[\s.-]?workshop|cerebellum[\s.-]?(?:academy|btr|tnd)?|slerm[\s.-]?(?:io|courses?)?|bc-[a-z]+course))\b`,
	`(?i)(\[\s*(freecoursesite|freecoursesonline|fcsnew|tutsnode|devcourseweb|webtooltip|freecourselab|freeallcourse|coursehunters|coursedrive|tutslet|udemyking|freetutorials|gigacourse|getfreecourses|freecoursenet)\.[a-z]{2,4}\s*\])`,
	`(?i)\b([a-z0-9-]+\.(courses))\b`,
	`(?i)\b((dlc[\s_]+\w*tutorial|ubisoft[\s.]?connect[\s.]?rip|steam[\s.-]?rip|gog[\s.-]?rip|epic[\s.-]?games[\s.-]?rip))\b`,
	`(?i)((видео[\s_-]?(?:курс|школа|урок)))`,
)

var parser = NewCompoundParser([]Parser{
	// Run FIRST so Adult detection sees the unmolested input. Transient
	// so its matched spans don't truncate Title downstream — see
	// "Bang My Tranny Ass" trace in parser.go/Match.Transient.
	NewTransientFieldParser(FieldTypeAdult, adultMatcher, nil),
	// Sport — same transient flag-only treatment. Pairs with the
	// `isSportPath` check in services/enrich/enrich.go to short-
	// circuit Claude calls for broadcasts (NBA/NHL/UFC/WWE etc.).
	NewTransientFieldParser(FieldTypeSport, sportMatcher, nil),
	// Course — same transient flag for educational content. Pairs
	// with isCoursePath in services/enrich to skip AI + path-title
	// fallback for Udemy/Coursera/pirate-aggregator releases.
	NewTransientFieldParser(FieldTypeCourse, courseMatcher, nil),
	NewCompoundParser(fieldParsers.ToParserSlice()),
	// Swallow stray separator characters into the adjacent consumed
	// spans so the gaps between non-transient matches stop being
	// "first available subrange" candidates for the greedy Title
	// regex. Without this, "angelslove.26.05.10.chanel.x..." → Title=""
	// because Title's `[^\[\(\{]*` match returned a one-byte "." gap.
	NewSeparatorExpander(),
	NewScopeParser(NewRegexpMatcher(`((.*))`), NewCompoundParser([]Parser{
		NewFieldParser(FieldTypeExtraTitle, NewRegexpMatcher(`(\[([^\)]+)\])`), titleTransformer),
		NewFieldParser(FieldTypeTitle, NewRegexpMatcher(`(([^\[\(\{]*))`), titleTransformer),
	})),
	NewFieldParser(FieldTypeGroup, NewRegexpMatcher(
		// Dotted-format release group with INTERNAL dash. Real torrents
		// 1974978dd6 (Дуплет.2025) and e123b05b68 (Сломанная стрела.2025)
		// — Russian "Files-x" group ships every file as
		// "<NN>. Title.Year.Quality.Files-x.<ext>". The default
		// dash-prefix regex below splits on the internal dash and
		// captures only "x", losing the "Files-" prefix.
		//
		// Anchored to "Files-" specifically rather than a generic
		// `\.(\w+-\w+)\.<ext>` because the generic form would false-fire
		// on quality tags ("WEB-DL.mkv" → group="WEB-DL") and codec tags
		// ("H264-RARBG" — though that one is dash-prefix not dotted, the
		// risk of overlap is real). Extend the alternation in this
		// regex with new known-dash-internal release-group prefixes as
		// they appear.
		`(?i)(\.((?:Files)-[a-zA-Z0-9]+))(?:\.[a-z0-9]{2,4})?$`,
		`\b(- ?([^-]+(?:-={[^-]+-?$)?))$`,
		// Space-dash-space form: "H264 - YIFY", "2014 - YIFY". The
		// existing `\b-` anchor fails when the dash has whitespace on
		// BOTH sides (no word/non-word transition AT the dash). Captured
		// group is restricted to a single token (no internal space) so
		// episode-title forms like "Onigashima 20 - Straw Hat Luffy" do
		// NOT spill into Group — multi-word trailing chunks belong in
		// Title/Extra, not the release-group field.
		` (- +([^-\s\[\]\{\}\(\)]+))$`,
	), nil),
	// LAST in the chain — collects any bytes no prior parser claimed
	// into FieldTypeExtra. Transient match so it doesn't affect any
	// downstream getAvailable (there are no downstream parsers, but
	// keeping the field span-neutral matches its semantic role: a
	// summary of leftovers, not a span-claim of its own).
	NewExtraExtractor(),
})

// Parse breaks up the given filename in TorrentInfo
func Parse(tor *TorrentInfo, filename string) (*TorrentInfo, error) {
	cleanName := strings.Replace(filename, "_", " ", -1)

	ms, err := parser.Parse(cleanName, Matches{})
	if err != nil {
		return nil, err
	}

	tor.Map(ms)

	// Promote `[<adult-studio>]` from Website to Studio. The Website
	// parser at line 64 is permissive — it claims ANY `[...]` prefix —
	// so adult releases like "[RKPrime] Megan Rain - ..." land in
	// Website even though "RKPrime" is a known studio. Without this
	// fixup the studio info is in the wrong field for downstream filters.
	if tor.Studio == "" && tor.Website != "" {
		if adultStudioRe.MatchString(tor.Website) {
			tor.Studio = tor.Website
			tor.Website = ""
		}
	}

	// Year-from-Date back-fill. When a scene date is extracted (almost
	// always from adult/dated releases — see FieldTypeDate above) and
	// no explicit 4-digit year survives in the filename, copy the
	// year component over so downstream metadata lookups still have
	// a year filter. Examples:
	//   "Blacked.18.03.21.Lana.Rhoades"   → Date=2018-03-21, Year=2018
	//   "Studio - Title (27.02.2026) rq"  → Date=2026-02-27, Year=2026
	// Date span has already consumed the surrounding 4-digit year
	// (when the source carried one), so this branch fires for the
	// YY-prefix form too.
	if tor.Year == 0 && len(tor.Date) >= 4 {
		if y, err := strconv.Atoi(tor.Date[:4]); err == nil {
			tor.Year = y
		}
	}

	return tor, nil
}

func GetFieldParser(fielType FieldType) *FieldParser {
	for _, p := range fieldParsers {
		if p.FieldType == fielType {
			return p
		}
	}
	return nil
}
