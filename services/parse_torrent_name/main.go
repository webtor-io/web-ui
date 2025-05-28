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
	{FieldTypePorn, NewRegexpMatcher(`(?i)\b((X{3}))\b`), nil},
	{FieldTypeSize, NewRegexpMatcher(`(?i)\b((\d+(?:\.\d+)?(?:GB|MB)))\b`), nil},
	{FieldTypeQuality, NewRegexpMatcher(`(?i)\b(((?:PPV\.)?[HP]DTV|(?:HD)?CAM|B[DR]Rip|(?:HD-?)?TS|(?:PPV )?WEB-?DL(?:Rip)?|HDRip|DVDRip|DVDRIP|CamRip|W[EB]BRip|BluRay|DvDScr|telesync))\b`), nil},
	{FieldTypeResolution, NewRegexpMatcher(`\b(([0-9]{3,4}p|[248]K))\b`), nil},
	{FieldTypeBitrate, NewRegexpMatcher(`(?i)\b(([0-9]+[KMGT]bps))\b`), nil},
	{FieldTypeColorDepth, NewRegexpMatcher(`(?i)(([HS]DR(?:[0-9]{0,2})?\+?))`), nil},
	{FieldTypeCodec, NewRegexpMatcher(`(?i)\b((xvid|[hx]\.?26[45]))\b`), nil},
	{FieldTypeAudio, NewRegexpMatcher(`(?i)\b((MP3|DD5\.?1|Dual[\- ]Audio|LiNE|DTS|AAC[.-]LC|AAC(?:\.?2\.0)?|AC3(?:(?:[\s-]+)?\.?5\.1)?))\b`), nil},
	{FieldTypeSeason, NewRegexpMatcher(`(?i)(s?([0-9]{1,2}))[\sex]`), nil},
	{FieldTypeScene, NewRegexpMatcher(`(?i)(^S([0-9]{2}))`, `(?i)(Scene([0-9]{2}))`), nil},
	{FieldTypeEpisode, NewRegexpMatcher(`(-\s+([0-9]{1,})(?:[^0-9]|$))`, `(?i)([ex]([0-9]{2})(?:[^0-9]|$))`), nil},
	{FieldTypeYear, NewRegexpMatcherLast(`\b(((?:19[0-9]|20[0-9])[0-9]))\b`), nil},
	{FieldTypeRegion, NewRegexpMatcher(`(?i)\b(R([0-9]))\b`), nil},
	{FieldTypeWebsite, NewRegexpMatcher(`^((www\.[a-zA-Z0-9][a-zA-Z0-9-]{1,61}[a-zA-Z0-9]\.[a-zA-Z]{2,}))`, `^(\[ ?([^\]]+?) ?\])`), nil},
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

	for _, m := range ms {
		tor.mapField(tor, m.FieldType, m.Content)
	}

	return tor, nil
}
