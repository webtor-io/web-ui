// "Does anything here match what this account asked for" — the question the
// stream list needs in order to say "nothing does".
//
// Discover deliberately shows every stream and lets the filter chips narrow
// them; the account's stream settings (profile → preferred resolutions and
// language) never touch that list. So a user whose sources returned only
// 4K German rips sees a full list and no explanation. This module supplies
// the missing signal, and nothing else: it hides no stream.
//
// The vocabulary is the profile's: 4k / 1080p / 720p / other. The server
// half of the same rule lives in services/release_subscription (the poller
// filters a subscription's finds by the same two settings).

import { LANG_MAP } from './lang.js';

// getStreamPrefs reads what the page bootstrap put there. Absent or empty
// means no preference, which every stream satisfies.
export function getStreamPrefs() {
    const raw = (typeof window !== 'undefined' && window._streamPrefs) || {};
    const language = typeof raw.language === 'string' ? raw.language : '';
    return {
        resolutions: Array.isArray(raw.resolutions) ? raw.resolutions : [],
        language,
        // The setting is a code ("ru"); the titles yield display names
        // ("Russian"). Resolve once here so the per-stream check is a
        // string compare.
        languageName: language ? (LANG_MAP[language] || {}).name || '' : '',
    };
}

export function hasPrefs(prefs) {
    return !!prefs && ((prefs.resolutions && prefs.resolutions.length > 0) || !!prefs.language);
}

// resolutionOf maps a parsed stream's labels onto the profile's vocabulary.
// Anything the parser could not place is "other", which the profile offers
// as its own choice.
export function resolutionOf(parsed) {
    const labels = (parsed && parsed.labels) || [];
    for (const label of labels) {
        const l = String(label).toLowerCase();
        if (l === '4k' || l === '2160p') return '4k';
        if (l === '1080p') return '1080p';
        if (l === '720p') return '720p';
    }
    return 'other';
}

// matchesPrefs answers for one stream. langs is the list of language names
// already extracted from its title (extractLanguages), passed in rather than
// re-derived so the caller keeps its single pass over the list.
export function matchesPrefs(parsed, langs, prefs) {
    if (!hasPrefs(prefs)) return true;
    if (prefs.resolutions.length > 0 && !prefs.resolutions.includes(resolutionOf(parsed))) {
        return false;
    }
    if (prefs.language) {
        // A code the client's table does not know is not a filter — the
        // list must not go quiet because of a setting we cannot read.
        if (!prefs.languageName) return true;
        if (!(langs || []).includes(prefs.languageName)) return false;
    }
    return true;
}

// noneMatchPrefs is the whole point: there are streams, and not one of them
// is what the account asked for.
export function noneMatchPrefs(parsedList, langsList, prefs) {
    if (!hasPrefs(prefs)) return false;
    if (!parsedList || parsedList.length === 0) return false;
    for (let i = 0; i < parsedList.length; i++) {
        if (matchesPrefs(parsedList[i], langsList[i], prefs)) return false;
    }
    return true;
}
