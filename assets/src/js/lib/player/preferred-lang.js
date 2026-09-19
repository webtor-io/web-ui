// The preferred-language select in the subtitles dialog (owner, 2026-09-19).
//
// The language decides which track the ladder turns on and whether an AI
// translation is offered, and all of that is rendered by the server with
// the player. So a change is stored (PUT) and the stream action is asked
// for again: a new job key, a fresh dialog. The transcoder session is
// reused, so this is a re-render, not a new transcode -- and the player
// that comes back continues where this one stopped, without the "continue
// from" prompt (see AUTO_RESUME_KEY): the viewer changed a setting, they
// did not leave.
//
// It is the profile's preferred language itself, not a player setting of
// its own. A viewer without an account keeps it in the session; the server
// tells the two apart.

import { backgroundToken, fetchStreamRender } from './background-render.js';

export const AUTO_RESUME_KEY = 'wt-auto-resume';
const AUTO_RESUME_TTL_MS = 60 * 1000;

// markAutoResume leaves a one-shot note for the next player of the same
// file. Short-lived: a note that outlives the restart it was written for
// would skip the prompt on an ordinary visit an hour later.
export function markAutoResume(resourceID, path, storage = safeSessionStorage()) {
    if (!storage || !resourceID) return;
    try {
        storage.setItem(AUTO_RESUME_KEY, JSON.stringify({ resourceID, path: path || '', until: Date.now() + AUTO_RESUME_TTL_MS }));
    } catch (e) { /* private mode: the prompt shows, nothing is lost */ }
}

// takeAutoResume reads and removes the note; true when it is for this file
// and still fresh.
export function takeAutoResume(resourceID, path, storage = safeSessionStorage()) {
    if (!storage) return false;
    let note = null;
    try {
        note = JSON.parse(storage.getItem(AUTO_RESUME_KEY) || 'null');
        storage.removeItem(AUTO_RESUME_KEY);
    } catch (e) {
        return false;
    }
    return !!note && note.resourceID === resourceID && (note.path || '') === (path || '') && Date.now() <= note.until;
}

function safeSessionStorage() {
    try {
        return window.sessionStorage;
    } catch (e) {
        return null;
    }
}

// restartStream asks for the stream action again, in place. The form is
// the one the viewer pressed to get here, found by what it posts to: its
// class is the button's action name ("stream" on a resource page), not the
// endpoint's, and looking for `form.stream-video` found nothing -- the
// fallback reload then closed the player on every URL without the
// `#action=stream` hash (owner, 2026-09-19). The fallback, for a layout
// with no such form, therefore carries the hash itself.
export function findStreamForm(root = document) {
    return root.querySelector('form[action$="/stream-video"]');
}

function restartStream() {
    const form = findStreamForm();
    if (form && typeof form.requestSubmit === 'function') {
        form.requestSubmit();
        return;
    }
    window.location.hash = 'action=stream';
    window.location.reload();
}

// fetchFreshDialog is the new render's #subtitles dialog, or null.
export async function fetchFreshDialog(form, opts = {}) {
    const doc = await fetchStreamRender(form, opts);
    return doc ? doc.querySelector('#subtitles') : null;
}

export function wirePreferredLang(container, { fetchImpl, EventSourceImpl, restart = restartStream, beforeRestart, swap, getToken = backgroundToken } = {}) {
    if (!container.querySelector('#preferred-lang')) return;
    const busyEl = () => container.querySelector('#preferred-lang-busy');
    // Delegated: the select is part of what a swap replaces.
    container.addEventListener('change', async (e) => {
        const select = e.target;
        if (!select || select.id !== 'preferred-lang') return;
        const setBusy = (on) => {
            select.disabled = on;
            const busy = busyEl();
            if (busy) busy.hidden = !on;
        };
        const from = select.getAttribute('data-current') || '';
        const to = select.value;
        if (!to || to === from) return;
        setBusy(true);
        let ok = false;
        try {
            const res = await (fetchImpl || fetch)('/stream-video/preferred-lang', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json', 'X-CSRF-TOKEN': window._CSRF },
                body: JSON.stringify({ lang: to }),
            });
            ok = !!res && res.ok !== false && (res.status === undefined || res.status < 400);
        } catch (err) {
            ok = false;
        }
        if (!ok) {
            // Nothing was stored, so nothing may look as if it was.
            select.value = from;
            setBusy(false);
            return;
        }
        if (window.umami) window.umami.track('subtitle-preferred-lang', { from, to });
        // The quiet path first: the new dialog is rendered off the page and
        // put in place of this one, and the film never notices. Whatever
        // that cannot do -- no account to start a job without a Turnstile
        // token, an answer that is not a player -- the visible restart does.
        if (swap) {
            const token = await getToken();
            if (token !== null) {
                const fresh = await fetchFreshDialog(findStreamForm(), { fetchImpl, EventSourceImpl, token });
                if (fresh && swap(fresh)) return;
            }
        }
        if (beforeRestart) beforeRestart();
        restart();
    });
}
