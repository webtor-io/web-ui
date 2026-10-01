// Recovery changes the transport, not the codec policy or the automatic budget.
// Fetch off-page, then replace the player on its existing fullscreen stage.
import { backgroundToken, fetchStreamRender } from './background-render.js';
import { replacePlayerOnStage } from './player-stage.js';
import { itemOf, startFormOf, answeredSlow } from './stream-restart.js';
import { declarationFor, vodRescueFor, VOD_FALLBACK_REASON, VOD_FALLBACK_CLASS } from './decode-declaration.js';
import { readCarry } from './next-item.js';

export const RESTART_RENDER_TIMEOUT_MS = 10 * 60 * 1000;

export function restartStartForm(video, root, win = window, doc = document) {
    if (win._embedSettings) {
        const form = doc.createElement('form');
        form.method = 'post';
        form.action = win.location.href;
        for (const [name, value] of Object.entries({ settings: JSON.stringify(win._embedSettings), _csrf: win._CSRF || '', _sessionID: win._sessionID || '' })) {
            const input = doc.createElement('input');
            Object.assign(input, { type: 'hidden', name, value });
            form.append(input);
        }
        return form;
    }
    const itemId = itemOf(video, root, doc);
    const resourceId = video.dataset.resourceId;
    // In fullscreen after Next, the page can still have the previous file's
    // form. Re-address a form of the SAME resource to the actual playing file.
    const current = startFormOf(video, root, doc) || [...doc.querySelectorAll('form[action$="/stream-video"], form[action$="/stream-audio"]')]
        .find((f) => f.querySelector('[name="resource-id"]')?.value === resourceId);
    if (!current || !itemId) return null;
    const form = current.cloneNode(true);
    const item = form.querySelector('[name="item-id"]');
    if (!item) return null;
    item.value = itemId;
    item.setAttribute('value', itemId);
    // A video and an audio file can follow each other in a resource.
    form.action = form.action.replace(/\/stream-(video|audio)$/, video.tagName === 'AUDIO' ? '/stream-audio' : '/stream-video');
    return form;
}

export function restartFields(video, root, fallback, win = window) {
    const file = { resourceId: video.dataset.resourceId, itemId: itemOf(video, root, win.document) };
    let decode = video.dataset.decode || null;
    if (fallback) {
        try { decode = declarationFor(win, file); } catch (e) { decode = null; }
    }
    const rescue = !fallback && vodRescueFor(win, file);
    return {
        ...readCarry(root?.querySelector('#subtitles')),
        purge: 'true', 'force-slow': answeredSlow(video) ? 'true' : null,
        decode: video.tagName === 'VIDEO' ? decode : null,
        'decode-fallback': fallback?.reason || (rescue ? VOD_FALLBACK_REASON : null),
        'decode-class': fallback?.cls || (rescue ? VOD_FALLBACK_CLASS : null),
    };
}

export function createBackgroundRestart({ video, root, getStage, getAspectRatio, getState,
    initPlayer, destroyPlayer, onLoading = () => {}, onFailure = () => {}, visible,
    fetchRender = fetchStreamRender, getToken = backgroundToken,
    win = window, doc = document }) {
    let disposed = false;
    let busy = false;
    let engaged = false;
    let failed = false;
    let controller = null;
    let lastFallback = null;
    let fixedPlace = null;

    const start = async ({ position, fixedPosition = false, fallback = lastFallback } = {}) => {
        if (disposed || busy) return;
        engaged = true;
        failed = false;
        lastFallback = fallback;
        if (fixedPosition) fixedPlace = position.at;
        if (fallback && fixedPlace !== null) {
            position = { ...position, at: fixedPlace };
            fixedPosition = true;
        }
        const form = restartStartForm(video, root, win, doc);
        // Legacy/non-enhanced hosts can still use the ordinary start path.
        if (!form || !win.EventSource) { visible(position, fallback); return; }
        busy = true;
        controller = new AbortController();
        const signal = controller.signal;
        let play = position?.play ?? !video.paused;
        const changed = () => { play = !video.paused; };
        video.addEventListener('play', changed);
        video.addEventListener('pause', changed);
        const stateNow = () => {
            const live = getState();
            return { ...live, ...(fixedPosition ? { at: position.at } : {}), play };
        };
        onLoading(true);
        let visibleRequired = false;
        let mountingState = null;
        try {
            const token = await getToken();
            if (signal.aborted) return;
            if (token === null) { visible(stateNow(), fallback); return; }
            const render = await fetchRender(form, {
                token, fields: restartFields(video, root, fallback, win), signal,
                timeoutMs: RESTART_RENDER_TIMEOUT_MS,
                onVisibleRequired: () => { visibleRequired = true; },
            });
            if (signal.aborted || !getStage()?.isConnected) return;
            if (!render?.querySelector('.player')) {
                if (render) visibleRequired = true;
                if (visibleRequired) visible(stateNow(), fallback);
                else { failed = true; onFailure(); }
                return;
            }
            // The old buffer may have played for minutes while the job ran.
            // Read its position NOW. A failed seek/decoder retains its target.
            const state = stateNow();
            mountingState = state;
            await replacePlayerOnStage(render, {
                stage: getStage(), root, aspectRatio: getAspectRatio(), initPlayer, destroyPlayer,
                restartState: state.settled ? state : null, awaitStart: state.play,
            });
        } catch (e) {
            if (mountingState || (!signal.aborted && !disposed)) {
                // A mount failure can leave the old player gone. Use the
                // visible path there; a fetch failure leaves it intact.
                if (!video.isConnected) visible(mountingState || stateNow(), fallback);
                else { failed = true; onFailure(); }
            }
        } finally {
            video.removeEventListener('play', changed);
            video.removeEventListener('pause', changed);
            busy = false;
            if (!disposed) onLoading(false);
        }
    };
    return {
        start,
        dispose() { disposed = true; controller?.abort(); },
        get engaged() { return engaged; },
        get failed() { return failed; },
    };
}
