import av from '../../lib/av';
import {
    NAVBAR_H, applyView, bindBlock, createCtaWatch, initDetails, mirrorBlock, paintBar, playing, present, upsellElsewhere,
} from '../../lib/transferStatus';
import { createPlayerActivity } from '../../lib/playerActivity';
import { debugQuery } from '../../lib/statusDebug';

// The transfer status view (#torrent-status, views/resource/get.html): one
// status stream per page, drawn into the card's block and into its copy in
// the sticky bar -- the chain while something moves, the badge while nothing
// does (the view's mode; the server holds the swarm on the chain through
// the gaps between its pieces, services/statusview Hold, and the page keeps
// the viewer on it while its own player streams, lib/transferStatus.js
// playing). The server
// renders the block with the page (partials/resource/status.html); every
// stream message carries the whole `view`
// (services/statusview), and lib/transferStatus.js writes it into the same
// nodes -- nothing here builds markup.

// A status that is only a gap in the data: the seeder's stats briefly
// unavailable (`unknown`) or a missed stats event (`idle`), both gone a second
// later mid-transfer. The sticky bar keeps its last picture through them for
// up to GAP_HOLD_MS -- repainting it read as a blink (owner, 2026-09-20) --
// and stickyStatus.js holds the bar itself up for as long.
const isGap = (status) => status.state === 'unknown' || status.state === 'idle';
const GAP_HOLD_MS = 8000;

// A plan box's wording depends on time as well as on messages (the minute
// after a stall, the player leaving its grace window, another offer coming
// and going): while one can be up, the
// last status is drawn again every TICK_MS. Writes only what changed.
const TICK_MS = 1000;

// Engines without scroll anchoring (Safari before 27) move everything under
// the card when its block grows or shrinks -- the plan box coming and going
// -- even while the block is scrolled away, so a playing video jumps. There
// the page makes up for it itself (render).
const anchoring = () => typeof CSS !== 'undefined' && typeof CSS.supports === 'function' && CSS.supports('overflow-anchor', 'auto');

av(async function() {
    const container = this;
    const resourceId = container.dataset.resourceId;
    const inner = container.querySelector('#torrent-status-block');
    const card = inner && inner.querySelector('[data-tx]');
    if (!resourceId || !card) return;
    container._statusGone = false;

    const blocks = [{ refs: bindBlock(card), location: 'card' }];
    const mirrorHost = document.querySelector(`[data-status-mirror-for="${resourceId}"]`);
    if (mirrorHost) blocks.push({ refs: bindBlock(mirrorBlock(card, mirrorHost)), location: 'sticky' });
    // The copy's Vault link was never bound by the page's async links (it
    // did not exist then): a press on it is a press on the card's, which
    // opens the pledge form (or the login) in place -- not a page load
    // under a playing video.
    const cardVault = blocks[0].refs.vault;
    const stickyVault = blocks[1] && blocks[1].refs.vault;
    const onStickyVault = (e) => {
        e.preventDefault();
        cardVault.click();
    };
    if (cardVault && stickyVault) stickyVault.addEventListener('click', onStickyVault);
    const detailsStops = blocks.map((b) => initDetails(b.refs, window, { topInset: NAVBAR_H }));
    const ctaWatch = createCtaWatch({ umami: window.umami });
    for (const b of blocks) for (const box of b.refs.boxes) ctaWatch.watch(box.cta);

    let last = null; // the last status message
    let steady = null; // the last one that was not a gap
    let gapSince = 0;
    let ticker = null;
    const render = () => {
        if (!last || !last.view) return;
        // What the page's player adds to the view (lib/transferStatus.js
        // present): what it is doing, the stream job's word on its file
        // against the cap, whether it is still inside its free grace window
        // (by movie time -- nothing is sold there), another offer on screen
        // or on its way (the grace popup the player is about to put up as it
        // leaves the window: one signal with inGrace, so no box comes up in
        // the frames between the two), and the viewer's answer to that popup
        // while their player has not yet stalled at the cap it told them of.
        const env = {
            player: activity.state(),
            stallSub: activity.stallSub(),
            fitsCap: activity.fitsCap(),
            overCap: activity.overCap(),
            inGrace: activity.inGrace(),
            upsellElsewhere: upsellElsewhere(document) || activity.graceOfferDue(),
            offerAnswered: activity.offerAnswered(),
        };
        // While it streams, the viewer stays on the chain through the gaps
        // between its HLS segments, where the proxy counts no request of
        // theirs open: the server's view with them at their last reading
        // (playing). Inside the grace window too: grace segments carry the
        // viewer's session and are counted like any other request of theirs.
        const streaming = activity.streaming();
        const shown = (status) => (streaming ? playing(status.view) : status.view);
        const held = isGap(last) && steady && steady.view && Date.now() - gapSince < GAP_HOLD_MS;
        // Scrolled away above the viewport, on an engine that does not
        // anchor: whatever the block gains or loses in height, the page
        // scrolls by, so nothing under it moves.
        let before = null;
        if (!anchoring()) {
            const r = container.getBoundingClientRect();
            if (r.bottom <= 0) before = r.height;
        }
        for (const b of blocks) {
            const status = b.location === 'sticky' && held ? steady : last;
            const view = shown(status);
            applyView(b.refs, view, present(view, env), b.location);
            paintBar(b.refs, view, status);
        }
        if (before !== null) {
            const d = container.getBoundingClientRect().height - before;
            if (d) window.scrollBy(0, d);
        }
        ctaWatch.refresh();
        // Broadcast rather than reach into the sticky bar from here: this
        // view owns the stream, not the page furniture that shows it.
        // `moving`: the chain is up -- something moves, or the viewer waits
        // (the badge has no sticky bar).
        document.dispatchEvent(new CustomEvent('torrent-status', {
            detail: { resourceId, state: last.state, moving: !!shown(last).sticky },
        }));
        // The view the player keeps changes with the player's own time too
        // (a paused buffer that stopped growing): drawn again every second
        // while there is one.
        const tick = !!last.view.plan || held || !!last.view.playing;
        if (tick && !ticker) ticker = setInterval(render, TICK_MS);
        if (!tick && ticker) {
            clearInterval(ticker);
            ticker = null;
        }
    };
    const activity = createPlayerActivity(document, { onChange: render });

    const onStatus = (status) => {
        if (isGap(status)) {
            if (!last || !isGap(last)) gapSince = Date.now();
        } else {
            steady = status;
        }
        last = status;
        render();
    };

    // Picking another file swaps #content and nothing above it: the stream
    // (and the file its download ETA prices) stays. A file that differs from
    // the one on the stream reopens it on the new one -- a second of the
    // viewer's link not drawn, against a plan box quoting another file's
    // wait. Set below, once the stream exists.
    let onSwap = null;

    const teardown = () => {
        if (onSwap) window.removeEventListener('async', onSwap);
        if (stickyVault) stickyVault.removeEventListener('click', onStickyVault);
        detailsStops.forEach((stop) => stop());
        ctaWatch.stop();
        activity.stop();
        if (ticker) {
            clearInterval(ticker);
            ticker = null;
        }
    };

    const csrfToken = container.dataset.csrf;
    if (!csrfToken) {
        container._statusTeardown = teardown;
        return;
    }

    const lang = document.documentElement.lang;
    const langPrefix = lang && lang !== 'en' ? `/${lang}` : '';
    // Dev-only preview (lib/statusDebug.js): the page URL's params ride
    // along to the stream; the server ignores them in release.
    let extra = debugQuery(window.location.search);
    // Page-issued, hash-bound, short-lived (handlers/resource/torrent_link.go).
    const statusToken = inner.dataset.statusToken || '';
    if (statusToken) extra += `&token=${encodeURIComponent(statusToken)}`;
    // The file the plan box's download ETA prices: the page's, then the one
    // a file link picks (onSwap).
    let file = inner.dataset.statusFile || '';
    // session=1: this page draws the chain, so the server builds the view and
    // follows the viewer's own thp session for it (the Vault dashboard's rows
    // open the same endpoint without it).
    const url = () => `${langPrefix}/${resourceId}/status?_csrf=${encodeURIComponent(csrfToken)}&session=1${extra}` +
        (file ? `&file=${encodeURIComponent(file)}` : '');
    // A final message (the server has nothing left to say: a vaulted
    // torrent whose viewer's link cannot be followed) closes the stream for
    // good -- left to itself EventSource would reconnect to the same answer
    // every few seconds.
    let finished = false;

    // The token lives an hour; a long download or vaulting is watched for
    // longer. When the stream is refused, reload this view the async way:
    // this.reload() (lib/async.js asyncLayout) re-fetches the page URL with
    // X-Layout "resource/status_inner", swaps in the fresh block with the
    // fresh token, and re-runs this init. Same URL, so the edge's challenge
    // clearance a person already holds lets it through and a client that
    // never loaded the page stops right here. At most once per
    // RELOAD_MIN_MS per view, so a dead stream never becomes a loop.
    const RELOAD_MIN_MS = 60 * 1000;
    const renew = () => {
        if (!statusToken || typeof container.reload !== 'function') return;
        const lastReload = container._statusReloadAt || 0;
        if (Date.now() - lastReload < RELOAD_MIN_MS) return;
        container._statusReloadAt = Date.now();
        // loadAsyncView only destroys views *inside* the target; this view is
        // the target, so drop our own listeners before the swap re-inits it.
        if (container._statusTeardown) { container._statusTeardown(); container._statusTeardown = null; }
        container.reload();
    };

    const open = () => {
        if (container._statusSource || container._statusGone || finished) return;
        const source = new EventSource(url());
        container._statusSource = source;
        // Keep-alive every 5 s: a chance to redraw for what changed without
        // a message (the player, another offer on screen).
        source.addEventListener('ping', render);
        // "vaulted" does not end this stream (it ends the Vault dashboard's):
        // vaulted content is served through the proxy too, and the viewer's
        // own link keeps changing -- until the server says it is final.
        source.onmessage = (e) => {
            let status;
            try {
                status = JSON.parse(e.data);
            } catch (err) {
                return;
            }
            onStatus(status);
            if (status.final) {
                finished = true;
                source.close();
                if (container._statusSource === source) container._statusSource = null;
            }
        };
        source.onerror = () => {
            // A refused stream (403 — token expired) closes the EventSource
            // for good; network blips reconnect on their own with the same URL.
            if (source.readyState === EventSource.CLOSED) {
                container._statusSource = null;
                if (container._statusGone) return;
                renew();
            }
        };
    };

    onSwap = (e) => {
        if (!e.detail || !e.detail.target || e.detail.target.id !== 'content') return;
        const picked = document.getElementById('file');
        const next = picked && picked.dataset.statusFile;
        if (!next || next === file) return;
        file = next;
        const source = container._statusSource;
        if (!source || finished) return;
        source.close();
        container._statusSource = null;
        open();
    };
    window.addEventListener('async', onSwap);

    // Opened at once: since 2026-09-09 a stream for a torrent nobody is
    // streaming is answered from disk without loading it (torrent-web-seeder
    // cold stats), so there is nothing left to defer.
    container._statusTeardown = teardown;
    open();

}, function() {
    const container = this;
    container._statusGone = true;
    if (container._statusTeardown) {
        container._statusTeardown();
        container._statusTeardown = null;
    }
    if (container._statusSource) {
        container._statusSource.close();
        container._statusSource = null;
    }
});

export {};
