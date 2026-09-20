// What the viewer set on the player and expects to find again: volume, mute,
// playback speed. Per browser, not per account -- it is how loud THIS machine
// is -- so localStorage, no server.
//
// Storage is touched through safeStorage() only: in a sandboxed or
// third-party iframe (the embed player) merely reading window.localStorage
// throws, and a player that cannot remember must still play.

const KEY = 'wt-player-prefs';

export const RATES = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2];
export const DEFAULTS = { volume: 1, muted: false, rate: 1 };

export function safeStorage() {
    try { return window.localStorage; } catch (e) { return null; }
}

const clamp01 = (v) => Math.max(0, Math.min(1, v));

// A stored value is somebody else's data by the time it is read back: an
// older build, a hand edit, another tab mid-write. Anything that is not
// exactly what we would have written reads as the default for that field.
export function loadPrefs(storage = safeStorage()) {
    const out = { ...DEFAULTS };
    if (!storage) return out;
    let raw = null;
    try { raw = JSON.parse(storage.getItem(KEY) || 'null'); } catch (e) { return out; }
    if (!raw || typeof raw !== 'object') return out;
    if (typeof raw.volume === 'number' && isFinite(raw.volume)) out.volume = clamp01(raw.volume);
    if (typeof raw.muted === 'boolean') out.muted = raw.muted;
    if (RATES.includes(raw.rate)) out.rate = raw.rate;
    return out;
}

export function savePrefs(patch, storage = safeStorage()) {
    if (!storage) return false;
    try {
        storage.setItem(KEY, JSON.stringify({ ...loadPrefs(storage), ...patch }));
        return true;
    } catch (e) {
        return false; // quota, private mode
    }
}

// stepRate moves one notch along RATES and stops at the ends. A rate that is
// not on the scale (set by something else) snaps to the nearest notch first.
export function stepRate(rate, dir) {
    let i = RATES.indexOf(rate);
    if (i < 0) {
        i = RATES.reduce((best, r, idx) => (Math.abs(r - rate) < Math.abs(RATES[best] - rate) ? idx : best), 0);
    }
    return RATES[Math.max(0, Math.min(RATES.length - 1, i + (dir > 0 ? 1 : -1)))];
}

export function rateLabel(rate) {
    return `${rate}×`;
}

// The subtitle delay is a fact about one file and its subtitles, not about
// the browser: a small capped map, newest last. Keyed by the file, not by the
// track -- two subtitle files for one film rarely need different corrections
// by more than the viewer will fix with one more press, and the reset button
// is right there.
const DELAY_KEY = 'wt-sub-delay';
const DELAY_CAP = 50;

function readDelays(storage) {
    try {
        const raw = JSON.parse(storage.getItem(DELAY_KEY) || 'null');
        return raw && typeof raw === 'object' && !Array.isArray(raw) ? raw : {};
    } catch (e) { return {}; }
}

export function loadSubtitleDelay(fileKey, storage = safeStorage()) {
    if (!storage || !fileKey) return 0;
    const v = readDelays(storage)[fileKey];
    return typeof v === 'number' && isFinite(v) ? v : 0;
}

export function saveSubtitleDelay(fileKey, delay, storage = safeStorage()) {
    if (!storage || !fileKey) return false;
    const map = readDelays(storage);
    delete map[fileKey]; // re-inserted last: insertion order is the age
    if (delay !== 0) map[fileKey] = delay;
    const keys = Object.keys(map);
    for (const k of keys.slice(0, Math.max(0, keys.length - DELAY_CAP))) delete map[k];
    try { storage.setItem(DELAY_KEY, JSON.stringify(map)); return true; } catch (e) { return false; }
}
