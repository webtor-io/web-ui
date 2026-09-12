// Pure helpers behind the two Umami events that measure the subtitle
// ladder (spec: docs/superpowers/specs/2026-09-12-auto-subtitles-design.md).
// Levels: 0 user upload, 1 embedded, 2 sidecar in torrent / embed
// external, 3 OpenSubtitles matched by file hash, 4 OpenSubtitles by
// IMDb id. Lower is better; whisper (5) does not exist yet.

const LEVELS = ['0', '1', '2', '3', '4'];

function levelOf(track) {
    switch (track.provider) {
        case 'UserSubtitle': return '0';
        case 'MediaProbe': return '1';
        case 'ExportTag':
        case 'External': return '2';
        case 'OpenSubtitles': return track.source === 'hash' ? '3' : '4';
        default: return null;
    }
}

function baseLang(tag) {
    return String(tag || '').toLowerCase().split(/[-_]/)[0];
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
        .map(selectEventData);
}

export function selectEventData(el) {
    return {
        provider: el.getAttribute('data-provider') || '',
        srclang: el.getAttribute('data-srclang') || '',
        source: el.getAttribute('data-source') || '',
    };
}

export function resolveSubtitleLevel(tracks, uiLang) {
    let best = null;
    let hasUiLang = false;
    const ui = baseLang(uiLang);
    for (const t of tracks) {
        const l = levelOf(t);
        if (l !== null && (best === null || LEVELS.indexOf(l) < LEVELS.indexOf(best))) best = l;
        if (ui && baseLang(t.srclang) === ui) hasUiLang = true;
    }
    return { level: best === null ? 'none' : best, hasUiLang, count: tracks.length };
}
