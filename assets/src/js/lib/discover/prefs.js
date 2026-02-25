const LS_KEY = 'discover-prefs';

export function loadPrefs() {
    try { return JSON.parse(localStorage.getItem(LS_KEY)) || {}; }
    catch { return {}; }
}

export function savePrefs(patch) {
    try {
        localStorage.setItem(LS_KEY, JSON.stringify({ ...loadPrefs(), ...patch }));
    } catch {}
}
