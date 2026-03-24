/**
 * Chromecast plugin — placeholder.
 * TODO: Port ChromecastPlayer from mediaelement-plugins/chromecast/player.js
 * Requires removing mejs.Utils.createEvent and media.originalNode dependencies.
 */

export function initChromecast(videoEl, containerEl) {
    // Chromecast will be ported in a follow-up
    // For now, add the cast button if Cast API is available
    if (!window.cast && !window.chrome?.cast) {
        const s = document.createElement('script');
        s.type = 'text/javascript';
        s.src = 'https://www.gstatic.com/cv/js/sender/v1/cast_sender.js?loadCastFramework=1';
        document.body.appendChild(s);
    }

    const castButton = document.createElement('div');
    castButton.className = 'wt-player-cast-button';
    castButton.innerHTML = '<google-cast-launcher id="castbutton"></google-cast-launcher>';
    containerEl.appendChild(castButton);
}
