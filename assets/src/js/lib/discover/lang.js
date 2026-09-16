// Language detection for stream titles.
//
// Mirror of services/stremio/lang.go (code, name, flag, titleAliases):
// TestLangJSMirrorsTheGoTable parses this file and compares the two, so an
// entry added on one side and not the other is a red test rather than a
// surface that quietly disagrees with the server.
//
// `titleAliases` are the tokens that mean this language IN A TITLE, which
// is a different question from what the language is called. A row with
// none is nameable and choosable but never detected -- the safe default
// for tokens nobody has measured against real release vocabulary, because
// a wrong detection pre-empts the Cyrillic fallback below and, server
// side, drops the release out of an exclusive filter.
const LANGUAGES = [
    { code: 'en', name: 'English',     flag: '🇬🇧', titleAliases: ['eng', 'english', 'en'], extraFlags: ['🇺🇸', '🇦🇺'] },
    { code: 'ru', name: 'Russian',     flag: '🇷🇺', titleAliases: ['rus', 'russian', 'ru', 'рус', 'русский'] },
    { code: 'uk', name: 'Ukrainian',   flag: '🇺🇦', titleAliases: ['ukr', 'ukrainian', 'ua', 'укр', 'українська'] },
    { code: 'it', name: 'Italian',     flag: '🇮🇹', titleAliases: ['ita', 'italian', 'it'] },
    { code: 'fr', name: 'French',      flag: '🇫🇷', titleAliases: ['fre', 'french', 'fr'] },
    { code: 'es', name: 'Spanish',     flag: '🇪🇸', titleAliases: ['spa', 'spanish', 'es'] },
    { code: 'de', name: 'German',      flag: '🇩🇪', titleAliases: ['ger', 'german', 'de'] },
    { code: 'pt', name: 'Portuguese',  flag: '🇧🇷', titleAliases: ['por', 'portuguese', 'pt'], extraFlags: ['🇵🇹'] },
    { code: 'cs', name: 'Czech',       flag: '🇨🇿', titleAliases: ['cze', 'czech', 'cz'] },
    { code: 'pl', name: 'Polish',      flag: '🇵🇱', titleAliases: ['pol', 'polish', 'pl'] },
    { code: 'nl', name: 'Dutch',       flag: '🇳🇱', titleAliases: ['dut', 'dutch', 'nl'] },
    { code: 'ja', name: 'Japanese',    flag: '🇯🇵', titleAliases: ['jpn', 'japanese', 'ja'] },
    { code: 'ko', name: 'Korean',      flag: '🇰🇷', titleAliases: ['kor', 'korean', 'ko'] },
    { code: 'zh', name: 'Chinese',     flag: '🇨🇳', titleAliases: ['chi', 'chinese', 'zh'] },
    { code: 'ar', name: 'Arabic',      flag: '🇸🇦', titleAliases: ['ara', 'arabic', 'ar'] },
    { code: 'hi', name: 'Hindi',       flag: '🇮🇳', titleAliases: ['hin', 'hindi', 'hi'] },
    { code: 'tr', name: 'Turkish',     flag: '🇹🇷', titleAliases: ['tur', 'turkish', 'tr'] },
    { code: 'sv', name: 'Swedish',     flag: '🇸🇪', titleAliases: ['swe', 'swedish', 'sv'] },
    { code: 'no', name: 'Norwegian',   flag: '🇳🇴', titleAliases: ['nor', 'norwegian', 'no'] },
    { code: 'da', name: 'Danish',      flag: '🇩🇰', titleAliases: ['dan', 'danish', 'da'] },
    { code: 'fi', name: 'Finnish',     flag: '🇫🇮', titleAliases: ['fin', 'finnish', 'fi'] },
    { code: 'ro', name: 'Romanian',    flag: '🇷🇴', titleAliases: ['rum', 'romanian', 'ro'] },
    { code: 'hu', name: 'Hungarian',   flag: '🇭🇺', titleAliases: ['hun', 'hungarian', 'hu'] },
    { code: 'el', name: 'Greek',       flag: '🇬🇷', titleAliases: ['gre', 'greek', 'el'] },
    { code: 'bg', name: 'Bulgarian',   flag: '🇧🇬', titleAliases: ['bul', 'bulgarian', 'bg'] },
    { code: 'hr', name: 'Croatian',    flag: '🇭🇷', titleAliases: ['hrv', 'croatian', 'hr'] },
    { code: 'sr', name: 'Serbian',     flag: '🇷🇸', titleAliases: ['srp', 'serbian', 'sr'] },
    { code: 'sl', name: 'Slovenian',   flag: '🇸🇮', titleAliases: ['slv', 'slovenian', 'sl'] },
    { code: 'he', name: 'Hebrew',      flag: '🇮🇱', titleAliases: ['heb', 'hebrew', 'he'] },
    { code: 'th', name: 'Thai',        flag: '🇹🇭', titleAliases: ['tha', 'thai', 'th'] },
    { code: 'vi', name: 'Vietnamese',  flag: '🇻🇳', titleAliases: ['vie', 'vietnamese', 'vi'] },
    { code: 'id', name: 'Indonesian',  flag: '🇮🇩', titleAliases: ['ind', 'indonesian', 'id'] },
    { code: 'ms', name: 'Malay',       flag: '🇲🇾', titleAliases: ['may', 'malay', 'ms'] },
    // Appended 2026-09-16, mirroring services/stremio/lang.go. They carry
    // NO titleAliases, exactly as the Go rows do: they exist to be named
    // and chosen, never found in a title (see the note there -- 'KAT' is
    // KickassTorrents, not Georgian). A Go test compares this list with
    // the Go one field by field, so the two cannot drift.
    { code: 'sk', name: 'Slovak',      flag: '🇸🇰', titleAliases: [] },
    { code: 'lt', name: 'Lithuanian',  flag: '🇱🇹', titleAliases: [] },
    { code: 'lv', name: 'Latvian',     flag: '🇱🇻', titleAliases: [] },
    { code: 'et', name: 'Estonian',    flag: '🇪🇪', titleAliases: [] },
    { code: 'fa', name: 'Persian',     flag: '🇮🇷', titleAliases: [] },
    { code: 'bn', name: 'Bengali',     flag: '🇧🇩', titleAliases: [] },
    { code: 'ta', name: 'Tamil',       flag: '🇱🇰', titleAliases: [] },
    { code: 'kk', name: 'Kazakh',      flag: '🇰🇿', titleAliases: [] },
    { code: 'ka', name: 'Georgian',    flag: '🇬🇪', titleAliases: [] },
    { code: 'hy', name: 'Armenian',    flag: '🇦🇲', titleAliases: [] },
    { code: 'az', name: 'Azerbaijani', flag: '🇦🇿', titleAliases: [] },
    { code: 'ca', name: 'Catalan',     flag: '🇦🇩', titleAliases: [] },
    // Latino has no Go counterpart and no code: it is a release-title tag,
    // not a language of the settings list, so the mirror test skips it by
    // the empty `code`. Its flag is deliberately Spanish's -- see the
    // first-wins registration below, which is what keeps a bare 🇪🇸
    // resolving to Spanish.
    { code: '', name: 'Latino', flag: '🇪🇸', titleAliases: ['lat', 'latino'], extraFlags: ['🇲🇽', '🇦🇷'] },
];

// LANG_MAP resolves a title token (alias / short code / flag emoji) to a
// language. Built from titleAliases alone, so a row with none is absent
// from it, flag included -- the Go side does the same.
//
// First key wins. Latino and Spanish share 🇪🇸, and last-wins made a bare
// 🇪🇸 in a title resolve to Latino, shadowing Spanish since the row was
// added; 🇲🇽 and 🇦🇷 still reach Latino, which is the distinction that
// tag is for.
export const LANG_MAP = {};
for (const lang of LANGUAGES) {
    if (!lang.titleAliases || lang.titleAliases.length === 0) continue;
    const entry = { flag: lang.flag, name: lang.name };
    const put = (k) => { if (!(k in LANG_MAP)) LANG_MAP[k] = entry; };
    for (const a of lang.titleAliases) put(a);
    put(lang.flag);
    if (lang.extraFlags) for (const f of lang.extraFlags) put(f);
}

// Words to skip -- they are not languages even though they match short codes
const LANG_SKIP = new Set([
    'no', // Norwegian conflicts with "no" (e.g. "No torrent")
]);

// supportsFlagEmoji reports whether the platform actually renders
// regional-indicator flag emoji. Windows (every browser except Firefox,
// which ships its own emoji font) falls back to letter pairs like "RU",
// so flag-decorated chips look broken there. Detection: draw a flag on
// a canvas and look for a colored (non-grayscale) pixel — the letter
// fallback is monochrome. Result is computed once per page.
let flagEmojiSupport = null;
export function supportsFlagEmoji() {
    if (flagEmojiSupport != null) return flagEmojiSupport;
    try {
        const canvas = document.createElement('canvas');
        canvas.width = 20;
        canvas.height = 20;
        const ctx = canvas.getContext('2d', { willReadFrequently: true });
        ctx.textBaseline = 'top';
        ctx.font = '16px sans-serif';
        ctx.fillText('🇷🇺', 0, 0);
        const data = ctx.getImageData(0, 0, 20, 20).data;
        flagEmojiSupport = false;
        for (let i = 0; i < data.length; i += 4) {
            if (data[i + 3] > 0 && !(data[i] === data[i + 1] && data[i + 1] === data[i + 2])) {
                flagEmojiSupport = true;
                break;
            }
        }
    } catch {
        flagEmojiSupport = true; // can't tell — assume the common case
    }
    return flagEmojiSupport;
}

const RU_VOICE_OVER = new Set(['avo', 'mvo', 'dvo']);

export function extractLanguages(title) {
    if (!title) return [];
    const found = {};
    const tokens = title.split(/[\s./()[\],|+]+/);
    for (const t of tokens) {
        const trimmed = t.trim();
        if (!trimmed) continue;
        const lower = trimmed.toLowerCase();
        if (LANG_SKIP.has(lower)) continue;
        const lang = LANG_MAP[lower];
        if (lang && !found[lang.name]) {
            found[lang.name] = lang;
        }
        // Voice-over abbreviations only the Russian scene uses. A tracker
        // release is routinely titled in transliterated English with
        // nothing but "AVO"/"MVO"/"DVO" to say what language it is in.
        if (RU_VOICE_OVER.has(lower)) {
            const ru = LANG_MAP['ru'];
            if (ru && !found[ru.name]) found[ru.name] = ru;
        }
    }
    // Only when nothing was tagged explicitly: Cyrillic is itself the tag.
    // Ukrainian-only letters mean Ukrainian, anything else Cyrillic is
    // Russian on the trackers these titles come from. Keep in sync with
    // ExtractLanguages in services/stremio/lang.go.
    if (Object.keys(found).length === 0 && /[\u0400-\u04FF]/.test(title)) {
        const lang = LANG_MAP[/[іїєґІЇЄҐ]/.test(title) ? 'ukr' : 'ru'];
        if (lang) found[lang.name] = lang;
    }
    return Object.values(found);
}
