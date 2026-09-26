// The transfer status block on the resource page: the chain "swarm ▸ cache ▸
// you" while something moves (only who takes part), the old badge while
// nothing does, the piece bar, the hint or the plan box (partials/resource/
// status.html, docs/transfer_status.html). The server builds the whole view,
// already localized (services/statusview), and sends it on every status
// stream message; this module moves it into the block. Which of the chain
// and the badge shows is the view's `mode` -- both are in the block for good,
// in one row, and the block's data-mode picks one.
//
// STABLE DOM. The block's nodes are built once, by the server. Every update
// writes text, attributes and cell fills into them, and writes only what
// changed. The v1 badge was re-created with innerHTML on every message: its
// animations restarted every second, a click on its link landed on a node
// that was gone, focus dropped. Here the line, the label and the arrowhead of
// a segment are the same elements for the life of the page, so the one sweep
// per segment runs on; the plan box's button is the same <a>, so a click in
// the middle of an update still lands. The piece bar's cells are made once
// (the first bar) and then only restyled.
//
// What only the page knows is its player. The server cannot tell a stream
// from a download (both are bytes to one session), so for a plan-limited
// state it sends both variants and present() picks one: the player buffering,
// or playing a file that needs more than the cap -> the stream box (the
// latter not once the viewer has answered the grace popup, until the
// player's first real stall); the player playing anything else -> the fact
// without a button, or nothing for a file under the cap with room to spare;
// no player -> the download box.
// The player is also what keeps the viewer on the chain between two HLS
// segments, where the proxy honestly counts no request of theirs open: the
// server sends the view with them still there next to its own, and
// playing() picks it while the player streams.

import { impressionKey } from './impression';
import { attr, hide, text } from './inPlace';
import { applyBadge, bindBadge } from './statusBadge';

const HAVE_POPOVER_API = (el) => !!el && typeof el.showPopover === 'function';

// The fixed navbar over the page (`top-[72px]` under it in the markup, and
// stickyStatus.js's NAVBAR_H): what sits under it is not on screen.
export const NAVBAR_H = 72;

// Only our own paths and https links become an href. The URL is the server's
// (a /trial or /donate path, or the plan's checkout from the catalog), but a
// javascript: URL must never reach an anchor, whoever sent it.
export function safeHref(url) {
    if (typeof url !== 'string' || !url) return '';
    if (url.startsWith('/') && !url.startsWith('//')) return url;
    try {
        return new URL(url).protocol === 'https:' ? url : '';
    } catch (e) {
        return '';
    }
}

// present is what the block shows of a view, given what the page knows and
// the server does not:
//   player          'none' | 'playing' | 'buffering' (lib/playerActivity.js)
//   stallSub        the player's own "…, and this file needs 8 Mbps" line
//                   (data-status-stall-sub, from the stream job), preferred
//                   over the stream box's line, which cannot know the bitrate
//   fitsCap         the stream job marked the played file as needing no more
//                   than the cap, with a margin (data-status-fits-cap,
//                   statusview.FitsMargin): nothing is said under the bar
//                   while it plays. Only that -- a real stall of it while
//                   the server says the limiter binds is the cap's doing and
//                   gets the stream box like any other (The Knick, 2026-09-26:
//                   marked "fits" at 4.34 of 5, it pulled 5.04 and stalled
//                   four times in 180 s with the cap line alone on the page)
//   overCap         the stream job marked the played file as needing more
//                   than the cap (data-status-over-cap): at the cap it will
//                   stall once its buffer runs out, so the stream box comes as
//                   soon as the server says it is due, playing or not
//   inGrace         the page's player is inside its free grace window by movie
//                   time (playerActivity inGrace): not the plan's cap, and
//                   nothing is sold -- though the server's verdict can be on
//                   there (hls.js fetches the segments past the window ahead,
//                   at the cap)
//   upsellElsewhere another offer is on screen (the grace popup, the cap
//                   modal, the download nudge), or the grace popup is on its
//                   way (playerActivity graceOfferDue): one offer at a time,
//                   so the box gives way to its line
//   offerAnswered   the viewer answered an offer about the cap (the grace
//                   popup's "continue at N Mbps" or its close, the
//                   slow-download modal's "watch as is") and the player has
//                   not stalled for
//                   real since (playerActivity offerAnswered): the popup has
//                   just told them the cap is coming, so a file over the cap
//                   is not sold again until they hit it -- no box, and no
//                   line either (the fact's "no stops" is false for it);
//                   its first real stall ends this, and the stream box
//                   comes as at any capped stall (owner, 2026-09-26)
// It returns the refined state key (docs/transfer_status.html: tier ->
// tier_dl / stream_ok / stream_stall / stream_over; cached_tier and
// vaulted_tier keep theirs for a download), the analytics context, the hint
// or the box, and the viewer segment's tone when it is not the server's.
export function present(view, env = {}) {
    const out = { key: view.key, ctx: '', hint: view.hint || '', hintTone: '', box: null, viewerTone: '', vault: !!view.vault };
    const plan = view.plan;
    if (!plan) return out;
    const player = env.player || 'none';
    // The player keeps up under the cap -- or cannot tell yet: the link to the
    // viewer is cyan (the data flows, nobody waits) and nothing is sold. The
    // fact is said only where the owner kept it (2026-09-25/26): a file
    // under the cap with room gets no line at all, one over it will stop
    // (and is sold the way out below), and one of unknown bitrate -- or
    // within the margin of the cap -- keeps the fact.
    const calm = (hint) => ({ ...out, key: 'stream_ok', ctx: 'stream', hint, viewerTone: 'flow' });
    if (env.inGrace && player !== 'none') {
        // Inside the grace window nothing is sold, whatever the server's
        // verdict: the cap is said once, as the cap, if the player waits.
        if (player === 'buffering') return { ...out, key: 'stream_stall', ctx: 'stream', hint: plan.cap || '' };
        return calm(env.overCap || env.fitsCap ? '' : plan.fact || '');
    }
    if (player === 'playing' && !env.overCap) return calm(env.fitsCap ? '' : plan.fact || '');
    // A file over the cap, an offer about the cap answered (the grace popup,
    // or "watch as is" on the slow-download modal before playback), no real
    // stall since (a 'buffering' here is the minute after one that ended
    // before the answer -- the popup's to speak of): as before the box is
    // due, the pink link says the cap and nothing stands under the bar.
    // Without an answer (no grace popup and no modal: paid tiers whose gate
    // passed, grace off) the box comes once due as ever.
    if (env.offerAnswered && env.overCap && player !== 'none') return { ...out, key: 'stream_over', ctx: 'stream', hint: '' };
    // A stream past this point: buffering, or playing a file over the cap.
    // Buffering is sold the way out whatever the stream job estimated: the
    // server sends the stream box only once thp's limiter has held the
    // viewer at the cap for 8 s (throttled half the window or more, 0.9 of
    // the cap used; statusview meter), and a real stall then is the cap's
    // doing -- fitsCap keeps only its playing-case meaning above.
    const stream = player !== 'none';
    const stalled = player === 'buffering';
    const variant = (stream ? plan.stream : plan.download) || {};
    const stall = stream ? env.stallSub || '' : '';
    let key = view.key === 'tier' ? 'tier_dl' : view.key;
    if (stream) key = stalled ? 'stream_stall' : 'stream_over';
    const ctx = stream ? 'stream' : 'download';
    const box = variant.box;
    // The cap has not held long enough for the box yet (the server sends
    // the variants only once it has, statusview.PlanBoxAfter): the pink link
    // says the cap, nothing is sold, and no line stands in for the box --
    // the block grows once, by the box, not by a line and then the box.
    if (!box && !variant.hint) return { ...out, key, ctx, hint: '' };
    if (box && safeHref(box.cta && box.cta.url) && !env.upsellElsewhere) {
        return { ...out, key, ctx, hint: '', box: { ...box, sub: stall || box.sub || '', cta: { ...box.cta } } };
    }
    // No button -- nothing faster on sale, or another offer already on
    // screen: the cap is still said, as the line under the bar.
    let hint = stall || variant.hint || '';
    if (!hint && box) hint = stream ? box.sub || box.title : box.title;
    return { ...out, key, ctx, hint: hint || '', hintTone: 'plan' };
}

// playerLabel is what the page's player says on its buffering label
// (lib/player/BufferingLabel.jsx; carried there by lib/playerLabel.js): null
// -- the plain "Buffering" -- or the lock with the viewer's cap and the card
// behind it. The lock says "this stall is the plan's cap", so it is drawn
// exactly where the status itself sells the stream box at a stall: the
// player stalled for real (the verdict 'buffering'), the server's verdict at
// the cap with the box due and something faster on sale, no other offer on
// screen or on its way (the grace popup), outside the free grace window, no
// answered offer standing -- present() decides all of it, and this only
// asks it (its stream_stall is the player's 'buffering' and nothing else).
// A swarm or network stall has no plan, and a cap that has not held long
// enough for the box has no card: the plain label either way. The card is
// that very box -- its words, the player's own "…and this file needs N" --
// with the link of the player's own surface (plan.player.url, built by the
// server: /trial?from=player-label).
export function playerLabel(view, env = {}) {
    const own = view && view.plan && view.plan.player;
    if (!own || !own.rate) return null;
    const pres = present(view, env);
    const url = safeHref(own.url);
    if (pres.key !== 'stream_stall' || !pres.box || !url) return null;
    const cta = pres.box.cta || {};
    return {
        rate: own.rate,
        title: pres.box.title || '',
        sub: pres.box.sub || '',
        cta: { label: cta.label || '', note: cta.note || '', url },
        // The status box's props, for the card's own events.
        props: { ctx: pres.ctx, location: 'player', auth: view.auth || '', state: pres.key, tier: view.tier || '', target: cta.target || '' },
    };
}

// playing is a view as the page shows it while its own player streams
// (lib/playerActivity.js streaming): the server's whole view with the viewer
// on the chain at their last reading (view.playing, sent only while the
// proxy counts no request of theirs open). An HLS player closes its request
// between two segments -- honestly nothing open there -- and without this
// the chain fell to the badge in every such gap while the film played on.
// The same nodes, only shown. (Inside the free grace window too, since grace
// segment tokens carry the viewer's session and the proxy counts them; the
// view "without the reading" the page used to show there, unmetered, is
// gone with the blind spot.)
export function playing(view) {
    const p = view && view.playing;
    if (!p) return view;
    return { ...p, playing: null };
}

// Another offer is on screen (data-upsell-surface: the grace popup over the
// player, the cap modal, the download nudge). A surface that is closed sits
// under `.hidden` / [hidden] (the grace popup toggles the class; a closed
// progress log gets it) or in a closed <dialog>.
export function upsellElsewhere(doc = document) {
    for (const el of doc.querySelectorAll('[data-upsell-surface]')) {
        if (el.closest('[hidden], .hidden')) continue;
        const dialog = el.closest('dialog');
        if (dialog && !dialog.open) continue;
        if (typeof el.checkVisibility === 'function' && !el.checkVisibility()) continue;
        return true;
    }
    return false;
}

// ---- writing into the DOM, only what changed (lib/inPlace.js) ----

// bindBlock finds the block's slots once. Cached on the element: a block is
// bound for the life of the page.
export function bindBlock(block) {
    if (block._tx) return block._tx;
    const all = (sel) => Array.from(block.querySelectorAll(sel));
    const one = (sel) => block.querySelector(sel);
    const refs = {
        block,
        toggle: one('[data-tx-toggle]'),
        nodes: all('[data-tx-node]').map((el) => ({
            el,
            use: el.querySelector('.tx-ic use'),
            name: el.querySelector('.tx-nm'),
            value: el.querySelector('.tx-val'),
            short: el.querySelector('.tx-short'),
            caption: el.querySelector('.tx-cap'),
        })),
        segs: all('[data-tx-seg]').map((el) => ({
            el,
            speed: el.querySelector('.tx-spd'),
            note: el.querySelector('.tx-note'),
        })),
        // The badge of the badge's mode, in the chain's row: the status
        // badge every page draws its pills with (lib/statusBadge.js).
        badge: bindBadge(one('[data-tx-badge]')),
        bar: one('[data-tx-bar]'),
        cellsHost: one('[data-tx-cells]'),
        cells: [],
        hint: one('[data-tx-hint]'),
        hintText: one('[data-tx-htext]'),
        // Only where Vault is configured (the server leaves it out).
        vault: one('[data-tx-vault]'),
        // Two: under the bar, and at the foot of the details.
        boxes: all('[data-tx-pbox]').map((el) => ({
            el,
            title: el.querySelector('[data-tx-pt]'),
            sub: el.querySelector('[data-tx-ps]'),
            cta: el.querySelector('[data-tx-cta]'),
            label: el.querySelector('[data-tx-cta-label]'),
            note: el.querySelector('[data-tx-pn]'),
        })),
        details: one('[data-tx-details]'),
        detailsPlan: one('[data-tx-dplan]'),
        rows: all('[data-tx-row]').map((el) => ({
            el,
            label: el.querySelector('[data-tx-rlabel]'),
            tag: el.querySelector('[data-tx-rtag]'),
            sub: el.querySelector('[data-tx-rsub]'),
            value: el.querySelector('[data-tx-rv]'),
        })),
    };
    block._tx = refs;
    return refs;
}

function applyBox(b, pres, view, location) {
    const box = pres.box;
    hide(b.el, !box);
    if (!box) return;
    const cta = box.cta || {};
    text(b.title, box.title || '');
    text(b.sub, box.sub || '');
    text(b.label, cta.label || '');
    text(b.note, cta.note || '');
    attr(b.cta, 'href', safeHref(cta.url) || null);
    // The click is Umami's own (data-umami-event on the <a>); the impression
    // (createCtaWatch) reads the same props.
    attr(b.cta, 'data-umami-event-ctx', pres.ctx);
    attr(b.cta, 'data-umami-event-location', location);
    attr(b.cta, 'data-umami-event-auth', view.auth || '');
    attr(b.cta, 'data-umami-event-state', pres.key);
    attr(b.cta, 'data-umami-event-tier', view.tier || '');
    attr(b.cta, 'data-umami-event-target', cta.target || '');
}

// applyView writes a view, as present() refined it, into a bound block.
// location: 'card' | 'sticky', for the analytics props.
export function applyView(refs, view, pres, location) {
    // A server that sends no mode is one from before the badge: the chain.
    const mode = view.mode === 'badge' ? 'badge' : 'chain';
    const was = refs.block.getAttribute('data-mode');
    const doc = refs.block.ownerDocument;
    const focused = doc.activeElement;
    attr(refs.block, 'data-key', pres.key);
    attr(refs.block, 'data-mode', mode);
    attr(refs.block, 'data-sticky', !!view.sticky);
    // The details belong to the chain: when it gives way to the badge, an
    // open popover goes with it (in the top layer it would stay, anchored
    // to nothing visible).
    if (mode === 'badge' && was !== 'badge' && refs.details) {
        const Ev = doc.defaultView.CustomEvent;
        refs.details.dispatchEvent(new Ev('tx-close'));
    }
    applyBadge(refs.badge, view.badge);
    (view.nodes || []).forEach((n, i) => {
        const r = refs.nodes[i];
        if (!r || !n) return;
        hide(r.el, !n.show);
        attr(r.el, 'data-kind', n.kind || '');
        attr(r.el, 'data-tone', n.tone || '');
        attr(r.el, 'data-dim', !!n.dim);
        if (n.icon) attr(r.use, 'href', `#tx-i-${n.icon}`);
        text(r.name, n.name || '');
        text(r.value, n.value || '');
        text(r.short, n.short || '');
        text(r.caption, n.caption || '');
    });
    (view.segs || []).forEach((s, i) => {
        const r = refs.segs[i];
        if (!r || !s) return;
        const tone = i === 1 && pres.viewerTone && s.tone === 'plan' ? pres.viewerTone : s.tone || 'off';
        hide(r.el, !s.show);
        attr(r.el, 'data-tone', tone);
        attr(r.el, 'data-moving', !!s.on);
        attr(r.el, 'data-dots', !!s.dots);
        text(r.speed, s.speed || '');
        text(r.note, s.note ? `· ${s.note}` : '');
    });
    // The words in their own span: the Vault link after them is the same
    // element for the life of the page.
    text(refs.hintText || refs.hint, pres.hint || '');
    hide(refs.hint, !pres.hint);
    attr(refs.hint, 'data-tone', pres.hintTone || '');
    hide(refs.vault, !pres.vault);
    for (const b of refs.boxes) applyBox(b, pres, view, location);
    hide(refs.detailsPlan, !pres.box);
    const rows = (view.details && view.details.rows) || [];
    rows.forEach((row, i) => {
        const r = refs.rows[i];
        if (!r || !row) return;
        hide(r.el, !row.show);
        attr(r.el, 'data-key', row.key || '');
        text(r.label, row.label || '');
        text(r.tag, row.tag || '');
        hide(r.tag, !row.tag);
        text(r.sub, row.sub || '');
        text(r.value, row.value || '');
    });
    keepFocus(refs, focused, mode);
}

// keepFocus: focus that was in the block stays in it. The chain goes
// invisible when it gives way to the badge (a hidden element loses focus),
// its details close with it, a plan box's button can go hidden; left to
// the browser, focus fell to <body> -- a keyboard or screen-reader user
// reading the details lost their place to a change they did not make, ten
// seconds of stillness. The badge takes it then (tabindex -1: no tab stop
// of its own) and hands it back to the chain when the chain returns.
function keepFocus(refs, before, mode) {
    const doc = refs.block.ownerDocument;
    const badge = refs.badge && refs.badge.el;
    const chain = refs.toggle;
    if (!badge || !chain) return;
    if (mode === 'chain') {
        if (doc.activeElement === badge) chain.focus({ preventScroll: true });
        return;
    }
    if (!before || before === badge || !refs.block.contains(before)) return;
    const gone = before === chain || (refs.details && refs.details.contains(before)) || !!before.closest('[hidden]');
    if (gone) badge.focus({ preventScroll: true });
}

function decodeBytes(b64) {
    try {
        const bin = atob(b64);
        const out = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
        return out;
    } catch (e) {
        return null;
    }
}

// paintBar draws the piece bar from a status message: `pieces` is one byte
// per bucket (0..255, the share of the bucket the seeder holds), `active` a
// bitset of the buckets being fetched, `missing` a bitset of the buckets
// holding a piece nobody connected has -- hatched, in either mode, and over
// the pulse: a piece being fetched that nobody has is not coming. The view
// says whether to draw the bar and in which colour; without cells to draw,
// the hairline divider.
export function paintBar(refs, view, status) {
    if (!refs.bar) return;
    const want = !!(view.bar && view.bar.mode === 'pieces');
    const fill = want && status && status.pieces ? decodeBytes(status.pieces) : null;
    const on = !!(fill && fill.length);
    attr(refs.bar, 'data-mode', on ? 'pieces' : 'divider');
    attr(refs.bar, 'data-tone', (view.bar && view.bar.tone) || 'flow');
    if (!on || !refs.cellsHost) return;
    const active = status.active ? decodeBytes(status.active) : null;
    const missing = status.missing ? decodeBytes(status.missing) : null;
    if (refs.cells.length !== fill.length) {
        // The one time cells are made: the first bar of the page (or a
        // server that changed its bucket count).
        const doc = refs.cellsHost.ownerDocument;
        const cells = [];
        const frag = doc.createDocumentFragment();
        for (let i = 0; i < fill.length; i++) {
            const c = doc.createElement('span');
            frag.appendChild(c);
            cells.push(c);
        }
        refs.cellsHost.replaceChildren(frag);
        refs.cells = cells;
    }
    const mask = holesMask(missing, fill.length);
    if (refs.cellsHost._holes !== mask) {
        // One hatch under the whole bar, shown where the missing cells are
        // (style.css .tx-pbar::before): stripes that run on across a run of
        // cells instead of starting over in each.
        if (mask) refs.cellsHost.style.setProperty('--tx-holes', mask);
        else refs.cellsHost.style.removeProperty('--tx-holes');
        attr(refs.cellsHost, 'data-holes', !!mask);
        refs.cellsHost._holes = mask;
    }
    for (let i = 0; i < fill.length; i++) {
        const c = refs.cells[i];
        const f = (fill[i] / 255).toFixed(2);
        if (c._fill !== f) {
            c.style.setProperty('--fill', f);
            c._fill = f;
        }
        const m = !!(missing && missing[i >> 3] & (1 << (i & 7)));
        if (c._missing !== m) {
            c.classList.toggle('m', m);
            c._missing = m;
        }
        const a = !m && !!(active && active[i >> 3] & (1 << (i & 7)));
        if (c._active !== a) {
            c.classList.toggle('a', a);
            c._active = a;
        }
    }
    const label = status.pieces_label || '';
    attr(refs.cellsHost, 'aria-label', label || null);
    attr(refs.cellsHost, 'title', label || null);
}

// The attributes that name another element by id (a space-separated list
// for the aria ones).
const ID_REFS = ['popovertarget', 'aria-describedby', 'aria-labelledby', 'aria-controls'];

// holesMask is the mask that shows the bar's hatch over the missing cells:
// one opaque band per run of them, as a share of the bar; '' for none.
export function holesMask(missing, n) {
    const bit = (i) => !!(missing && missing[i >> 3] & (1 << (i & 7)));
    const pct = (i) => `${+((i / n) * 100).toFixed(4)}%`;
    const bands = [];
    for (let i = 0; i < n; i++) {
        if (!bit(i)) continue;
        let j = i;
        while (j < n && bit(j)) j++;
        bands.push(`transparent ${pct(i)}, #000 ${pct(i)} ${pct(j)}, transparent ${pct(j)}`);
        i = j;
    }
    return bands.length ? `linear-gradient(90deg, ${bands.join(', ')})` : '';
}

// mirrorBlock puts a copy of the card's block into the sticky bar: the very
// same markup (owner, 2026-09-24: no compact variant), made once. Ids get a
// suffix, and whatever names them follows: the buttons that open or close
// the details, the chain's description (the badge's words).
export function mirrorBlock(block, host, suffix = 'sticky') {
    const clone = block.cloneNode(true);
    const renamed = new Map();
    for (const el of clone.querySelectorAll('[id]')) {
        renamed.set(el.id, `${el.id}-${suffix}`);
        el.id = renamed.get(el.id);
    }
    for (const name of ID_REFS) {
        for (const el of clone.querySelectorAll(`[${name}]`)) {
            const ids = el.getAttribute(name).split(/\s+/).filter(Boolean);
            el.setAttribute(name, ids.map((id) => renamed.get(id) || id).join(' '));
        }
    }
    host.replaceChildren(clone);
    return clone;
}

// initDetails wires "where the bottleneck is". With the Popover API the
// markup already works on its own (popovertarget: opens from the chain,
// closes on Esc and on a press outside); this only places it under the chain
// -- the browser would centre it -- keeps it there while the page scrolls,
// closes it when the chain is gone from view (off the window, or under the
// fixed navbar: topInset) or when its block is (a `tx-close` event on the
// details: the sticky bar sliding away), and keeps aria-expanded true to
// what is on screen. Without the API the same button opens it in flow.
// Returns the teardown.
export function initDetails(refs, win = window, { topInset = 0 } = {}) {
    const { toggle, details } = refs;
    if (!toggle || !details) return () => {};
    const doc = details.ownerDocument;
    const GAP = 8;
    // The least room worth scrolling in rather than covering the chain.
    const MIN_ROOM = 200;
    let open = false;

    const place = (sized) => {
        const b = toggle.getBoundingClientRect();
        const gone = (b.width === 0 && b.height === 0) || b.bottom <= topInset || b.top >= win.innerHeight;
        if (sized && gone) {
            close();
            return;
        }
        let top = b.bottom + GAP;
        let left = b.left;
        let maxHeight = '';
        // Measuring means lifting the cap for a moment, which clamps the
        // box's own scroll to the top: put it back after.
        const scrolled = details.scrollTop;
        if (sized) {
            // Below the chain if it fits, above if that fits; otherwise on
            // the roomier side, scrolling inside -- never over the chain
            // (unless neither side has room for a useful part of it).
            details.style.maxHeight = '';
            const m = details.getBoundingClientRect();
            const below = win.innerHeight - GAP - top;
            const above = b.top - 2 * GAP;
            if (m.height > below) {
                if (m.height <= above) {
                    top = b.top - GAP - m.height;
                } else if (Math.max(below, above) >= MIN_ROOM) {
                    const room = Math.max(below, above);
                    maxHeight = `${Math.floor(room)}px`;
                    top = below >= above ? top : GAP;
                } else {
                    top = Math.max(GAP, win.innerHeight - GAP - m.height);
                }
            }
            left = Math.min(left, win.innerWidth - GAP - m.width);
        }
        details.style.maxHeight = maxHeight;
        details.style.inset = 'auto';
        details.style.margin = '0';
        details.style.top = `${Math.round(top)}px`;
        details.style.left = `${Math.round(Math.max(GAP, left))}px`;
        if (scrolled && details.scrollTop !== scrolled) details.scrollTop = scrolled;
    };
    // The page's scroll moves the chain; the box's own scroll does not --
    // and re-placing on it snapped the box back to its top on every wheel
    // tick, so a capped box could never be read to its end.
    const onMove = (e) => {
        const t = e && e.type === 'scroll' ? e.target : null;
        if (t && t.nodeType === 1 && details.contains(t)) return;
        place(true);
    };
    const onClose = () => close();
    const opened = () => {
        open = true;
        attr(toggle, 'aria-expanded', 'true');
        win.addEventListener('scroll', onMove, true);
        win.addEventListener('resize', onMove);
    };
    const closed = () => {
        open = false;
        attr(toggle, 'aria-expanded', 'false');
        win.removeEventListener('scroll', onMove, true);
        win.removeEventListener('resize', onMove);
    };
    let close;

    if (HAVE_POPOVER_API(details)) {
        close = () => {
            try {
                details.hidePopover();
            } catch (e) {
                /* already closed */
            }
        };
        // beforetoggle: placed before the first frame (the size is not known
        // yet, so below the chain); toggle: clamped to the screen with it.
        const onBefore = (e) => {
            if (e.newState === 'open') place(false);
        };
        const onToggle = (e) => {
            if (e.newState === 'open') {
                opened();
                place(true);
            } else {
                closed();
            }
        };
        details.addEventListener('beforetoggle', onBefore);
        details.addEventListener('toggle', onToggle);
        details.addEventListener('tx-close', onClose);
        return () => {
            details.removeEventListener('beforetoggle', onBefore);
            details.removeEventListener('toggle', onToggle);
            details.removeEventListener('tx-close', onClose);
            if (open) close();
            closed();
        };
    }

    // No Popover API: the `popover` attribute means nothing here and the
    // details sit in flow under the chain (style.css), opened by .is-open.
    const set = (now) => {
        details.classList.toggle('is-open', now);
        if (now) opened();
        else closed();
    };
    close = () => set(false);
    const onClick = (e) => {
        e.preventDefault();
        set(!open);
    };
    const closeBtns = Array.from(details.querySelectorAll('[data-tx-close]'));
    const onCloseBtn = (e) => {
        e.preventDefault();
        set(false);
        toggle.focus();
    };
    const onDown = (e) => {
        if (open && !details.contains(e.target) && !toggle.contains(e.target)) set(false);
    };
    const onKey = (e) => {
        if (open && e.key === 'Escape') {
            set(false);
            toggle.focus();
        }
    };
    toggle.addEventListener('click', onClick);
    closeBtns.forEach((b) => b.addEventListener('click', onCloseBtn));
    details.addEventListener('tx-close', onClose);
    doc.addEventListener('pointerdown', onDown, true);
    doc.addEventListener('keydown', onKey);
    return () => {
        toggle.removeEventListener('click', onClick);
        closeBtns.forEach((b) => b.removeEventListener('click', onCloseBtn));
        details.removeEventListener('tx-close', onClose);
        doc.removeEventListener('pointerdown', onDown, true);
        doc.removeEventListener('keydown', onKey);
        closed();
    };
}

// ---- the plan box's impression ----

export const IMPRESSION = 'donate-status-bar-shown';
export const DWELL_MS = 1000;
const PROPS = ['ctx', 'location', 'auth', 'state', 'tier', 'target'];

// The props the click carries (data-umami-event-*), for the impression.
export function ctaProps(a) {
    const props = {};
    for (const k of PROPS) props[k] = a.getAttribute(`data-umami-event-${k}`) || '';
    return props;
}

// Per page load, and on window, not in this module: a module imported by two
// entries is two copies with two registries (CLAUDE.md, shared JS state).
function pageRegistry() {
    if (typeof window === 'undefined') return new Set();
    if (!window._txImpressions) window._txImpressions = new Set();
    return window._txImpressions;
}

// createCtaWatch counts an impression of a plan box's button once it has been
// at least half on screen for a second (a box that flashes by, or sits under
// the fold or under the fixed navbar -- rootMargin -- was not seen), once
// per page per set of props -- the same button for another state or in the
// sticky bar is another impression. The click has its own event
// (data-umami-event="donate-status-bar").
export function createCtaWatch({
    umami, IO = globalThis.IntersectionObserver, dwellMs = DWELL_MS, registry = pageRegistry(),
    rootMargin = `-${NAVBAR_H}px 0px 0px 0px`,
} = {}) {
    const watched = new Map(); // <a> -> { visible, timer }
    const shown = (a) => !a.closest('[hidden]') && a.hasAttribute('href');
    const fire = (a) => {
        const props = ctaProps(a);
        const key = impressionKey(IMPRESSION, props);
        if (registry.has(key)) return;
        registry.add(key);
        const u = umami || (typeof window !== 'undefined' ? window.umami : null);
        if (u && typeof u.track === 'function') u.track(IMPRESSION, props);
    };
    const arm = (a) => {
        const st = watched.get(a);
        if (!st || !st.visible || st.timer || !shown(a)) return;
        if (registry.has(impressionKey(IMPRESSION, ctaProps(a)))) return;
        st.timer = setTimeout(() => {
            st.timer = null;
            if (st.visible && shown(a)) fire(a);
        }, dwellMs);
    };
    const io = typeof IO === 'function'
        ? new IO((entries) => {
            for (const e of entries) {
                const st = watched.get(e.target);
                if (!st) continue;
                st.visible = e.isIntersecting && e.intersectionRatio >= 0.5;
                if (!st.visible && st.timer) {
                    clearTimeout(st.timer);
                    st.timer = null;
                }
                arm(e.target);
            }
        }, { threshold: [0, 0.5, 1], rootMargin })
        : null;
    return {
        watch(a) {
            if (!a || watched.has(a)) return;
            watched.set(a, { visible: false, timer: null });
            if (io) io.observe(a);
        },
        // After an update: a button on screen whose props changed (another
        // state, another variant) starts its own second.
        refresh() {
            for (const a of watched.keys()) arm(a);
        },
        stop() {
            if (io) io.disconnect();
            for (const st of watched.values()) if (st.timer) clearTimeout(st.timer);
            watched.clear();
        },
    };
}
