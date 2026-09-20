// A stream action started off the page: the job runs, its render comes back
// as a Document, and nothing on screen changes until the caller decides what
// to take from it. First user: the preferred-language select, which takes
// the #subtitles dialog and leaves the playing film alone (preferred-lang.js).
// Meant for the next ones too -- switching to the next episode needs exactly
// this: the next file's player rendered while the current one still plays
// (owner, 2026-09-19).

import { silentToken } from '../turnstileAction';

// backgroundToken is what a job start made off the page needs from
// Turnstile. A signed-in viewer and a deployment with no widget need
// nothing (""), and that is an answer; an anonymous viewer needs a token,
// and only a silent one will do -- there is no form on screen to host a
// checkbox. Tokens are single-use, so the one the page got for its first
// start cannot be sent again; a fresh one from the warm widget costs about a
// second. null means "cannot be had quietly": the visible restart, which
// goes through a real submit and can show the checkbox, takes over.
export async function backgroundToken() {
    if (window._userId || !document.getElementById('turnstile-action')) return '';
    const token = await silentToken();
    return token || null;
}

const FRESH_RENDER_TIMEOUT_MS = 30000;

// fetchStreamRender asks for a stream action WITHOUT putting it on the page,
// and resolves with the new render as a parsed Document (or null: any answer
// that is not a rendered player -- an error card, a cap modal, a timeout --
// is for a visible start to show). Two steps, the same two the
// page itself takes: the POST answers with a job log, and the job's last
// message (`rendertemplate`) carries the player's HTML.
//
// `onProgress(text)`: the job's log as it happens, one line at a time -- the
// step that is running ("warming up torrent client, downloading 10 MB") and
// its status under it ("37%", "12 s until we give up on this swarm"). For a
// caller whose viewer is WAITING for this render (the move to the next
// episode): the visible start shows its log, and a wait with nothing on
// screen but a spinner reads as a hang. A silent restart simply passes none.
export function progressText(step, status) {
    if (step && status) return `${step} \u2014 ${status}`;
    return step || status || '';
}

export async function fetchStreamRender(form, { fetchImpl, EventSourceImpl, token = '', timeoutMs = FRESH_RENDER_TIMEOUT_MS, onProgress = null } = {}) {
    const doFetch = fetchImpl || fetch;
    const ES = EventSourceImpl || window.EventSource;
    if (!form || !ES) return null;
    const target = document.querySelector(form.getAttribute('data-async-target') || '');
    let text = '';
    const body = new FormData(form);
    if (token) body.set('cf-turnstile-response', token);
    try {
        const res = await doFetch(form.action, {
            method: 'POST',
            body,
            headers: {
                'X-Requested-With': 'XMLHttpRequest',
                'X-Layout': (target && target.getAttribute('data-async-layout')) || '',
                'X-Return-Url': window.location.pathname + window.location.search,
                'X-CSRF-TOKEN': window._CSRF,
                'X-SESSION-ID': window._sessionID,
            },
        });
        if (!res || res.ok === false) return null;
        text = await res.text();
    } catch (e) {
        return null;
    }
    const doc = new DOMParser().parseFromString(text, 'text/html');
    // The async wire format wraps the view in <template> blocks, whose
    // content DOMParser keeps in a separate fragment.
    let host = doc.querySelector('[data-async-progress-log]');
    for (const tpl of doc.querySelectorAll('template')) {
        if (host) break;
        host = tpl.content.querySelector('[data-async-progress-log]');
    }
    const url = host && host.getAttribute('data-async-progress-log');
    if (!url) return null;

    return new Promise((resolve) => {
        const src = new ES(url, { withCredentials: true });
        let step = '';
        const finish = (value) => {
            clearTimeout(timer);
            src.close();
            resolve(value);
        };
        const timer = setTimeout(() => finish(null), timeoutMs);
        src.onerror = () => finish(null);
        src.onmessage = (ev) => {
            let data = null;
            try { data = JSON.parse(ev.data); } catch (e) { return; }
            if (onProgress) {
                // A step line names itself in `message`; a status update
                // belongs to the step above it. `done` closes a step, and the
                // next one is about to say what it is.
                if ((data.level === 'inprogress' || data.level === 'info') && data.message) {
                    step = data.message;
                    onProgress(progressText(step, ''));
                } else if (data.level === 'statusupdate' && data.status) {
                    onProgress(progressText(step, data.status));
                }
            }
            if (data.level === 'rendertemplate') {
                finish(new DOMParser().parseFromString(String(data.body || ''), 'text/html'));
            } else if (data.level === 'close' || data.level === 'custom' || data.level === 'error') {
                finish(null);
            }
        };
    });
}

