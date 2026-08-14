package stremio

import "strings"

// Language describes a language entry exposed to the Stremio settings UI
// and used for filtering stream titles. Mirrors the LANGUAGES list in
// assets/src/js/lib/discover/lang.js so the Stremio addon and the
// Discover stream modal share the same detection rules.
type Language struct {
	Code    string
	Name    string
	Flag    string
	Aliases []string
}

// Languages is the canonical, ordered list of supported languages. Keep in
// sync with assets/src/js/lib/discover/lang.js.
var Languages = []Language{
	{Code: "en", Name: "English", Flag: "🇬🇧", Aliases: []string{"eng", "english", "en"}},
	{Code: "ru", Name: "Russian", Flag: "🇷🇺", Aliases: []string{"rus", "russian", "ru", "рус", "русский"}},
	{Code: "uk", Name: "Ukrainian", Flag: "🇺🇦", Aliases: []string{"ukr", "ukrainian", "ua", "укр", "українська"}},
	{Code: "it", Name: "Italian", Flag: "🇮🇹", Aliases: []string{"ita", "italian", "it"}},
	{Code: "fr", Name: "French", Flag: "🇫🇷", Aliases: []string{"fre", "french", "fr"}},
	{Code: "es", Name: "Spanish", Flag: "🇪🇸", Aliases: []string{"spa", "spanish", "es"}},
	{Code: "de", Name: "German", Flag: "🇩🇪", Aliases: []string{"ger", "german", "de"}},
	{Code: "pt", Name: "Portuguese", Flag: "🇧🇷", Aliases: []string{"por", "portuguese", "pt"}},
	{Code: "cs", Name: "Czech", Flag: "🇨🇿", Aliases: []string{"cze", "czech", "cz"}},
	{Code: "pl", Name: "Polish", Flag: "🇵🇱", Aliases: []string{"pol", "polish", "pl"}},
	{Code: "nl", Name: "Dutch", Flag: "🇳🇱", Aliases: []string{"dut", "dutch", "nl"}},
	{Code: "ja", Name: "Japanese", Flag: "🇯🇵", Aliases: []string{"jpn", "japanese", "ja"}},
	{Code: "ko", Name: "Korean", Flag: "🇰🇷", Aliases: []string{"kor", "korean", "ko"}},
	{Code: "zh", Name: "Chinese", Flag: "🇨🇳", Aliases: []string{"chi", "chinese", "zh"}},
	{Code: "ar", Name: "Arabic", Flag: "🇸🇦", Aliases: []string{"ara", "arabic", "ar"}},
	{Code: "hi", Name: "Hindi", Flag: "🇮🇳", Aliases: []string{"hin", "hindi", "hi"}},
	{Code: "tr", Name: "Turkish", Flag: "🇹🇷", Aliases: []string{"tur", "turkish", "tr"}},
	{Code: "sv", Name: "Swedish", Flag: "🇸🇪", Aliases: []string{"swe", "swedish", "sv"}},
	{Code: "no", Name: "Norwegian", Flag: "🇳🇴", Aliases: []string{"nor", "norwegian", "no"}},
	{Code: "da", Name: "Danish", Flag: "🇩🇰", Aliases: []string{"dan", "danish", "da"}},
	{Code: "fi", Name: "Finnish", Flag: "🇫🇮", Aliases: []string{"fin", "finnish", "fi"}},
	{Code: "ro", Name: "Romanian", Flag: "🇷🇴", Aliases: []string{"rum", "romanian", "ro"}},
	{Code: "hu", Name: "Hungarian", Flag: "🇭🇺", Aliases: []string{"hun", "hungarian", "hu"}},
	{Code: "el", Name: "Greek", Flag: "🇬🇷", Aliases: []string{"gre", "greek", "el"}},
	{Code: "bg", Name: "Bulgarian", Flag: "🇧🇬", Aliases: []string{"bul", "bulgarian", "bg"}},
	{Code: "hr", Name: "Croatian", Flag: "🇭🇷", Aliases: []string{"hrv", "croatian", "hr"}},
	{Code: "sr", Name: "Serbian", Flag: "🇷🇸", Aliases: []string{"srp", "serbian", "sr"}},
	{Code: "sl", Name: "Slovenian", Flag: "🇸🇮", Aliases: []string{"slv", "slovenian", "sl"}},
	{Code: "he", Name: "Hebrew", Flag: "🇮🇱", Aliases: []string{"heb", "hebrew", "he"}},
	{Code: "th", Name: "Thai", Flag: "🇹🇭", Aliases: []string{"tha", "thai", "th"}},
	{Code: "vi", Name: "Vietnamese", Flag: "🇻🇳", Aliases: []string{"vie", "vietnamese", "vi"}},
	{Code: "id", Name: "Indonesian", Flag: "🇮🇩", Aliases: []string{"ind", "indonesian", "id"}},
	{Code: "ms", Name: "Malay", Flag: "🇲🇾", Aliases: []string{"may", "malay", "ms"}},
}

// langMap resolves an alias / 2-letter code / flag emoji to a Language entry.
var langMap = func() map[string]*Language {
	m := make(map[string]*Language, len(Languages)*4)
	for i := range Languages {
		l := &Languages[i]
		for _, a := range l.Aliases {
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
