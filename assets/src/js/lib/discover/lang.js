// Language detection for stream titles

export const LANG_MAP = {
    'eng': { flag: '\u{1F1EC}\u{1F1E7}', name: 'English' },
    'english': { flag: '\u{1F1EC}\u{1F1E7}', name: 'English' },
    'en': { flag: '\u{1F1EC}\u{1F1E7}', name: 'English' },
    'rus': { flag: '\u{1F1F7}\u{1F1FA}', name: 'Russian' },
    'russian': { flag: '\u{1F1F7}\u{1F1FA}', name: 'Russian' },
    'ru': { flag: '\u{1F1F7}\u{1F1FA}', name: 'Russian' },
    'ukr': { flag: '\u{1F1FA}\u{1F1E6}', name: 'Ukrainian' },
    'ukrainian': { flag: '\u{1F1FA}\u{1F1E6}', name: 'Ukrainian' },
    'ua': { flag: '\u{1F1FA}\u{1F1E6}', name: 'Ukrainian' },
    'ita': { flag: '\u{1F1EE}\u{1F1F9}', name: 'Italian' },
    'italian': { flag: '\u{1F1EE}\u{1F1F9}', name: 'Italian' },
    'it': { flag: '\u{1F1EE}\u{1F1F9}', name: 'Italian' },
    'fre': { flag: '\u{1F1EB}\u{1F1F7}', name: 'French' },
    'french': { flag: '\u{1F1EB}\u{1F1F7}', name: 'French' },
    'fr': { flag: '\u{1F1EB}\u{1F1F7}', name: 'French' },
    'spa': { flag: '\u{1F1EA}\u{1F1F8}', name: 'Spanish' },
    'spanish': { flag: '\u{1F1EA}\u{1F1F8}', name: 'Spanish' },
    'es': { flag: '\u{1F1EA}\u{1F1F8}', name: 'Spanish' },
    'ger': { flag: '\u{1F1E9}\u{1F1EA}', name: 'German' },
    'german': { flag: '\u{1F1E9}\u{1F1EA}', name: 'German' },
    'de': { flag: '\u{1F1E9}\u{1F1EA}', name: 'German' },
    'por': { flag: '\u{1F1E7}\u{1F1F7}', name: 'Portuguese' },
    'portuguese': { flag: '\u{1F1E7}\u{1F1F7}', name: 'Portuguese' },
    'pt': { flag: '\u{1F1E7}\u{1F1F7}', name: 'Portuguese' },
    'cze': { flag: '\u{1F1E8}\u{1F1FF}', name: 'Czech' },
    'czech': { flag: '\u{1F1E8}\u{1F1FF}', name: 'Czech' },
    'cz': { flag: '\u{1F1E8}\u{1F1FF}', name: 'Czech' },
    'pol': { flag: '\u{1F1F5}\u{1F1F1}', name: 'Polish' },
    'polish': { flag: '\u{1F1F5}\u{1F1F1}', name: 'Polish' },
    'pl': { flag: '\u{1F1F5}\u{1F1F1}', name: 'Polish' },
    'dut': { flag: '\u{1F1F3}\u{1F1F1}', name: 'Dutch' },
    'dutch': { flag: '\u{1F1F3}\u{1F1F1}', name: 'Dutch' },
    'nl': { flag: '\u{1F1F3}\u{1F1F1}', name: 'Dutch' },
    'jpn': { flag: '\u{1F1EF}\u{1F1F5}', name: 'Japanese' },
    'japanese': { flag: '\u{1F1EF}\u{1F1F5}', name: 'Japanese' },
    'ja': { flag: '\u{1F1EF}\u{1F1F5}', name: 'Japanese' },
    'kor': { flag: '\u{1F1F0}\u{1F1F7}', name: 'Korean' },
    'korean': { flag: '\u{1F1F0}\u{1F1F7}', name: 'Korean' },
    'ko': { flag: '\u{1F1F0}\u{1F1F7}', name: 'Korean' },
    'chi': { flag: '\u{1F1E8}\u{1F1F3}', name: 'Chinese' },
    'chinese': { flag: '\u{1F1E8}\u{1F1F3}', name: 'Chinese' },
    'zh': { flag: '\u{1F1E8}\u{1F1F3}', name: 'Chinese' },
    'ara': { flag: '\u{1F1F8}\u{1F1E6}', name: 'Arabic' },
    'arabic': { flag: '\u{1F1F8}\u{1F1E6}', name: 'Arabic' },
    'ar': { flag: '\u{1F1F8}\u{1F1E6}', name: 'Arabic' },
    'hin': { flag: '\u{1F1EE}\u{1F1F3}', name: 'Hindi' },
    'hindi': { flag: '\u{1F1EE}\u{1F1F3}', name: 'Hindi' },
    'hi': { flag: '\u{1F1EE}\u{1F1F3}', name: 'Hindi' },
    'tur': { flag: '\u{1F1F9}\u{1F1F7}', name: 'Turkish' },
    'turkish': { flag: '\u{1F1F9}\u{1F1F7}', name: 'Turkish' },
    'tr': { flag: '\u{1F1F9}\u{1F1F7}', name: 'Turkish' },
    'swe': { flag: '\u{1F1F8}\u{1F1EA}', name: 'Swedish' },
    'swedish': { flag: '\u{1F1F8}\u{1F1EA}', name: 'Swedish' },
    'sv': { flag: '\u{1F1F8}\u{1F1EA}', name: 'Swedish' },
    'nor': { flag: '\u{1F1F3}\u{1F1F4}', name: 'Norwegian' },
    'norwegian': { flag: '\u{1F1F3}\u{1F1F4}', name: 'Norwegian' },
    'no': { flag: '\u{1F1F3}\u{1F1F4}', name: 'Norwegian' },
    'dan': { flag: '\u{1F1E9}\u{1F1F0}', name: 'Danish' },
    'danish': { flag: '\u{1F1E9}\u{1F1F0}', name: 'Danish' },
    'da': { flag: '\u{1F1E9}\u{1F1F0}', name: 'Danish' },
    'fin': { flag: '\u{1F1EB}\u{1F1EE}', name: 'Finnish' },
    'finnish': { flag: '\u{1F1EB}\u{1F1EE}', name: 'Finnish' },
    'fi': { flag: '\u{1F1EB}\u{1F1EE}', name: 'Finnish' },
    'rum': { flag: '\u{1F1F7}\u{1F1F4}', name: 'Romanian' },
    'romanian': { flag: '\u{1F1F7}\u{1F1F4}', name: 'Romanian' },
    'ro': { flag: '\u{1F1F7}\u{1F1F4}', name: 'Romanian' },
    'hun': { flag: '\u{1F1ED}\u{1F1FA}', name: 'Hungarian' },
    'hungarian': { flag: '\u{1F1ED}\u{1F1FA}', name: 'Hungarian' },
    'hu': { flag: '\u{1F1ED}\u{1F1FA}', name: 'Hungarian' },
    'gre': { flag: '\u{1F1EC}\u{1F1F7}', name: 'Greek' },
    'greek': { flag: '\u{1F1EC}\u{1F1F7}', name: 'Greek' },
    'el': { flag: '\u{1F1EC}\u{1F1F7}', name: 'Greek' },
    'bul': { flag: '\u{1F1E7}\u{1F1EC}', name: 'Bulgarian' },
    'bulgarian': { flag: '\u{1F1E7}\u{1F1EC}', name: 'Bulgarian' },
    'bg': { flag: '\u{1F1E7}\u{1F1EC}', name: 'Bulgarian' },
    'hrv': { flag: '\u{1F1ED}\u{1F1F7}', name: 'Croatian' },
    'croatian': { flag: '\u{1F1ED}\u{1F1F7}', name: 'Croatian' },
    'hr': { flag: '\u{1F1ED}\u{1F1F7}', name: 'Croatian' },
    'srp': { flag: '\u{1F1F7}\u{1F1F8}', name: 'Serbian' },
    'serbian': { flag: '\u{1F1F7}\u{1F1F8}', name: 'Serbian' },
    'sr': { flag: '\u{1F1F7}\u{1F1F8}', name: 'Serbian' },
    'slv': { flag: '\u{1F1F8}\u{1F1EE}', name: 'Slovenian' },
    'slovenian': { flag: '\u{1F1F8}\u{1F1EE}', name: 'Slovenian' },
    'sl': { flag: '\u{1F1F8}\u{1F1EE}', name: 'Slovenian' },
    'heb': { flag: '\u{1F1EE}\u{1F1F1}', name: 'Hebrew' },
    'hebrew': { flag: '\u{1F1EE}\u{1F1F1}', name: 'Hebrew' },
    'he': { flag: '\u{1F1EE}\u{1F1F1}', name: 'Hebrew' },
    'tha': { flag: '\u{1F1F9}\u{1F1ED}', name: 'Thai' },
    'thai': { flag: '\u{1F1F9}\u{1F1ED}', name: 'Thai' },
    'th': { flag: '\u{1F1F9}\u{1F1ED}', name: 'Thai' },
    'vie': { flag: '\u{1F1FB}\u{1F1F3}', name: 'Vietnamese' },
    'vietnamese': { flag: '\u{1F1FB}\u{1F1F3}', name: 'Vietnamese' },
    'vi': { flag: '\u{1F1FB}\u{1F1F3}', name: 'Vietnamese' },
    'ind': { flag: '\u{1F1EE}\u{1F1E9}', name: 'Indonesian' },
    'indonesian': { flag: '\u{1F1EE}\u{1F1E9}', name: 'Indonesian' },
    'id': { flag: '\u{1F1EE}\u{1F1E9}', name: 'Indonesian' },
    'may': { flag: '\u{1F1F2}\u{1F1FE}', name: 'Malay' },
    'malay': { flag: '\u{1F1F2}\u{1F1FE}', name: 'Malay' },
    'ms': { flag: '\u{1F1F2}\u{1F1FE}', name: 'Malay' },
    'lat': { flag: '\u{1F1EA}\u{1F1F8}', name: 'Latino' },
    'latino': { flag: '\u{1F1EA}\u{1F1F8}', name: 'Latino' },
};

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
