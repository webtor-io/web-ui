import {makeDebug} from '../../lib/debug';
import semver from 'semver'
const debug = await makeDebug('webtor:ext');

function init() {
    return new Promise((resolve) => {
        if (window.__webtorInjected) return resolve();
        debug('wait for initialization');
        window.addEventListener('message', (event) => {
            if (event.source !== window)
                return;

            if (event.data.webtorInjected) return resolve();
        });
    });
}
function fetch(downloadId) {
    debug('request downloadId=%d', downloadId);
    return new Promise((resolve) => {
        window.addEventListener('message', (event) => {
            if (event.source !== window) {
                return;
            }
            if (event.data.torrent) {
                if (event.data.ver && semver.gte('0.1.12', event.data.ver)) {
                    resolve(new Blob([new Uint8Array(event.data.torrent)]));
                    return;
                }
                resolve(new Blob([new Uint8Array(event.data.torrent.data)]));
            }
        });
        window.postMessage({downloadId}, '*');
    });
}
function send(data) {
    const form = document.createElement('form');
    form.setAttribute('method', 'post');
    form.setAttribute('enctype', 'multipart/form-data');
    form.style.display = 'none';
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
    const res = document.createElement('input');
    res.setAttribute('name', 'resource');
    res.setAttribute('type', 'file');
    let file = new File([data], 'resource.torrent');
    let container = new DataTransfer();
    container.items.add(file);
    res.files = container.files;
    form.append(res);
    document.body.append(form);
    form.setAttribute('action', '/');
    form.submit();
}
await init();
const data = await fetch(window._downloadID)
send(data);
