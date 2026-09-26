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
// (fullscreen) are on its container: nothing here reaches them.
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
// classes as partials/resource/status.html), over the video and inside the
// player, so a fullscreen player shows it too. Its words are the box's, its
// link the player's own surface (/trial?from=player-label).
export function CapCard({ label, onClose }) {
    const p = label.props || {};
    return (
        <div class="tx-pbox wt-cap-card" role="dialog" aria-label={label.title}
            onClick={stop} onDblClick={stop}>
            <button type="button" class="wt-cap-card-x" aria-label={t('player.close')} onClick={(e) => { stop(e); onClose(); }}>
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
                    data-umami-event-target={p.target || ''}>
                    <BoltIcon />
                    <span>{label.cta.label}</span>
                </a>
                {label.cta.note && <span class="tx-pn">{label.cta.note}</span>}
            </div>
        </div>
    );
}
