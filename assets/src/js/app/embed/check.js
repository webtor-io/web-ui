const message = (await import('../../lib/message')).default;
import {makeDebug} from '../../lib/debug';
const debug = await makeDebug('webtor:embed:check');
const sha1 = require('sha1');
message.send('init');
const data = await message.receiveOnce('init');
if (window._umami) {
    (await import('../../lib/umami')).init(window, Object.assign(window._umami, {
        referrer: data.referer,
    }));
}
const c = await check();
if (c) {
    await initPlaceholder(data);
    window.addEventListener('click', async () => {
        initEmbed(data);
    }, { once: true });
    message.send('inited');
} else {
   document.body.remove();
   console.log('webtor check not passed, use original embed script');
}

async function initPlaceholder(data) {
    if (!data.height) {
        function setHeight() {
            const width = document.body.offsetWidth;
            const height = width / 16 * 9;
            document.body.style.height = height + 'px';
        }
        window.addEventListener('resize', setHeight);
        (await import('@open-iframe-resizer/core'));
        setHeight();
    } else {
        document.body.style.height = data.height;
    }
    if (data.poster) {
        document.body.style.backgroundImage = 'url(' + data.poster + ')';
        document.body.style.backgroundSize = 'cover';
    }
}

async function check() {
    message.send('inject', window._checkScript);
    const check = await message.receiveOnce('check');
    const hash = sha1(window._id + check)
    debug('check window._id=%o check=%o hash=%o _checkHash=%o', window._id, check, hash, _checkHash);
    return hash  === _checkHash;
}

function initEmbed(data) {
    message.send('play_clicked');
    const form = document.createElement('form');
    form.setAttribute('method', 'post');
    form.setAttribute('enctype', 'multipart/form-data');
    const csrf = document.createElement('input');
    csrf.setAttribute('name', '_csrf');
    csrf.setAttribute('value', window._CSRF);
    csrf.setAttribute('type', 'hidden');
    form.append(csrf);
    const sessionID = document.createElement('input');
    sessionID.setAttribute('name', '_sessionID');
    sessionID.setAttribute('value', window._sessionID);
    sessionID.setAttribute('type', 'hidden');
    form.append(sessionID);
    const i = document.createElement('input');
    i.setAttribute('name', 'settings');
    i.setAttribute('value', JSON.stringify(data));
    i.setAttribute('type', 'hidden');
    form.append(i);
    document.body.append(form);
    // form.setAttribute('action', '/');
    form.submit();
}