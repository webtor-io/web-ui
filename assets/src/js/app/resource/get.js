import av from '../../lib/av';
import { waitForElement } from '../../lib/waitForElement';
import { findTextCut, trimTextCut } from '../../lib/textClamp';
import { initStickyStatus } from '../../lib/stickyStatus';
import '../../lib/share/share';
// Plot clamp: when the 3-line clamped paragraph overflows, cut the text
// at the longest fitting prefix (shared findTextCut from lib/textClamp)
// and append a clickable inline "\u2026" that expands the full plot; an
// inline "\u2191" at the end of the expanded text collapses it back.
// Mirrors the Preact ExpandableText used in Discover.
//
// ONE av() per script: lib/asyncView.js registers a script's init under its
// URL (`__async/assets/resource/get_loaded`) and runs only the first
// registration — a second av() in the same file is silently dropped. That
// is how the `#action=stream` deep link went dead in June 2026: the plot
// clamp was added as its own av() above it. Everything this script does
// on init therefore lives in the single callback at the bottom.
function initPlotClamp() {
    const plot = document.querySelector('[data-plot-clamp]');
    if (!plot) return;
    const full = plot.textContent;
    const cut = findTextCut(plot, full);
    if (cut == null) return;
    const cutText = trimTextCut(full, cut) + ' ';

    function toggleBtn(label, onClick) {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.textContent = label;
        btn.className = 'text-w-cyan hover:underline cursor-pointer font-bold';
        btn.addEventListener('click', onClick);
        return btn;
    }
    function renderCollapsed() {
        plot.style.maxHeight = '';
        plot.textContent = cutText;
        plot.appendChild(toggleBtn('\u2026', renderExpanded));
    }
    function renderExpanded() {
        plot.style.maxHeight = 'none';
        plot.textContent = full + ' ';
        plot.appendChild(toggleBtn('\u2191', renderCollapsed));
    }
    renderCollapsed();
}
av( async function() {
    initPlotClamp();
    // Before the early return below: the status mirror is for every visit to
    // this page, not only the ones that arrive with #action=stream.
    const stopSticky = initStickyStatus(document);
    this._stickyStatusStop = stopSticky;
    // Picking a file swaps #content only (views/resource/get.html), so this
    // view is not re-run and nothing scrolls. The card that just changed is
    // ABOVE the list the viewer clicked in -- bring it into view, under the
    // navbar and the sticky status (#file carries the scroll margin). Only
    // when it is actually out of sight: a short list needs no movement.
    const onContentSwap = (e) => {
        if (!e.detail || !e.detail.target || e.detail.target.id !== 'content') return;
        const file = document.getElementById('file');
        if (!file) return;
        const r = file.getBoundingClientRect();
        if (r.top >= 72 && r.top < window.innerHeight / 2) return;
        file.scrollIntoView({ block: 'start', behavior: 'smooth' });
    };
    window.addEventListener('async', onContentSwap);
    this._contentSwapStop = () => window.removeEventListener('async', onContentSwap);
    if (window._ads !== undefined && window._sessionExpired !== true) {
        const renderAd = (await import('../../lib/ads')).default;
        for (const ad of window._ads) {
            renderAd(this, ad);
        }
    }
    const query = window.location.hash.replace('#', '');
    const urlParams = new URLSearchParams(query);
    const action = urlParams.get('action');
    const modal = urlParams.get('modal');
    const purge = urlParams.get('purge');
    const debug = urlParams.get('debug');
    if (!action) return;
    // "stream" is a shorthand — try stream-video first, then stream-audio.
    const findForm = () => document.querySelector('form.' + action)
        || (action === 'stream' ? (document.querySelector('form.stream-video') || document.querySelector('form.stream-audio')) : null);
    // The av() queue is drained before the file card (and its form) is in
    // the DOM, so a one-shot lookup here found nothing and the deep link did
    // nothing at all. Wait for the form instead (lib/waitForElement.js).
    const form = await waitForElement(findForm);
    if (!form) return;
    if (purge) {
        const purgeInput = document.createElement('input');
        purgeInput.setAttribute('type', 'hidden');
        purgeInput.setAttribute('name', 'purge');
        purgeInput.setAttribute('value', 'true');
        form.appendChild(purgeInput);
    }
    if (debug) {
        const debugInput = document.createElement('input');
        debugInput.setAttribute('type', 'hidden');
        debugInput.setAttribute('name', 'debug');
        debugInput.setAttribute('value', debug);
        form.appendChild(debugInput);
    }
    form.requestSubmit();
    if (modal) {
        window.addEventListener('player_ready', function () {
            if (!modal) return;
            const checkbox = document.getElementById(modal + '-checkbox');
            checkbox.checked = true;
        });
    }
}, function () {
    if (this._contentSwapStop) {
        this._contentSwapStop();
        this._contentSwapStop = null;
    }
    if (this._stickyStatusStop) {
        this._stickyStatusStop();
        this._stickyStatusStop = null;
    }
});
