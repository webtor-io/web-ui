// Watch / Listen pressed again while that file's player is on the page plays
// or pauses it instead of starting the stream anew (owner, 2026-10-18). The
// button says what a press will do: Pause while the player plays.
//
// Only a press: the stream form is also submitted by code -- Turnstile's
// re-submit, a restart for a preferred language or a passthrough fallback,
// the next file -- and those mean a new stream. They go through
// requestSubmit(), which fires no click on the button (the polyfill in
// app/layout.js clicks a temporary <input>, or the button only for
// Turnstile's pass, while the form is busy). The tool line's "Watch here"
// (lib/toolIntent.js) clicks the button for a person and is a press.
//
// The label is not kept here: the player gets data-playing, and the file
// card swaps the button's spans with :has() (partials/button.html). A card
// rendered anew around a kept log (the watched toggle, lib/loadAsyncView.js)
// so reads the state from the player itself.

const PLAYER = 'video.player, audio.player';

// livePlayer is the player in the log this stream form reports into, if it
// is one a press can play or pause: connected, with a source, not failed.
// Anything else -- no player yet, a refusal card, a dead player -- is for a
// new stream.
export function livePlayer(form) {
    const target = form.getAttribute('data-async-target');
    const log = target ? form.ownerDocument.querySelector(target) : null;
    const media = log ? log.querySelector(PLAYER) : null;
    if (!media || !media.isConnected || media.error || !media.currentSrc) return null;
    return media;
}

function onClick(e) {
    const btn = e.target && e.target.closest ? e.target.closest('form.stream button[type="submit"]') : null;
    if (!btn || !btn.form) return;
    // A form whose job runs is lib/actionBusy.js's to answer.
    if (btn.form.dataset.busy) return;
    const media = livePlayer(btn.form);
    if (!media) return;
    // Not a start: no submit, and no "stream" click for Umami or
    // countBusyPress to count.
    e.preventDefault();
    e.stopPropagation();
    if (media.paused || media.ended) {
        const p = media.play();
        if (p && typeof p.catch === 'function') p.catch(() => {});
    } else {
        media.pause();
    }
}

// Media events do not bubble: caught on the way down.
function onMedia(e) {
    const media = e.target;
    if (!media || !media.matches || !media.matches(PLAYER)) return;
    if (e.type === 'play') media.setAttribute('data-playing', '');
    else media.removeAttribute('data-playing');
}

const MEDIA_EVENTS = ['play', 'pause', 'ended', 'emptied', 'error'];

// initStreamToggle wires the page once, whatever imports it how many times:
// two listeners would play and pause on one press.
export function initStreamToggle(doc = document) {
    const win = doc.defaultView;
    if (!win || win.__streamToggle) return;
    win.__streamToggle = true;
    doc.addEventListener('click', onClick, true);
    for (const t of MEDIA_EVENTS) doc.addEventListener(t, onMedia, true);
}
