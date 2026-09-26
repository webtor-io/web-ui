// The transfer status's dev-only preview (handlers/resource/status.go
// debugStatus): these params of a page's URL ride along to its /status
// streams -- the resource page's (app/resource/status.js) and each live row's
// on the Vault page (app/vault/progress.js) -- so every state can be looked at
// on either. The server ignores them in release.
export const DEBUG_PARAMS = [
    'debug_status', 'seeders', 'leechers', 'peers', 'progress', 'debug_pieces', 'rate', 'paused', 'noseeders',
    'checking', 'user_rate', 'plan_limited', 'plan_rate', 'viewer', 'viewer_stalled', 'bitrate',
    'availability', 'debug_missing', 'wanted_missing', 'reader_missing',
];

// debugQuery is the preview's params of a page's query string as a tail for
// the stream's URL ("&debug_status=caching&seeders=4"), '' for none.
export function debugQuery(search) {
    const q = new URLSearchParams(search);
    let out = '';
    for (const k of DEBUG_PARAMS) {
        if (q.has(k)) out += `&${k}=${encodeURIComponent(q.get(k))}`;
    }
    return out;
}
