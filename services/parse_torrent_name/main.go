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
	{FieldTypeSeason, NewRegexpMatcher(
		`(?i)\b(s(\d{1,2})\s*[-–—]\s*s\d{1,2})\b`,
		`(?i)(s?([0-9]{1,2}))[ex]`,
		`(?i)(s?([0-9]{1,2}))\se`,
	), nil},
	{FieldTypeScene, NewRegexpMatcher(`(?i)(^S([0-9]{2}))`, `(?i)(Scene([0-9]{2}))`), nil},
	// Episode digit count capped at 3. Anything longer (4+ digits) is
	// always a year, size, codec tag, or some other false-positive — there
	// are no real shows with 1000+ episodes packaged into a single torrent.
	// Without the cap, "- 1997" / "- 1046" / "- 1080p" off a movie filename
	// would write into Episode and flip the whole torrent into series.
	{FieldTypeEpisode, NewRegexpMatcher(`(-\s+([0-9]{1,3})(?:[^0-9]|$))`, `(?i)([ex]([0-9]{2})(?:[^0-9]|$))`), nil},
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
