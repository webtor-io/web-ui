// Where the credits begin, guessed from subtitle timings: dialogue ends, and
// what follows is credits. Timings do not depend on the language, so any
// whole-file subtitle track will do, shown or not.
//
// It moves the "up next" card earlier -- from "the last ten seconds" to "when
// the talking stops" -- and nothing else. The move to the next file still
// happens on `ended` or on the viewer's click: a wrong guess then costs a card
// that came early, never a cut episode or a lost post-credits scene.
//
// Where the guess is wrong, and what is done about it:
//   - a scene after the credits: its lines are the last cues, so the guess
//     lands at the very end and is discarded (too late to be worth anything);
//   - a translator's signature inside the credits ("Subtitles by ..."): one or
//     two short cues after a long silence. Dropped -- see SIGNATURE_*. A real
//     post-credits scene is more than two lines and survives this;
//   - subtitles for another cut, or a truncated file: the last line falls
//     absurdly early or past the end. Discarded by the range checks.
// Anything discarded returns null and the card keeps its 25-second rule.

export const MIN_CUES = 20;              // fewer is not a transcript
export const MAX_CREDITS_S = 600;        // credits longer than this are not credits
export const MIN_GAIN_S = 25;            // credits shorter than this are not worth a separate guess
export const AFTER_LAST_LINE_S = 3;      // let the last line leave the screen
export const SIGNATURE_GAP_S = 60;       // silence before a translator's signature
export const SIGNATURE_MAX_CUES = 2;
export const SIGNATURE_MAX_S = 15;

const TIME = /(?:(\d+):)?(\d{1,2}):(\d{2})[.,](\d{3})/;

function seconds(m) {
    return (parseInt(m[1] || '0', 10) * 3600) + (parseInt(m[2], 10) * 60) + parseInt(m[3], 10) + (parseInt(m[4], 10) / 1000);
}

// parseVttTimings reads only the timing lines of a WebVTT (or SRT) text.
export function parseVttTimings(text) {
    const out = [];
    if (typeof text !== 'string') return out;
    for (const line of text.split(/\r?\n/)) {
        const i = line.indexOf('-->');
        if (i < 0) continue;
        const a = TIME.exec(line.slice(0, i));
        const b = TIME.exec(line.slice(i + 3));
        if (!a || !b) continue;
        const start = seconds(a);
        const end = seconds(b);
        if (end > start) out.push({ start, end });
    }
    return out;
}

export function creditsStart(cues, duration) {
    if (!Array.isArray(cues) || cues.length < MIN_CUES || !(duration > 0)) return null;
    const list = cues.filter((c) => c && c.end > c.start && c.start >= 0).sort((x, y) => x.start - y.start);
    if (list.length < MIN_CUES) return null;

    // A translator's signature: the trailing run of cues after a long silence,
    // if it is short in both count and time.
    let last = list.length - 1;
    let runStart = last;
    while (runStart > 0 && list[runStart].start - list[runStart - 1].end <= SIGNATURE_GAP_S) runStart--;
    if (runStart > 0) {
        const count = last - runStart + 1;
        const span = list[last].end - list[runStart].start;
        if (count <= SIGNATURE_MAX_CUES && span <= SIGNATURE_MAX_S) last = runStart - 1;
    }

    const at = list[last].end + AFTER_LAST_LINE_S;
    if (at > duration - MIN_GAIN_S) return null;     // a late line: nothing gained (or a post-credits scene)
    if (at < duration - MAX_CREDITS_S) return null;  // too early to be credits: another cut, a truncated file
    return at;
}

// creditsFromElement: the container's own answer, when the server found one in
// the file's chapters (jobs/scripts/credits.go -> data-credits-at). Validated
// here against the same bounds as everything else -- the attribute is ours,
// but the duration the server saw and the one the player has can differ.
export function creditsFromElement(el, duration) {
    const raw = el && el.dataset ? parseFloat(el.dataset.creditsAt) : NaN;
    if (!(raw > 0) || !(duration > 0)) return null;
    if (raw > duration - MIN_GAIN_S || raw < duration - MAX_CREDITS_S) return null;
    return raw;
}

// --- Where the timings come from -------------------------------------------

// cuesOfLoadedTracks: film-time cues of element-backed <track>s that already
// have them. cue-offset.js stashes the authored times on first touch; a cue it
// has not touched is still in authored time.
export function cuesOfLoadedTracks(video) {
    const out = [];
    if (!video || typeof video.querySelectorAll !== 'function') return out;
    for (const el of video.querySelectorAll('track')) {
        const cues = el.track && el.track.cues;
        if (!cues || !cues.length) continue;
        const list = [];
        for (const c of cues) {
            const start = c.__absStart !== undefined ? c.__absStart : c.startTime;
            const end = c.__absEnd !== undefined ? c.__absEnd : c.endTime;
            list.push({ start, end });
        }
        if (list.length > out.length) out.splice(0, out.length, ...list); // the fullest track
    }
    return out;
}

// timingSourceURL: a whole-file subtitle track to read timings from when none
// is loaded -- the viewer has subtitles off, or is on a track muxed into the
// film (hls.js feeds those segment by segment; their last line is not known
// until the end). Any language: timings do not depend on it. Not a
// translation: asking for one starts a job that costs money.
export function timingSourceURL(modal) {
    if (!modal || typeof modal.querySelectorAll !== 'function') return '';
    for (const chip of modal.querySelectorAll('.subtitle[data-src]')) {
        const provider = chip.getAttribute('data-provider') || '';
        if (provider === 'Translated' || provider === 'MediaProbe') continue;
        if (chip.getAttribute('data-locked') === 'true') continue;
        const src = chip.getAttribute('data-src');
        if (src) return src;
    }
    return '';
}
