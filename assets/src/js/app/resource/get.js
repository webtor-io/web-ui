import av from '../../lib/av';
import { findTextCut, trimTextCut } from '../../lib/textClamp';
import '../../lib/share/share';
// Plot clamp: when the 3-line clamped paragraph overflows, cut the text
// at the longest fitting prefix (shared findTextCut from lib/textClamp)
// and append a clickable inline "\u2026" that expands the full plot; an
// inline "\u2191" at the end of the expanded text collapses it back.
// Mirrors the Preact ExpandableText used in Discover.
av(function () {
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
});
av( async function() {
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
    let form = document.querySelector('form.' + action);
    // "stream" is a shorthand — try stream-video first, then stream-audio
    if (!form && action === 'stream') {
        form = document.querySelector('form.stream-video') || document.querySelector('form.stream-audio');
    }
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
});
