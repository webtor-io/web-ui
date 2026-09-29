const message = (await import('../../lib/message')).default;
import {makeDebug} from '../../lib/debug';
const debug = await makeDebug('webtor:embed:check');
// NOTE: SHA1 is used here for embed integrity verification (not cryptographic security).
// Both client and server must agree on the algorithm, so migrating to SHA-256
// would require a coordinated change and would break all existing embeds.
import sha1 from 'sha1';
import { applyUrlSwitch, applyAudioUrlSwitch, applyMmsUrlSwitch, takesPart, startProbe, whenDeclared, declarationFor } from '../../lib/player/decode-declaration';
// The HEVC passthrough declaration (lib/player/decode-declaration.js). An
// embed takes part the way any page does -- its own storage says so -- and
// only then probes. Wrapped: this module has top-level awaits, and a throw
// here would take the whole embed down.
try {
    applyUrlSwitch(window);
    applyAudioUrlSwitch(window);
    applyMmsUrlSwitch(window);
    if (takesPart(window)) startProbe(window);
} catch (e) { /* no declaration */ }
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
        await initEmbed(data);
    }, { once: true });
    message.send('inited');
} else {
   document.body.remove();
   console.warn('webtor check not passed, use original embed script');
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
        document.documentElement.style.height = '100%';
        document.body.style.height = data.height;
        document.body.setAttribute('data-fixed-height', '');
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

// embedDeclaration is the `decode` field of the embed's start, or null. The
// file is not known here (the job finds it), so there is no per-file memory
// to consult; the per-class memory still applies.
async function embedDeclaration() {
    try {
        if (!takesPart(window)) return null;
        await whenDeclared(window, 300);
        return declarationFor(window, {});
    } catch (e) {
        return null;
    }
}

async function initEmbed(data) {
    message.send('play_clicked');
    const decode = await embedDeclaration();
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
    if (decode) {
        const d = document.createElement('input');
        d.setAttribute('name', 'decode');
        d.setAttribute('value', decode);
        d.setAttribute('type', 'hidden');
        form.append(d);
    }
    document.body.append(form);
    form.submit();
}