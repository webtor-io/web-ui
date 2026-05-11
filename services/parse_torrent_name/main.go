package parsetorrentname

import "strings"

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
	// FieldTypePorn — adult/erotic content marker. Used downstream to
	// skip Claude-backed enrichment for the ~30-40% of negative-cache
	// traffic that is porn/JAV/cam content (see ai_enrich.query telemetry
	// 2026-05-11). Each pattern is a separate regex so the matcher's
	// "first hit wins" semantic still works; only the bool flag matters
	// — the captured content itself is discarded.
	//
	// Cyrillic and CJK patterns use a non-capturing `(?:^|[^...])` prefix
	// guard instead of `\b` because Go's RE2 `\b` only recognizes ASCII
	// word characters. Without the guard, `трах[...]` would false-match
	// "страх" (fear) and similar.
	{FieldTypePorn, NewRegexpMatcher(
		// Explicit XXX scene tag. Kept first for backwards compatibility
		// — existing golden_file_083 expects this exact match.
		`(?i)\b((X{3}))\b`,
		// English single-occurrence keywords. All of these are
		// effectively never found in non-adult release names.
		`(?i)\b((porn(?:o|hub|star)?|hentai|gangbang|bukkake|deepthroat|fisting|cums?hot|blowjob|handjob|footjob|threesome|creampie|squirter|squirting|cuckold|stepmom|stepdad|stepsis|stepson|stepbro|stepsister|stepdaughter|stepbrother|stepfather|stepmother|hotwife|pawg|gloryhole|nudism|nudist|camgirl|camslut|masturbat[a-z]*|fingering|titties|titty))\b`,
		// Adult studios / sites (case-insensitive). Curated from
		// ai_enrich.query — every name here was observed dominating
		// the negative cache (milfy alone: 106 rows).
		`(?i)\b((blackedraw|blacked|brazzers|naughtyamerica|mylf|milfy|mylfx|hegre|onlyfans|manyvids|pornstarwife|wowgirls|spankmonster|momswapped|latinpapixxl|latinpapi|allover30|gilfaf|edgedandbound|maturenl|mofos|ersties|hgshequ|hhd800|fakehub|bangbros|realitykings|teamskeet|atkgalleria|atkhairy|czechcasting|fc2ppv|heyzo|10musume|1pondo|s-cute|stickam|voyeur-russian|julesjordan|nubilesporn|exploitedcollegegirls|kink\.com|milflicious|wankzvr|tushy|deeper\.com|vixen\.com))\b`,
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
		// (so no bare "sw", "jul", "mum", "stars").
		`(?i)\b((abp|abw|adn|atid|cawd|dasd|ebod|hbad|hmn|hnd|ipvr|ipx|ipz|jufe|meyd|mide|midv|mird|pred|prtd|rbd|rct|sdde|sdmu|shkd|sone|ssis|ssni|venu|venx|wanz)[\-_]?\d{2,5})\b`,
		// Russian explicit markers. (?i) lets uppercase forms ("Трахаю")
		// match the lowercase alternation. Non-Cyrillic prefix guard
		// prevents false matches like "страх" (fear) → "трах".
		`(?i)((?:^|[^а-яА-Я])(трах[аеёиоунюя]|еб[аеёилоутю]|инцест|шлюх|минет|дрочи|кримпай|пизд|сперм|порно))`,
		// Chinese adult markers — uncensored / leaked / explicit body
		// terms / adult-BBS shorthand. No CJK prefix guard: these tokens
		// don't appear inside other common Chinese words, and Chinese
		// titles concatenate ideographs (so "[^CJK]" would block real
		// hits like "极品...内射" mid-string).
		`((无码|無碼|中文字幕|流出|探花|美穴|馒头|内射|中出|偷拍|啪啪|淫|网黄|網黃))`,
	), nil},
	{FieldTypeSize, NewRegexpMatcher(`(?i)\b((\d+(?:\.\d+)?(?:GB|MB)))\b`), nil},
	{FieldTypeQuality, NewRegexpMatcher(`(?i)\b(((?:PPV\.)?[HP]DTV|(?:HD)?CAM|B[DR]Rip|(?:HD-?)?TS|(?:PPV )?WEB-?DL(?:Rip)?|HDRip|DVDRip|DVDRIP|CamRip|W[EB]BRip|BluRay|DvDScr|telesync))\b`), nil},
	{FieldTypeResolution, NewRegexpMatcher(`\b(([0-9]{3,4}p|[248][Kk]))\b`), NewLowercaseTransformer()},
	{FieldTypeBitrate, NewRegexpMatcher(`(?i)\b(([0-9]+[KMGT]bps))\b`), nil},
	{FieldTypeColorDepth, NewRegexpMatcher(`(?i)(([HS]DR(?:[0-9]{0,2})?\+?))`), nil},
	{FieldTypeCodec, NewRegexpMatcher(`(?i)\b((xvid|[hx]\.?26[45]))\b`), nil},
	{FieldTypeAudio, NewRegexpMatcher(`(?i)\b((MP3|DD5\.?1|Dual[\- ]Audio|LiNE|DTS|AAC[.-]LC|AAC(?:\.?2\.0)?|AC3(?:(?:[\s-]+)?\.?5\.1)?))\b`), nil},
	{FieldTypeWebsite, NewRegexpMatcher(`^((www\.[a-zA-Z0-9][a-zA-Z0-9-]{1,61}[a-zA-Z0-9]\.[a-zA-Z]{2,}))`, `^(\[ ?([^\]]+?) ?\])`), nil},
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
		`(?i)((?:episode|ep)[\s.]?([0-9]{2,3})(?:[^0-9]|$))`,
		`(\b([0-9]{2,3})[\s.]+-[\s.]+\p{L})`,
	), nil},
	{FieldTypeRegion, NewRegexpMatcher(`(?i)\b(R([0-9]))\b`), nil},
	{FieldTypeLanguage, NewRegexpMatcher(`(?i)\b((rus\.eng|ita\.eng))\b`), nil},
	{FieldTypeSBS, NewRegexpMatcher(`(?i)\b(((?:Half-)?SBS))\b`), nil},
	{FieldTypeContainer, NewRegexpMatcher(`(?i)\b((MKV|AVI|MP4|WEBM))\b`), nil},
	{FieldTypeStudio, NewRegexpMatcher(`(?i)\b((AMZN|NF))\b`, `(\[ ?([^\]]+?)[\s.]?[0-9]{4}\])`), titleTransformer},
}

var parser = NewCompoundParser([]Parser{
	NewCompoundParser(fieldParsers.ToParserSlice()),
	NewScopeParser(NewRegexpMatcher(`((.*))`), NewCompoundParser([]Parser{
		NewFieldParser(FieldTypeExtraTitle, NewRegexpMatcher(`(\[([^\)]+)\])`), titleTransformer),
		NewFieldParser(FieldTypeTitle, NewRegexpMatcher(`(([^\[\(\{]*))`), titleTransformer),
	})),
	NewFieldParser(FieldTypeGroup, NewRegexpMatcher(`\b(- ?([^-]+(?:-={[^-]+-?$)?))$`), nil),
})

// Parse breaks up the given filename in TorrentInfo
func Parse(tor *TorrentInfo, filename string) (*TorrentInfo, error) {
	cleanName := strings.Replace(filename, "_", " ", -1)

	ms, err := parser.Parse(cleanName, Matches{})
	if err != nil {
		return nil, err
	}

	tor.Map(ms)

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
