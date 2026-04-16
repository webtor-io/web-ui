// AI recommendations API client for the /discover/ai/* routes served by
// handlers/discover_ai.
//
// Responsibilities
// - Build the request payload with the browser's local clock (the server
//   is UTC and cannot guess the user's time zone).
// - Parse JSON responses and expose a single typed error shape to callers.
// - Open SSE streams for the streaming recommend / refine endpoints.
// - Abort in-flight chip requests on page navigation via an optional signal.
//
// The server-side error contract for the JSON endpoints is:
//   200 OK                → parsed JSON body
//   400 Bad Request       → { code, message }
//   402 Payment Required  → { code: "quota_exceeded", tier }
//   404 Not Found         → { code: "disabled" }         ← feature flag off
//   500 Internal          → { code: "internal", message }
//
// Streaming endpoints emit SSE frames with named events: phase, item,
// done, error. See openAIStream below.

const AI_TIMEOUT_MS = 60000; // chips fetch — Claude can take 20-30s on cold cache

// AIError is thrown for any non-2xx response so callers branch on `err.code`
// instead of parsing English messages. Network/abort errors surface as the
// native fetch error.
//
// dailyQuota / resetAt / upgradeQuota are populated for the quota_exceeded
// code (both via the JSON envelope and via the SSE error event) so the
// UI can render the full upgrade hint without a separate lookup:
//   - dailyQuota   — current tier's daily cap, for the "0 / N" headline
//   - resetAt      — unix-seconds the cap resets, for "resets in 10h 30m"
//   - upgradeQuota — paid tier's daily cap, only present for free users
export class AIError extends Error {
    constructor(status, code, message, tier, dailyQuota, resetAt, upgradeQuota) {
        super(message || code || 'ai error');
        this.name = 'AIError';
        this.status = status;
        this.code = code || 'unknown';
        this.tier = tier;
        this.dailyQuota = dailyQuota;
        this.resetAt = resetAt;
        this.upgradeQuota = upgradeQuota;
    }
}

// currentClock returns the browser's local day-of-week + hour, matching the
// Go server's ClientClock contract. Uses Intl for the weekday so we don't
// ship a locale map just for this one string.
export function currentClock() {
    const now = new Date();
    const day = new Intl.DateTimeFormat('en-US', { weekday: 'long' }).format(now);
    return { day, hour: now.getHours() };
}

// currentLocale picks a two-letter code the server understands.
// Mirrors normalizeLocale + supportedLocales in
// services/recommendations/context.go.
//
// Source priority:
//   1. document.documentElement.lang — the user's CURRENT UI choice. The
//      i18n middleware (services/i18n/middleware.go) sets this from the
//      URL prefix (/ru/, /fr/...), the lang cookie, or Accept-Language
//      detection on first visit; the lang switcher in app/layout.js syncs
//      it on click. Trusting it means the JS respects an explicit
//      switch — e.g. a user with browser pref `ru-RU, en` who visited
//      /en/ gets English chips instead of being overruled by the browser
//      preference list.
//   2. navigator.languages — fallback when <html lang> is missing or set
//      to a value we don't support (shouldn't happen in practice since the
//      server always renders a supported lang, but keeps the function
//      robust if the script ever runs on a non-Webtor page).
//   3. navigator.language — single-value fallback for older browsers.
//   4. "en" hardcoded default.
//
// We walk each list and pick the first entry whose 2-letter prefix we
// actually support on the server.
const SUPPORTED_LOCALES = ['en', 'ru', 'es', 'de', 'fr', 'pt', 'it'];

function firstSupported(tags) {
    for (const raw of tags) {
        if (!raw) continue;
        const prefix = String(raw).toLowerCase().slice(0, 2);
        if (SUPPORTED_LOCALES.includes(prefix)) return prefix;
    }
    return null;
}

export function currentLocale() {
    const fromHtml = firstSupported([document.documentElement.lang]);
    if (fromHtml) return fromHtml;
    const fromList = firstSupported(navigator.languages || []);
    if (fromList) return fromList;
    const fromSingle = firstSupported([navigator.language]);
    if (fromSingle) return fromSingle;
    return 'en';
}

async function parseError(resp) {
    let body = {};
    try { body = await resp.json(); } catch { /* non-JSON body */ }
    throw new AIError(
        resp.status, body.code, body.message, body.tier,
        body.daily_quota, body.reset_at, body.upgrade_quota,
    );
}

function withTimeout(signal, ms) {
    const ac = new AbortController();
    const timer = setTimeout(() => ac.abort(new Error('ai request timeout')), ms);
    if (signal) {
        if (signal.aborted) ac.abort(signal.reason);
        else signal.addEventListener('abort', () => ac.abort(signal.reason), { once: true });
    }
    return {
        signal: ac.signal,
        cancel: () => clearTimeout(timer),
    };
}

// fetchChips — GET /discover/ai/chips.
// Does not consume quota; the server reads from its distributed cache.
export async function fetchChips({ signal } = {}) {
    const { day, hour } = currentClock();
    const locale = currentLocale();
    const params = new URLSearchParams({ day, hour: String(hour), locale });
    const t = withTimeout(signal, AI_TIMEOUT_MS);
    try {
        const resp = await fetch(`/discover/ai/chips?${params.toString()}`, {
            method: 'GET',
            headers: { 'Accept': 'application/json' },
            signal: t.signal,
        });
        if (!resp.ok) await parseError(resp);
        return await resp.json();
    } finally {
        t.cancel();
    }
}

// refreshChips — POST /discover/ai/chips/refresh.
// Explicit user action; consumes 1 quota unit.
export async function refreshChips({ signal } = {}) {
    const body = { locale: currentLocale(), clock: currentClock() };
    const t = withTimeout(signal, AI_TIMEOUT_MS);
    try {
        const resp = await fetch('/discover/ai/chips/refresh', {
            method: 'POST',
            headers: {
                'Accept': 'application/json',
                'Content-Type': 'application/json',
                'X-CSRF-TOKEN': window._CSRF,
            },
            body: JSON.stringify(body),
            signal: t.signal,
        });
        if (!resp.ok) await parseError(resp);
        return await resp.json();
    } finally {
        t.cancel();
    }
}

// HISTORY_TURNS_CAP is how many of the most recent conversation turns we
// forward to /refine/stream. We cap it on the client because the request
// rides in the URL (EventSource is GET-only) and a 5-refine conversation
// can otherwise grow to several KB of URL-encoded JSON. Four turns ≈
// 1.5KB encoded, well under any sane proxy limit.
const HISTORY_TURNS_CAP = 4;

// recommendStream / refineStream are the SSE-emitting variants of the
// recommend/refine endpoints. They use the browser's native EventSource
// (GET-only) so we don't need a custom fetch+ReadableStream parser, but
// that does mean every request parameter has to live in the query string
// and the CSRF token rides as a query param too (matches the project's
// existing pattern in lib/progressLog.js and app/resource/status.js).
//
// callbacks shape:
//   onPhase({phase, expected})  — "claude" | "resolving" | "done"
//   onItem(recommendation)      — single resolved card
//   onDone({total, remaining_quota, tier}) — terminal success
//   onError(AIError)            — terminal failure
// Returns a {close()} handle so callers can abort on unmount.
function buildStreamURL(path, { query, history }) {
    const { day, hour } = currentClock();
    const locale = currentLocale();
    const params = new URLSearchParams({
        query: String(query || '').trim(),
        locale,
        day,
        hour: String(hour),
        _csrf: window._CSRF || '',
    });
    if (Array.isArray(history) && history.length > 0) {
        const trimmed = history.slice(-HISTORY_TURNS_CAP);
        params.set('history', JSON.stringify(trimmed));
    }
    return `${path}?${params.toString()}`;
}

function openAIStream(url, { onPhase, onItem, onDone, onError }) {
    const source = new EventSource(url, { withCredentials: true });
    let closed = false;
    const close = () => {
        if (closed) return;
        closed = true;
        source.close();
    };

    const safeParse = (raw) => {
        try { return JSON.parse(raw); } catch { return null; }
    };

    source.addEventListener('phase', (e) => {
        const data = safeParse(e.data);
        if (data && onPhase) onPhase(data);
    });
    source.addEventListener('item', (e) => {
        const data = safeParse(e.data);
        if (data && onItem) onItem(data);
    });
    source.addEventListener('done', (e) => {
        const data = safeParse(e.data) || {};
        if (onDone) onDone(data);
        close();
    });
    source.addEventListener('error', (e) => {
        // Two distinct error sources collapse into this handler:
        //  1. A named "error" SSE event sent by the backend (with payload)
        //  2. A transport-level error (network drop / EOF) where e.data
        //     is undefined and source.readyState === CLOSED.
        // Discriminate by checking for payload first.
        if (e && e.data) {
            const data = safeParse(e.data) || {};
            if (onError) onError(new AIError(
                0, data.code, data.code, data.tier,
                data.daily_quota, data.reset_at, data.upgrade_quota,
            ));
        } else if (source.readyState === EventSource.CLOSED && !closed) {
            if (onError) onError(new AIError(0, 'connection_lost', 'lost connection to server'));
        }
        close();
    });

    return { close };
}

export function recommendStream(query, callbacks) {
    const url = buildStreamURL('/discover/ai/recommend/stream', { query });
    return openAIStream(url, callbacks);
}

export function refineStream(query, history, callbacks) {
    const url = buildStreamURL('/discover/ai/refine/stream', { query, history });
    return openAIStream(url, callbacks);
}
