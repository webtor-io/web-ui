// Pure helpers behind the Umami events that measure the subtitle
// ladder (spec: docs/superpowers/specs/2026-09-12-auto-subtitles-design.md).
// Levels: 0 user upload, 1 embedded, 2 sidecar in torrent / embed
// external, 3 OpenSubtitles matched by file hash, 4 OpenSubtitles by
// IMDb id, 5 AI translation of another track. Lower is better; whisper
// transcription (phase 3) will take 6.

import { baseLang } from './subtitle-rules.js';

const LEVELS = ['0', '1', '2', '3', '4', '5'];

function levelOf(track) {
    switch (track.provider) {
        case 'UserSubtitle': return '0';
        case 'MediaProbe': return '1';
        case 'ExportTag':
        case 'External': return '2';
        case 'OpenSubtitles': return track.source === 'hash' ? '3' : '4';
        case 'Translated': return '5';
        default: return null;
    }
}

export function readTracks(modal) {
    if (!modal) return [];
    // Not `li.subtitle`: user-uploaded subtitles render the `.subtitle`
    // marker on a `<div>` inside a plain `<li>`
    // (templates/partials/action/user_subtitles.html), while embedded/
    // sidecar/OpenSubtitles tracks render it directly on the `<li>`
    // (templates/views/action/stream_video.html). `[data-provider]` is the
    // trait both share.
    return Array.from(modal.querySelectorAll('.subtitle[data-provider]'))
        .filter((el) => el.getAttribute('data-id') !== 'none')
        .map(trackData);
}

// trackData is the full read of a list item — everything the default
// rule (subtitle-rules.js) and the progress wiring need. The narrower
// selectEventData below is what actually ships to Umami.
function trackData(el) {
    const rank = parseInt(el.getAttribute('data-rank') || '', 10);
    return {
        id: el.getAttribute('data-id') || '',
        provider: el.getAttribute('data-provider') || '',
        srclang: el.getAttribute('data-srclang') || '',
        source: el.getAttribute('data-source') || '',
        badge: el.getAttribute('data-badge') || '',
        // Unknown rank sorts last, same as the server's rankUnknown.
        rank: Number.isFinite(rank) ? rank : 9,
        forced: el.getAttribute('data-forced') === 'true',
        locked: el.getAttribute('data-locked') === 'true',
        isDefault: el.getAttribute('data-default') === 'true',
        // The viewer's own earlier choice, as opposed to a ladder pick.
        saved: el.getAttribute('data-saved') === 'true',
        sourceBadge: el.getAttribute('data-source-badge') || '',
    };
}

export function selectEventData(el) {
    return {
        provider: el.getAttribute('data-provider') || '',
        srclang: el.getAttribute('data-srclang') || '',
        source: el.getAttribute('data-source') || '',
        badge: el.getAttribute('data-badge') || '',
    };
}

// resolveSubtitleLevel summarises what the viewer actually got.
// `needed` separates "no subtitles offered" from "no subtitles wanted":
// when the audio is already in the language the viewer wants to read,
// an empty list is the right answer, and counting it as a miss would
// bury the real ones. That language is the preferred content language
// (`data-preferred-lang`, what the ladder itself ran on); the UI
// language stands in only when no preference is configured, so the two
// halves of the same question are never asked of different languages.
export function resolveSubtitleLevel(tracks, uiLang, { audioLang = '', preferredLang = '' } = {}) {
    let best = null;
    let hasUiLang = false;
    let badge = '';
    const ui = baseLang(uiLang);
    for (const t of tracks) {
        const l = levelOf(t);
        if (l !== null && (best === null || LEVELS.indexOf(l) < LEVELS.indexOf(best))) best = l;
        if (ui && baseLang(t.srclang) === ui) hasUiLang = true;
        if (t.isDefault && !badge) badge = t.badge || '';
    }
    return {
        level: best === null ? 'none' : best,
        hasUiLang,
        count: tracks.length,
        badge,
        // Unknown audio language ⇒ assume subtitles are needed.
        needed: !audioLang || baseLang(audioLang) !== (baseLang(preferredLang) || ui),
        translated: badge === 'ai',
    };
}
