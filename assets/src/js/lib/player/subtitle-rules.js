// Client half of the subtitle default rule. The server picks the default
// at render time (handlers/action/helper.go, selectListItem); the client
// re-runs the rule when the viewer switches the audio track, because the
// answer depends on the audio language and the server cannot know it will
// change.
//
// The ladder itself is NOT reimplemented here: the server renders its
// ladderRank as data-rank on every item (0 user upload, 1 embedded,
// 2 sidecar, 3 OpenSubtitles by hash, 4 OpenSubtitles by imdb id, 5 AI
// translation, 9 unknown) and this module only compares those numbers.
// One ladder, one place to change it.

// baseLang reduces a language tag to its base subtag. The server already
// canonizes 3-letter codes to 2-letter tags before rendering, so there is
// no ISO-639-2 map here — regional suffixes (pt-BR, pt_BR) are all that
// is left to strip.
export function baseLang(tag) {
    return String(tag || '').toLowerCase().split(/[-_]/)[0];
}

// best returns the lowest-ranked (= most trusted) candidate in lang among
// forced or among full tracks, never mixing the two. Locked items are not
// candidates: a locked track cannot be turned on, so selecting it would
// leave the viewer with subtitles "on" and nothing on screen.
function best(tracks, lang, forced) {
    let pick = null;
    for (const t of tracks) {
        if (t.id === 'none' || t.locked) continue;
        if (!!t.forced !== forced) continue;
        if (baseLang(t.srclang) !== lang) continue;
        if (!pick || rankOf(t) < rankOf(pick)) pick = t;
    }
    return pick;
}

function rankOf(t) {
    const n = Number(t.rank);
    return Number.isFinite(n) ? n : 9;
}

// pickDefaultSubtitle answers "which subtitle should be on, given this
// audio track?" and returns the item id to activate, or 'none'.
//
// Audio already in the viewer's language: only a forced (signs-only)
// track is wanted — a full translation of dialogue the viewer
// understands is noise. Audio in another language: the best full track
// in the preferred language, AI translation included when nothing human
// is available.
export function pickDefaultSubtitle(tracks, audioLang, preferredLang) {
    const list = Array.isArray(tracks) ? tracks : [];
    const pref = baseLang(preferredLang);
    const audio = baseLang(audioLang);
    if (!pref) return 'none';
    if (audio && audio === pref) {
        const f = best(list, pref, true);
        return f ? f.id : 'none';
    }
    const full = best(list, pref, false);
    if (full) return full.id;
    // The preferred language yielded nothing activatable. Switching to
    // 'none' here would take subtitles away from a viewer who had them
    // before touching the audio menu, so the default the server already
    // chose stands.
    const current = list.find((t) => t.isDefault && t.id && t.id !== 'none');
    return current ? current.id : 'none';
}

// translationAction decides what selecting this item should do, given
// what already happened to it this page load. `status` is a Map of item
// id to 'running' (a run was started) or 'done' (a run reached the final
// progress).
//
//   'start'  — nothing has run for this item yet.
//   'resume' — a run was started and interrupted before it finished
//              (the viewer selected another track, which stops the poll).
//              Poll again, but do NOT re-emit subtitle-translate-start:
//              it is the same translation, and counting it twice would
//              inflate the start rate against a flat done rate.
//   'none'   — not a runnable AI item, or its translation already
//              finished: re-selecting it must neither poll nor report.
//
// Without the 'done' state a warm cache double-counts — the click runs
// the translation to completion and the engagement-gate auto-start finds
// the item still marked default with no poll running. Without 'resume' a
// viewer who switches away mid-translation and comes back is stuck with
// a frozen partial file for the life of the page.
export function translationAction(track, status) {
    if (!track || !track.id) return 'none';
    if (track.provider !== 'Translated') return 'none';
    // A locked item has no Src: there is nothing to poll.
    if (track.locked) return 'none';
    const state = status && typeof status.get === 'function' ? status.get(track.id) : undefined;
    if (state === 'done') return 'none';
    if (state === 'running') return 'resume';
    return 'start';
}

// hasSavedDefault reports whether the default the server rendered is the
// viewer's own earlier choice (ud.SubtitleID) rather than a ladder pick.
// The audio-switch rule must leave a saved choice alone — re-deciding
// over it would turn off subtitles the viewer explicitly asked for.
export function hasSavedDefault(tracks) {
    return (Array.isArray(tracks) ? tracks : []).some((t) => t.saved && t.isDefault);
}
