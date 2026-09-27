import { render } from 'preact';
import { useEffect, useRef } from 'preact/hooks';
import { t, tf } from './i18n';

// The player's buffering label and the card behind it (docs/player.md
// "Buffering label"; buffering-label.js says when it is the lock). Design:
// the canvas "Плеер: буферизация вместо спиннера", variant A (owner,
// 2026-09-26). Both components take the label the transfer status published
// (lib/transferStatus.js playerLabel): { rate, title, sub, cta: { label,
// note, url }, props } -- they have no text, number or URL of their own.

function Spinner() {
    return (
        <svg class="wt-buffering-spin" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-opacity=".25" stroke-width="3" />
            <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" stroke-width="3" stroke-linecap="round" />
        </svg>
    );
}

function LockIcon() {
    return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <rect x="5" y="11" width="14" height="10" rx="2" />
            <path d="M8 11V7a4 4 0 0 1 8 0v4" />
        </svg>
    );
}

function ChevronIcon() {
    return (
        <svg class="wt-buffering-chev" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M9 6l6 6-6 6" />
        </svg>
    );
}

// The status's bolt (partials/resource/status.html #tx-i-bolt), inline: the
// card must not depend on a sprite somewhere else on the page.
function BoltIcon({ cls }) {
    return (
        <svg class={cls} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
            <path fill-rule="evenodd" clip-rule="evenodd" d="M14.615 1.595a.75.75 0 0 1 .359.852L12.982 9.75h7.268a.75.75 0 0 1 .548 1.262l-10.5 11.25a.75.75 0 0 1-1.272-.71l1.992-7.302H3.75a.75.75 0 0 1-.548-1.262l10.5-11.25a.75.75 0 0 1 .913-.143Z" />
        </svg>
    );
}

function CloseIcon() {
    return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true">
            <path d="M6 6l12 12M18 6L6 18" />
        </svg>
    );
}

// The player's own click (play/pause, tap-seek) and double-click
// (fullscreen) are on its container, and its keys on the document: nothing
// here reaches them -- the card, on the page, would bubble its keys to the
// document too.
const stop = (e) => e.stopPropagation();

// BufferingPill: without a label, "Buffering" -- a status line the picture
// takes the clicks through, as it did through the spinner. With one, the
// lock: a button with the viewer's cap that opens the card; the chevron says
// there is more, and goes while the card, the more, is open.
export function BufferingPill({ label, open = false, onToggle }) {
    if (!label) {
        return (
            <span class="wt-buffering-pill" role="status">
                <Spinner />
                {t('player.buffering')}
            </span>
        );
    }
    return (
        <button type="button" class="wt-buffering-pill wt-buffering-pill--cap"
            aria-expanded={open ? 'true' : 'false'} aria-label={tf('player.bufferingCap', label.rate)}
            onClick={(e) => { stop(e); onToggle(); }} onDblClick={stop}>
            <Spinner />
            {t('player.buffering')}
            <span class="wt-buffering-sep" aria-hidden="true" />
            <span class="wt-buffering-cap"><LockIcon />{label.rate}</span>
            {!open && <ChevronIcon />}
        </button>
    );
}

// CapCard: the transfer status's stream plan box (.tx-pbox, the same
// classes as partials/resource/status.html) in a native <dialog
// class="modal">, opened with showModal() like the player's other popups
// (#subtitles, #embed, the grace popup #grace-cta in stream_video.html). The
// top layer: centred on the screen with the page dimmed behind it, never cut
// by the player's rounded overflow:hidden frame (the owner's stage screenshot,
// 2026-09-26: the card inside the player was cut off), and over a fullscreen
// player -- the top layer is above the fullscreen element. Its words are the
// box's, its link the player's own surface (/trial?from=player-label).
//
// Mounted open, with the label as it was when the viewer opened it: it stays
// open, and says what it said, when the film plays again, the status takes
// its word back or the next file's player replaces this one (owner,
// 2026-09-26) -- only the viewer closes it: its X, Esc (`cancel`), a click
// beside it (the backdrop form, as #subtitles has), or its button (the trial
// opens in a new tab). Each asks the owner, onClose, to unmount it, and the
// unmount closes the dialog: one way out, however it was asked for. Focus
// starts on the box itself (tabindex -1, no ring): no ring on a button the
// viewer did not reach by keyboard; Tab reaches them all. Without showModal
// (jsdom; a browser without <dialog>) it is shown by DaisyUI's .modal-open
// class instead.
//
// Its owner is the page, not a player (openCapCard below).
export function CapCard({ label, onClose }) {
    const dialogRef = useRef(null);
    const boxRef = useRef(null);
    const onCloseRef = useRef(onClose);
    onCloseRef.current = onClose;
    const close = () => onCloseRef.current();
    useEffect(() => {
        const el = dialogRef.current;
        if (!el) return undefined;
        if (typeof el.showModal === 'function') {
            if (!el.open) el.showModal();
        } else {
            // A plain element on the page, outside the fullscreen stage,
            // would stay invisible there: leave fullscreen first, as the
            // grace popup's fallback does (Player.jsx).
            const doc = el.ownerDocument;
            const exit = doc.exitFullscreen || doc.webkitExitFullscreen;
            if ((doc.fullscreenElement || doc.webkitFullscreenElement) && exit) {
                Promise.resolve(exit.call(doc)).catch(() => {});
            }
            el.classList.add('modal-open');
        }
        // showModal() focuses the autofocus box itself; the fallback does not.
        if (boxRef.current && el.ownerDocument.activeElement !== boxRef.current) {
            boxRef.current.focus({ preventScroll: true });
        }
        // Esc: the close, as the X. Closed any other way (the browser's own
        // close request), the same.
        const onCancel = (e) => {
            e.preventDefault();
            onCloseRef.current();
        };
        const onNativeClose = () => onCloseRef.current();
        el.addEventListener('cancel', onCancel);
        el.addEventListener('close', onNativeClose);
        return () => {
            el.removeEventListener('cancel', onCancel);
            el.removeEventListener('close', onNativeClose);
            // Closed is unmounted: out of the top layer while still in the
            // document, so the browser hands focus back to where it was.
            if (typeof el.close === 'function' && el.open) el.close();
        };
    }, []);
    const p = label.props || {};
    return (
        <dialog ref={dialogRef} class="modal wt-cap-dialog" aria-label={label.title}
            onClick={stop} onDblClick={stop} onKeyDown={stop}>
            <div ref={boxRef} class="modal-box tx-pbox wt-cap-card" tabindex="-1" autofocus>
                <button type="button" class="wt-cap-card-x" aria-label={t('player.close')} onClick={() => close()}>
                    <CloseIcon />
                </button>
                <div class="tx-pl">
                    <BoltIcon cls="tx-bolt" />
                    <div class="wt-cap-card-text">
                        <div class="tx-pt">{label.title}</div>
                        {label.sub && <div class="tx-ps">{label.sub}</div>}
                    </div>
                </div>
                <div class="tx-pr">
                    <a class="tx-sbtn" href={label.cta.url} target="_blank" rel="noopener"
                        data-umami-event="donate-player-label" data-umami-event-ctx={p.ctx || ''}
                        data-umami-event-location={p.location || ''} data-umami-event-auth={p.auth || ''}
                        data-umami-event-state={p.state || ''} data-umami-event-tier={p.tier || ''}
                        data-umami-event-target={p.target || ''}
                        onClick={() => close()}>
                        <BoltIcon />
                        <span>{label.cta.label}</span>
                    </a>
                    {label.cta.note && <span class="tx-pn">{label.cta.note}</span>}
                </div>
            </div>
            {/* A click beside the card closes it (#subtitles' backdrop: a form
                behind the box across the dialog). Closed here rather than by
                the form's submit, which jsdom does not do. Literal label, as
                every modal's in this codebase. */}
            <form method="dialog" class="modal-backdrop">
                <button onClick={(e) => { e.preventDefault(); close(); }}>close</button>
            </form>
        </dialog>
    );
}

// The card is the page's, not the player's. Rendered inside the player it
// went with it: moving to the next file destroys the player it was opened
// from and mounts another on the same stage (next-item-go.js mountOnStage,
// destroyPlayer({ keepStage: true })), and a card opened while the next file
// loaded -- or at a stall near the end, before autoplay moved on under it --
// vanished with nobody closing it (review, 2026-09-27). So it has a root of
// its own on <body>: a top-layer dialog is on top wherever it sits in the
// document, over a fullscreen stage as the grace popup is. Only the viewer
// closes it (CapCard above); leaving the page does too (destroyPlayer
// without keepStage).
//
// Its state is the document's (CLAUDE.md: shared JS state lives in the DOM
// or on window, not in a module): open is "the host is on the page", and a
// change is said with a `player_cap_card` event on window -- the player on
// screen, whichever one it is by then, reads it for its lock's open state.
const CARD_HOST = 'wt-cap-card-host';
const CARD_EVENT = 'player_cap_card';

const cardHost = () => document.querySelector(`.${CARD_HOST}`);

export function capCardOpen() {
    return !!cardHost();
}

// openCapCard opens the card with this label, unless one is open already
// (one card at a time, saying what it said when opened).
export function openCapCard(label) {
    if (!label || capCardOpen()) return false;
    const host = document.createElement('div');
    host.className = CARD_HOST;
    document.body.appendChild(host);
    render(<CapCard label={label} onClose={closeCapCard} />, host);
    window.dispatchEvent(new CustomEvent(CARD_EVENT));
    return true;
}

export function closeCapCard() {
    const host = cardHost();
    if (!host) return;
    // Unmounted while still in the document: CapCard's cleanup closes the
    // dialog out of the top layer, and the browser hands focus back.
    render(null, host);
    host.remove();
    window.dispatchEvent(new CustomEvent(CARD_EVENT));
}

// onCapCard(fn): fn(open) on every open and close; returns the unsubscribe.
export function onCapCard(fn) {
    const h = () => fn(capCardOpen());
    window.addEventListener(CARD_EVENT, h);
    return () => window.removeEventListener(CARD_EVENT, h);
}
