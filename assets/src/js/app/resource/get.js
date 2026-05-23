import av from '../../lib/av';
import '../../lib/share/share';
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
