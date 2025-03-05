import av from '../lib/av';
import message from './embed/message';


av(async function() {
    if (window._sessionExpired) return;
    if (window._ads === undefined) {
        window._ads = [];
    }
    for (const ad of window._ads) {
        renderAd(this, ad);
    }
});

function renderAd(el, ad) {
    ad = Object.assign({}, {
        trackPrefix: 'ad-',
    }, ad);
    if (ad.script) {
        renderScriptAd(ad);
    } else if (ad.injectScript) {
        renderInjectAd(ad);
    } else {
        renderMediaAd(el, ad);
    }
}

function  renderScriptAd(ad) {
    const script = document.createElement('script');
    script.type = 'text/javascript';
    script.innerHTML = ad.script;
    document.body.appendChild(script);
    window.umami.track(`${ad.trackPrefix}${ad.name}-script`)
}

function  renderInjectAd(ad) {
    message.send('inject', ad.injectScript);
    window.umami.track(`${ad.trackPrefix}${ad.name}-inject`)
}

function generateVideoEl(ad = {}) {
    const el = document.createElement('video');
    el.src = ad.src;
    el.autoplay = true;
    el.playsInline = true;
    return el;
}

function generateImageEl(ad) {
    ad = Object.assign({}, {
        duration: 10,
    }, ad);
    const el = document.createElement('img');
    el.src = ad.src;
    el.addEventListener('load', function () {
        const ev = new CustomEvent('play');
        el.dispatchEvent(ev);
        setTimeout(function () {
            const ev = new CustomEvent('ended');
            el.dispatchEvent(ev);
        }, ad.duration * 1000);
    })
    return el;
}

function generateMediaEl(ad = {}) {
    if (ad.src.endsWith('.mp4')) {
        return generateVideoEl(ad);
    } else if (ad.src.endsWith('.jpg')) {
        return generateImageEl(ad);
    }
}

function renderMediaAd(el, ad = {}) {
    ad = Object.assign({}, {
        skipDelay: 5,
    }, ad);
    const event = new CustomEvent('ads_play');
    window.dispatchEvent(event);
    const mediaEl = generateMediaEl(ad);
    const aEl = document.createElement('a');
    aEl.classList.add('absolute', 'top-0', 'left-0', 'z-50');
    aEl.href = ad.url;
    aEl.target = '_blank';
    aEl.setAttribute('data-umami-event', `${ad.trackPrefix}${ad.name}-click`);
    aEl.appendChild(mediaEl);
    el.appendChild(aEl);
    const skipDelay = ad.skipDelay;
    const closeEl = document.createElement('button');
    closeEl.classList.add('absolute', 'top-2', 'right-2', 'btn', 'btn-accent', 'btn-sm', 'z-50');
    closeEl.textContent = 'Close (' + skipDelay + ')';
    closeEl.setAttribute('data-umami-event', `${ad.trackPrefix}${ad.name}-close`);
    closeEl.disabled = true;
    mediaEl.addEventListener('ended', function() {
        aEl.remove();
        closeEl.remove();
        const event = new CustomEvent('ads_close');
        window.dispatchEvent(event);
    });
    mediaEl.addEventListener('play', function() {
        if (window.umami) {
            window.umami.track(`${ad.trackPrefix}${ad.name}-play`)
        }
        el.appendChild(closeEl);
        let cnt = 0;
        const ip = setInterval(function () {
            cnt++;
            if (cnt >= skipDelay) {
                closeEl.disabled = false;
                clearInterval(ip);
                closeEl.textContent = 'Close (X)';
                closeEl.addEventListener('click', function () {
                    aEl.remove();
                    closeEl.remove();
                    const event = new CustomEvent('ads_close');
                    window.dispatchEvent(event);
                });
                return;
            }
            closeEl.textContent = 'Close (' + (skipDelay - cnt) + ')';
        }, 1000);
    });
}
