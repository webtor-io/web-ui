// Language detection for stream titles

const LANGUAGES = [
    { name: 'English',    flag: '🇬🇧', aliases: ['eng', 'english', 'en'], extraFlags: ['🇺🇸', '🇦🇺'] },
    { name: 'Russian',    flag: '🇷🇺', aliases: ['rus', 'russian', 'ru'] },
    { name: 'Ukrainian',  flag: '🇺🇦', aliases: ['ukr', 'ukrainian', 'ua'] },
    { name: 'Italian',    flag: '🇮🇹', aliases: ['ita', 'italian', 'it'] },
    { name: 'French',     flag: '🇫🇷', aliases: ['fre', 'french', 'fr'] },
    { name: 'Spanish',    flag: '🇪🇸', aliases: ['spa', 'spanish', 'es'] },
    { name: 'German',     flag: '🇩🇪', aliases: ['ger', 'german', 'de'] },
    { name: 'Portuguese', flag: '🇧🇷', aliases: ['por', 'portuguese', 'pt'], extraFlags: ['🇵🇹'] },
    { name: 'Czech',      flag: '🇨🇿', aliases: ['cze', 'czech', 'cz'] },
    { name: 'Polish',     flag: '🇵🇱', aliases: ['pol', 'polish', 'pl'] },
    { name: 'Dutch',      flag: '🇳🇱', aliases: ['dut', 'dutch', 'nl'] },
    { name: 'Japanese',   flag: '🇯🇵', aliases: ['jpn', 'japanese', 'ja'] },
    { name: 'Korean',     flag: '🇰🇷', aliases: ['kor', 'korean', 'ko'] },
    { name: 'Chinese',    flag: '🇨🇳', aliases: ['chi', 'chinese', 'zh'] },
    { name: 'Arabic',     flag: '🇸🇦', aliases: ['ara', 'arabic', 'ar'] },
    { name: 'Hindi',      flag: '🇮🇳', aliases: ['hin', 'hindi', 'hi'] },
    { name: 'Turkish',    flag: '🇹🇷', aliases: ['tur', 'turkish', 'tr'] },
    { name: 'Swedish',    flag: '🇸🇪', aliases: ['swe', 'swedish', 'sv'] },
    { name: 'Norwegian',  flag: '🇳🇴', aliases: ['nor', 'norwegian', 'no'] },
    { name: 'Danish',     flag: '🇩🇰', aliases: ['dan', 'danish', 'da'] },
    { name: 'Finnish',    flag: '🇫🇮', aliases: ['fin', 'finnish', 'fi'] },
    { name: 'Romanian',   flag: '🇷🇴', aliases: ['rum', 'romanian', 'ro'] },
    { name: 'Hungarian',  flag: '🇭🇺', aliases: ['hun', 'hungarian', 'hu'] },
    { name: 'Greek',      flag: '🇬🇷', aliases: ['gre', 'greek', 'el'] },
    { name: 'Bulgarian',  flag: '🇧🇬', aliases: ['bul', 'bulgarian', 'bg'] },
    { name: 'Croatian',   flag: '🇭🇷', aliases: ['hrv', 'croatian', 'hr'] },
    { name: 'Serbian',    flag: '🇷🇸', aliases: ['srp', 'serbian', 'sr'] },
    { name: 'Slovenian',  flag: '🇸🇮', aliases: ['slv', 'slovenian', 'sl'] },
    { name: 'Hebrew',     flag: '🇮🇱', aliases: ['heb', 'hebrew', 'he'] },
    { name: 'Thai',       flag: '🇹🇭', aliases: ['tha', 'thai', 'th'] },
    { name: 'Vietnamese', flag: '🇻🇳', aliases: ['vie', 'vietnamese', 'vi'] },
    { name: 'Indonesian', flag: '🇮🇩', aliases: ['ind', 'indonesian', 'id'] },
    { name: 'Malay',      flag: '🇲🇾', aliases: ['may', 'malay', 'ms'] },
    { name: 'Latino',     flag: '🇪🇸', aliases: ['lat', 'latino'], extraFlags: ['🇲🇽', '🇦🇷'] },
];

export const LANG_MAP = {};
for (const lang of LANGUAGES) {
    const entry = { flag: lang.flag, name: lang.name };
    for (const a of lang.aliases) LANG_MAP[a] = entry;
    LANG_MAP[lang.flag] = entry;
    if (lang.extraFlags) for (const f of lang.extraFlags) LANG_MAP[f] = entry;
}

// Words to skip -- they are not languages even though they match short codes
const LANG_SKIP = new Set([
    'no', // Norwegian conflicts with "no" (e.g. "No torrent")
]);

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
    }
    return Object.values(found);
}
