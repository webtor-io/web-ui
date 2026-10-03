import av from '../../lib/av';
import {
    NAVBAR_H, applyKeptBox, applyView, bindBlock, boxDismissed, createCtaWatch, dismissBox, initDetails, keepBox, mirrorBlock, newBoxMemory,
    paintBar, playerCause, playerLabel, playing, forPhase, present, upsellElsewhere,
} from '../../lib/transferStatus';
import { attr, hide } from '../../lib/inPlace';
import { createPlayerActivity } from '../../lib/playerActivity';
import { publishPlayerLabel } from '../../lib/playerLabel';
import { applyBadge } from '../../lib/statusBadge';
import { debugQuery } from '../../lib/statusDebug';
import { watchStatusStream } from '../../lib/statusStream';

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
// the card when its block grows or shrinks -- the plan box coming, closed by
// the viewer, or gone with another file -- even while the block is scrolled
// away, so a playing video jumps. There the page makes up for it itself
// (steadily).
const anchoring = () => typeof CSS !== 'undefined' && typeof CSS.supports === 'function' && CSS.supports('overflow-anchor', 'auto');

av(async function() {
    const container = this;
    // A renewal's fetch that lands after the page has gone (lib/
    // loadAsyncView.js swaps it into the container it was made for and runs
    // this there): nothing to come back to.
    if (!container.isConnected) return;
    const resourceId = container.dataset.resourceId;
    const inner = container.querySelector('#torrent-status-block');
    const card = inner && inner.querySelector('[data-tx]');
    if (!resourceId || !card) return;
    container._statusGone = false;

    // The plan box this page view has drawn, and the viewer's close of it
    // (lib/transferStatus.js keepBox): on the container, which the status
    // token's renewal keeps while it swaps the block and runs this again --
    // a box that is up stays up through it. A reload is a new container.
    const mem = container._txBox || (container._txBox = newBoxMemory());
    const blocks = [{ refs: bindBlock(card), location: 'card' }];
    // The fresh block of a renewal has no box yet (the page renders none: the
    // viewer is not known then) -- the kept one goes in before the copy is
    // made and before the first message, so the block never loses it.
    applyKeptBox(blocks[0].refs, mem, 'card');
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

    // The last status message -- kept on the container: a renewal's block is
    // the page's render ("checking" for a torrent nothing has been asked
    // about), and the new stream's first word comes only once the server has
    // tried the seeder's stats. The last word is drawn into it meanwhile
    // (below), not "checking" and the hairline for those seconds.
    let last = container._statusLast || null;
    let steady = last && !isGap(last) ? last : null; // the last one that was not a gap
    let gapSince = 0;
    let ticker = null;
    // Another file was picked and the stream reopened on it (onSwap), and
    // the new stream has not yet said anything but a gap: `last` and
    // `steady` are the old stream's word. The block and the player's label
    // are still drawn from it with this second's player, as before; the plan
    // box is not -- it priced the old file, and keepBox would take it as this
    // page view's.
    let swapped = false;
    // Scrolled away above the viewport, on an engine that does not anchor:
    // whatever the block gains or loses in height while `write` runs, the
    // page scrolls by, so nothing under it moves.
    const steadily = (write) => {
        let before = null;
        if (!anchoring()) {
            const r = container.getBoundingClientRect();
            if (r.bottom <= 0) before = r.height;
        }
        write();
        if (before !== null) {
            const d = container.getBoundingClientRect().height - before;
            if (d) window.scrollBy(0, d);
        }
    };
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
        // Preparation, player pause and a stopped download keep the viewer
        // on the route without borrowing the proxy's last speed. Playback
        // still bridges HLS request gaps using the existing playing view.
        const phase = activity.phase();
        const shown = (status) => forPhase(status.view,
            phase === 'none' && container._statusDownloadSeen && !status.view.nodes[2].show ? 'idle' : phase);
        const now = Date.now();
        const held = isGap(last) && steady && steady.view && now - gapSince < GAP_HOLD_MS;
        // The plan box: what present() picks this second, folded into what
        // this page view has drawn -- an up box stays until the viewer
        // closes it, and another offer on screen keeps a box from coming up
        // and takes an up one's button down (keepBox).
        const keep = { dismissed: boxDismissed(mem, now, window), elsewhere: env.upsellElsewhere };
        steadily(() => {
            for (const b of blocks) {
                const status = b.location === 'sticky' && held ? steady : last;
                const view = shown(status);
                const pres = present(view, env);
                applyView(b.refs, view, keepBox(mem, swapped ? { ...pres, box: null } : pres, view, keep), b.location);
                paintBar(b.refs, view, status);
            }
        });
        ctaWatch.refresh();
        // The player's buffering label: the lock and its card whenever the
        // view says the viewer is held at the cap with the stream box due,
        // outside the grace window and with no other offer up (the same env;
        // the player picks the waits to draw it at). From the view the
        // sticky bar keeps through a one-second gap in the data: the lock
        // would blink off there. Carried over to the player's bundle on
        // window (lib/playerLabel.js); published only when it changes. With
        // it the cause the view names when the wait is not the cap's
        // (playerCause: the swarm, no seeders, pieces nobody has, nothing
        // flowing): the lock the player draws by itself after its grace
        // answer gives way to that word. Both are the server's verdict, not
        // the block's drawing of the page's phase (forPhase): a session
        // seek's POST reads as preparation there, and the resting view it
        // draws has neither the plan nor the cause -- the lock went for the
        // seek and the cause was lost (review 2026-10-03).
        const raw = (held ? steady : last).view;
        const told = activity.streaming() ? playing(raw) : raw;
        publishPlayerLabel(playerLabel(told, env), window, playerCause(told));
        // Broadcast rather than reach into the sticky bar from here: this
        // view owns the stream, not the page furniture that shows it.
        // `moving`: the chain is up -- something moves, or the viewer waits
        // (the badge has no sticky bar).
        document.dispatchEvent(new CustomEvent('torrent-status', {
            detail: { resourceId, state: last.state, moving: !!shown(last).sticky },
        }));
        // The view the player keeps changes with the player's own time too
        // (a paused buffer that stopped growing): drawn again every second
        // while there is one. A box kept up follows other offers coming and
        // going (its button) the same way.
        const tick = !asleep && (phase !== 'none' || !!last.view.plan || held || !!last.view.playing || !!mem.box);
        if (tick && !ticker) ticker = setInterval(render, TICK_MS);
        if (!tick && ticker) {
            clearInterval(ticker);
            ticker = null;
        }
    };
    const activity = createPlayerActivity(document, { onChange: render });
    // Only an explicit download start retains the viewer afterwards. A final
    // probe sample can arrive after a preparation error or a cancelled card;
    // proxy traffic alone must not latch those as a download.
    const onDownload = (e) => {
        if (!e.target.closest('a[data-transfer-download]')) return;
        container._statusDownloadSeen = true;
        render();
    };
    document.addEventListener('click', onDownload, true);


    // Both copies of the box down, and the details' plan line with them,
    // where no message draws the block: the viewer's × before the first one,
    // another file picked (onSwap).
    const dropBoxes = () => {
        steadily(() => {
            for (const b of blocks) {
                for (const box of b.refs.boxes) hide(box.el, true);
                hide(b.refs.detailsPlan, true);
            }
        });
        ctaWatch.refresh();
    };

    // The viewer's × on the box, in the card's block or its copy in the
    // sticky bar: gone from both, and none in this browser for a day, on any
    // torrent (lib/transferStatus.js dismissBox). The click itself is
    // Umami's (data-umami-event="donate-status-bar-dismiss" on the ×, the
    // box's props). Focus on the × goes to what stays of its block: the
    // chain while it is up, the badge otherwise.
    const onDismiss = (e) => {
        e.preventDefault();
        const block = e.currentTarget.closest('[data-tx]');
        const focused = !!block && block.contains(document.activeElement);
        dismissBox(mem, Date.now(), window);
        render();
        // Before the first message there is nothing to render from.
        dropBoxes();
        if (focused) {
            const refs = blocks.find((b) => b.refs.block === block);
            const to = refs && (block.getAttribute('data-mode') === 'badge' ? refs.refs.badge && refs.refs.badge.el : refs.refs.toggle);
            if (to) to.focus({ preventScroll: true });
        }
    };
    const closes = blocks.flatMap((b) => b.refs.boxes.map((box) => box.close).filter(Boolean));
    for (const x of closes) x.addEventListener('click', onDismiss);

    const onStatus = (status) => {
        if (isGap(status)) {
            if (!last || !isGap(last)) gapSince = Date.now();
        } else {
            steady = status;
            swapped = false;
        }
        last = status;
        container._statusLast = status;
        render();
    };

    // Picking another file swaps #content and nothing above it: the stream
    // (and the file its download ETA prices) stays. A file that differs from
    // the one on the stream reopens it on the new one -- a second of the
    // viewer's link not drawn, against a plan box quoting another file's
    // wait. A box kept up goes with the old stream: the new file is a new
    // page view for it, as a reload would be (the owner's call, 2026-09-27:
    // no carrying it across); its close stays. It goes with the swap, the
    // page's own update, and no box is drawn from the old stream's word
    // (`swapped`): that word priced the old file, and the ticker, a
    // keep-alive or the new player draw from it long before the new stream
    // speaks (the server waits for the seeder's stats) -- keepBox took the
    // old box back as this page view's, and a gap as the new stream's first
    // word brought it back in the sticky bar from `steady` (review
    // 2026-09-27). Set below, once the stream exists.
    let onSwap = null;
    // The stream's life (lib/statusStream.js): set below, once there is one.
    let stream = null;
    // A hidden tab has let the stream go (lib/statusStream.js): no ticker.
    let asleep = false;

    const teardown = () => {
        if (stream) stream.stop();
        // No status, no word on the cap: the player's label goes plain.
        publishPlayerLabel(null);
        for (const x of closes) x.removeEventListener('click', onDismiss);
        if (onSwap) window.removeEventListener('async', onSwap);
        if (stickyVault) stickyVault.removeEventListener('click', onStickyVault);
        detailsStops.forEach((stop) => stop());
        ctaWatch.stop();
        activity.stop();
        document.removeEventListener('click', onDownload, true);
        if (ticker) {
            clearInterval(ticker);
            ticker = null;
        }
    };

    // On the block a renewal swaps in, not on the container: the CSRF pair is
    // the session cookie's, and a cookie changed since the page refused the
    // renewed stream again with the page's old one.
    const csrfToken = inner.dataset.csrf;
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

    // Renewing gives nothing (lib/statusStream.js): the status says it is
    // unavailable -- the badge statusview has for it, its words rendered with
    // the block -- in both copies, and takes its word on the cap back from
    // the player. Not the box: an up one stays, as through any state.
    const dead = () => {
        teardown();
        container._statusTeardown = null;
        container._statusLast = null;
        const label = inner.dataset.statusUnknown || '';
        for (const b of blocks) {
            attr(b.refs.block, 'data-key', 'status_unknown');
            attr(b.refs.block, 'data-mode', 'badge');
            attr(b.refs.block, 'data-sticky', false);
            applyBadge(b.refs.badge, { tone: 'muted', icon: 'unknown', label });
            attr(b.refs.bar, 'data-mode', 'divider');
            hide(b.refs.hint, true);
        }
        document.dispatchEvent(new CustomEvent('torrent-status', {
            detail: { resourceId, state: 'unknown', moving: false },
        }));
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
            stream.spoke();
            onStatus(status);
            if (status.final) {
                finished = true;
                source.close();
                if (container._statusSource === source) container._statusSource = null;
            }
        };
        source.onerror = () => {
            // A refused stream (403) closes the EventSource for good; network
            // blips reconnect on their own with the same URL.
            if (source.readyState === EventSource.CLOSED) {
                container._statusSource = null;
                if (container._statusGone) return;
                stream.refused();
            }
        };
    };
    stream = watchStatusStream(container, {
        open: () => {
            asleep = false;
            open();
        },
        close: () => {
            asleep = true;
            if (container._statusSource) container._statusSource.close();
            container._statusSource = null;
            if (ticker) {
                clearInterval(ticker);
                ticker = null;
            }
        },
        // loadAsyncView only destroys views *inside* the target; this view
        // is the target, so it drops its own listeners before the swap
        // re-inits it.
        teardown: () => {
            if (container._statusTeardown) {
                container._statusTeardown();
                container._statusTeardown = null;
            }
        },
        dead,
    });

    onSwap = (e) => {
        if (!e.detail || !e.detail.target || e.detail.target.id !== 'content') return;
        const picked = document.getElementById('file');
        const next = picked && picked.dataset.statusFile;
        if (!next || next === file) return;
        file = next;
        container._statusDownloadSeen = false;
        container._statusLast = null;
        swapped = true;
        mem.box = null;
        dropBoxes();
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
    render();
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
