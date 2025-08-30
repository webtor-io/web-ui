import initAsyncView from '../../lib/asyncView';

if (window._umami) {
    (await import('../../lib/umami')).init(window, window._umami);
}

function setHeight() {
    const width = document.body.offsetWidth;
    const height = width/16*9;
    document.body.style.height = height + 'px';
}

function startPlayer(progress, el) {
    window.removeEventListener('resize', setHeight);
    document.body.style.height = 'auto';
    progress.classList.add('hidden');
    el.classList.remove('hidden');
    const event = new CustomEvent('player_play');
    window.dispatchEvent(event);
}

window.addEventListener('load', async () => {
    const progress = document.querySelector('.progress-alert');
    const player = document.createElement('div');
    const initProgressLog = (await import('../../lib/progressLog')).initProgressLog;
    let playingAds = false;
    let playerReady = false;
    let adsPlayed = false;
    initProgressLog(progress, function(ev) {
        if (ev.level !== 'rendertemplate') return;
        if (ev.tag === 'rendering action') {
            window.addEventListener('player_ready', function() {
                playerReady = true;
                if (playingAds) {
                    const event = new CustomEvent('player_paused');
                    window.dispatchEvent(event);
                } else {
                    startPlayer(progress, player);
                }
            }, {once: true});
            player.classList.add('hidden');
            document.body.appendChild(player);
            ev.render(player);
        }
        if (ev.tag === 'rendering forbid') {
            const forbid = document.createElement('div');
            document.body.appendChild(forbid);
            ev.render(forbid);
        }
        if (ev.tag === 'rendering ads' && !adsPlayed) {
            window.addEventListener('ads_play', function() {
                playingAds = true;
            }, {once: true});
            window.addEventListener('ads_close', function() {
                playingAds = false;
                adsPlayed = true;
                if (playerReady) startPlayer(progress, player);
            }, {once: true});
            const ads = document.createElement('div');
            document.body.appendChild(ads);
            ev.render(ads);
        }
    });
    if (!window._embedSettings.height) {
        (await import('@open-iframe-resizer/core'));
    }
    setHeight();
    window.addEventListener('resize', setHeight);
    if (window._embedSettings.poster) {
        document.body.style.backgroundImage = 'url(' + window._embedSettings.poster + ')';
        document.body.style.backgroundSize = 'cover';
    }
    initAsyncView();
});