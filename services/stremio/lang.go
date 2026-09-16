package stremio

import "strings"

// Language describes a language entry exposed to the Stremio settings UI
// and used for filtering stream titles. Mirrors the LANGUAGES list in
// assets/src/js/lib/discover/lang.js so the Stremio addon and the
// Discover stream modal share the same detection rules.
type Language struct {
	Code string
	Name string
	Flag string
	// TitleAliases are the tokens that mean this language *in a torrent
	// title*, and nothing else. They are a different question from the
	// three fields above, which are identity: what the language is called
	// and how it is drawn. A row may be perfectly nameable -- offered in
	// the Stremio settings dropdown, resolvable by LanguageByCode, given a
	// flag and a name on a picker chip -- and still carry no title
	// aliases at all, which is exactly what the twelve rows appended in
	// 2026-09 do.
	//
	// Empty means "never detected from a title": neither these tokens nor
	// the flag enter langMap, so ExtractLanguages cannot answer with this
	// language. That is the safe default for a code nobody has measured
	// against real release vocabulary, because the cost is asymmetric --
	// a language not detected loses one filter chip, while a language
	// detected wrongly *pre-empts* the right answer (ExtractLanguages
	// runs its Cyrillic fallback only when nothing else matched) and
	// LangFilterStream then drops the release from the viewer's list
	// entirely. "KAT" is KickassTorrents, not Georgian; "EST" is
	// Electronic Sell-Through, not Estonian.
	TitleAliases []string
}

// Detectable reports whether ExtractLanguages can ever answer with this
// language. Callers that filter *by* a language have to ask: keeping only
// the streams that advertise a language nothing can advertise keeps none,
// which reads to the viewer as "there is nothing for this film".
func (l *Language) Detectable() bool {
	return len(l.TitleAliases) > 0
}

// Languages is the canonical, ordered list of supported languages. Keep in
// sync with assets/src/js/lib/discover/lang.js.
var Languages = []Language{
	{Code: "en", Name: "English", Flag: "🇬🇧", TitleAliases: []string{"eng", "english", "en"}},
	{Code: "ru", Name: "Russian", Flag: "🇷🇺", TitleAliases: []string{"rus", "russian", "ru", "рус", "русский"}},
	{Code: "uk", Name: "Ukrainian", Flag: "🇺🇦", TitleAliases: []string{"ukr", "ukrainian", "ua", "укр", "українська"}},
	{Code: "it", Name: "Italian", Flag: "🇮🇹", TitleAliases: []string{"ita", "italian", "it"}},
	{Code: "fr", Name: "French", Flag: "🇫🇷", TitleAliases: []string{"fre", "french", "fr"}},
	{Code: "es", Name: "Spanish", Flag: "🇪🇸", TitleAliases: []string{"spa", "spanish", "es"}},
	{Code: "de", Name: "German", Flag: "🇩🇪", TitleAliases: []string{"ger", "german", "de"}},
	{Code: "pt", Name: "Portuguese", Flag: "🇧🇷", TitleAliases: []string{"por", "portuguese", "pt"}},
	{Code: "cs", Name: "Czech", Flag: "🇨🇿", TitleAliases: []string{"cze", "czech", "cz"}},
	{Code: "pl", Name: "Polish", Flag: "🇵🇱", TitleAliases: []string{"pol", "polish", "pl"}},
	{Code: "nl", Name: "Dutch", Flag: "🇳🇱", TitleAliases: []string{"dut", "dutch", "nl"}},
	{Code: "ja", Name: "Japanese", Flag: "🇯🇵", TitleAliases: []string{"jpn", "japanese", "ja"}},
	{Code: "ko", Name: "Korean", Flag: "🇰🇷", TitleAliases: []string{"kor", "korean", "ko"}},
	{Code: "zh", Name: "Chinese", Flag: "🇨🇳", TitleAliases: []string{"chi", "chinese", "zh"}},
	{Code: "ar", Name: "Arabic", Flag: "🇸🇦", TitleAliases: []string{"ara", "arabic", "ar"}},
	{Code: "hi", Name: "Hindi", Flag: "🇮🇳", TitleAliases: []string{"hin", "hindi", "hi"}},
	{Code: "tr", Name: "Turkish", Flag: "🇹🇷", TitleAliases: []string{"tur", "turkish", "tr"}},
	{Code: "sv", Name: "Swedish", Flag: "🇸🇪", TitleAliases: []string{"swe", "swedish", "sv"}},
	{Code: "no", Name: "Norwegian", Flag: "🇳🇴", TitleAliases: []string{"nor", "norwegian", "no"}},
	{Code: "da", Name: "Danish", Flag: "🇩🇰", TitleAliases: []string{"dan", "danish", "da"}},
	{Code: "fi", Name: "Finnish", Flag: "🇫🇮", TitleAliases: []string{"fin", "finnish", "fi"}},
	{Code: "ro", Name: "Romanian", Flag: "🇷🇴", TitleAliases: []string{"rum", "romanian", "ro"}},
	{Code: "hu", Name: "Hungarian", Flag: "🇭🇺", TitleAliases: []string{"hun", "hungarian", "hu"}},
	{Code: "el", Name: "Greek", Flag: "🇬🇷", TitleAliases: []string{"gre", "greek", "el"}},
	{Code: "bg", Name: "Bulgarian", Flag: "🇧🇬", TitleAliases: []string{"bul", "bulgarian", "bg"}},
	{Code: "hr", Name: "Croatian", Flag: "🇭🇷", TitleAliases: []string{"hrv", "croatian", "hr"}},
	{Code: "sr", Name: "Serbian", Flag: "🇷🇸", TitleAliases: []string{"srp", "serbian", "sr"}},
	{Code: "sl", Name: "Slovenian", Flag: "🇸🇮", TitleAliases: []string{"slv", "slovenian", "sl"}},
	{Code: "he", Name: "Hebrew", Flag: "🇮🇱", TitleAliases: []string{"heb", "hebrew", "he"}},
	{Code: "th", Name: "Thai", Flag: "🇹🇭", TitleAliases: []string{"tha", "thai", "th"}},
	{Code: "vi", Name: "Vietnamese", Flag: "🇻🇳", TitleAliases: []string{"vie", "vietnamese", "vi"}},
	{Code: "id", Name: "Indonesian", Flag: "🇮🇩", TitleAliases: []string{"ind", "indonesian", "id"}},
	{Code: "ms", Name: "Malay", Flag: "🇲🇾", TitleAliases: []string{"may", "malay", "ms"}},
	// Appended 2026-09-16 so every code the subtitle-translate service
	// accepts has an entry here (TestLanguagesCoverTheTranslateService
	// pins that). Order of the entries above is unchanged: the Stremio
	// settings list and the language row read this order, and reshuffling
	// it would move chips under people.
	//
	// None of them carries TitleAliases, deliberately (review C1, fixed
	// 2026-09-16 before merge). The first version gave them the obvious
	// ISO 639-2/B codes and two-letter tags, and measurement said no: KAT
	// is KickassTorrents branding, EST is the Electronic-Sell-Through
	// release tag, and "Фильм 2019 [KAT] 1080p" came back Georgian
	// instead of Russian -- a false positive pre-empts the Cyrillic
	// fallback, and LangFilterStream is exclusive, so that release
	// vanished from a Russian viewer's Stremio list. These rows exist to
	// be named and chosen, not found; a token list can be added later per
	// language, against real titles.
	{Code: "sk", Name: "Slovak", Flag: "🇸🇰"},
	{Code: "lt", Name: "Lithuanian", Flag: "🇱🇹"},
	{Code: "lv", Name: "Latvian", Flag: "🇱🇻"},
	{Code: "et", Name: "Estonian", Flag: "🇪🇪"},
	{Code: "fa", Name: "Persian", Flag: "🇮🇷"},
	{Code: "bn", Name: "Bengali", Flag: "🇧🇩"},
	// Sri Lanka, not India: Tamil is official in both, and 🇮🇳 is already
	// Hindi's. Flags stay unique across the table even for rows outside
	// langMap: they are a map key wherever detection does use them, and a
	// row that gains TitleAliases later must not silently shadow another.
	{Code: "ta", Name: "Tamil", Flag: "🇱🇰"},
	{Code: "kk", Name: "Kazakh", Flag: "🇰🇿"},
	{Code: "ka", Name: "Georgian", Flag: "🇬🇪"},
	{Code: "hy", Name: "Armenian", Flag: "🇦🇲"},
	{Code: "az", Name: "Azerbaijani", Flag: "🇦🇿"},
	// Andorra: the one state where Catalan is the sole official language,
	// and 🇪🇸 is already Spanish's (see the note on Tamil).
	{Code: "ca", Name: "Catalan", Flag: "🇦🇩"},
}

// langMap resolves a title token (alias / short code / flag emoji) to a
// Language. Built from TitleAliases alone, so a row with none is absent
// from it -- flag included: a language that cannot be read out of a title
// cannot be read out of one by its flag either, and adding the flag would
// make exactly the false positives TitleAliases exists to prevent.
var langMap = func() map[string]*Language {
	m := make(map[string]*Language, len(Languages)*4)
	for i := range Languages {
		l := &Languages[i]
		if !l.Detectable() {
			continue
		}
		for _, a := range l.TitleAliases {
			m[a] = l
		}
		m[l.Flag] = l
	}
	return m
}()

// langSkip mirrors LANG_SKIP in lang.js — short tokens that look like
// language codes but produce too many false positives.
var langSkip = map[string]bool{
	"no": true, // Norwegian conflicts with the English word "no"
}

// langSplitter mirrors the JS regex /[\s./()[\],|+]+/ used to tokenise
// stream titles in extractLanguages.
func splitTitle(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r',
			'.', '/', '(', ')', '[', ']', ',', '|', '+':
			return true
		}
		return false
	})
}

// ExtractLanguages detects language tags in a stream/torrent title using the
// same rules as Discover's extractLanguages() (assets/src/js/lib/discover/lang.js).
// Returns languages in first-seen order, deduplicated by Name.
func ExtractLanguages(title string) []*Language {
	if title == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []*Language
	for _, t := range splitTitle(title) {
		if t == "" {
			continue
		}
		lower := strings.ToLower(t)
		if langSkip[lower] {
			continue
		}
		if l, ok := langMap[lower]; ok && !seen[l.Name] {
			seen[l.Name] = true
			out = append(out, l)
		}
		// Voice-over abbreviations that only the Russian scene uses. A
		// rutracker release is routinely titled in transliterated English
		// with nothing but "AVO"/"MVO"/"DVO" to say what language it is in,
		// and a user who asked for Russian would otherwise have exactly
		// those releases filtered out.
		if ruVoiceOver[lower] {
			addLang(&out, seen, "ru")
		}
	}
	// Only when nothing was tagged explicitly: Cyrillic in the title is
	// itself the tag. Ukrainian-only letters mean Ukrainian, anything else
	// Cyrillic is Russian in practice on the trackers these titles come
	// from. Running this as a fallback rather than an addition keeps a
	// Ukrainian release from also counting as Russian.
	if len(out) == 0 && hasCyrillic(title) {
		if hasUkrainianLetters(title) {
			addLang(&out, seen, "uk")
		} else {
			addLang(&out, seen, "ru")
		}
	}
	return out
}

// ruVoiceOver are dubbing markers specific to the Russian scene: авторский,
// многоголосый and двухголосый voice-over. Deliberately excludes the bare
// "VO" and "DUB", which every scene uses.
var ruVoiceOver = map[string]bool{"avo": true, "mvo": true, "dvo": true}

func addLang(out *[]*Language, seen map[string]bool, code string) {
	l := LanguageByCode(code)
	if l == nil || seen[l.Name] {
		return
	}
	seen[l.Name] = true
	*out = append(*out, l)
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if r >= 0x0400 && r <= 0x04FF {
			return true
		}
	}
	return false
}

// hasUkrainianLetters looks for the letters Ukrainian has and Russian does
// not, in either case.
func hasUkrainianLetters(s string) bool {
	return strings.ContainsAny(s, "іїєґІЇЄҐ")
}

// LanguageByCode returns the Language entry with the given 2-letter code, or nil.
func LanguageByCode(code string) *Language {
	for i := range Languages {
		if Languages[i].Code == code {
			return &Languages[i]
		}
	}
	return nil
}
