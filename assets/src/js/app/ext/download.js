import {makeDebug} from '../../lib/debug';
import {receiveTorrent} from '../../lib/extDownload';
const debug = await makeDebug('webtor:ext');

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

// The page's own message (templates/views/ext/download.html): the extension
// did not hand the file over, update it or upload the .torrent here.
function showFallback() {
    const show = () => {
        const el = document.getElementById('ext-fallback');
        if (el) el.hidden = false;
    };
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', show, {once: true});
    else show();
}

try {
    debug('request downloadId=%d', window._downloadID);
    send(new Blob([await receiveTorrent(window, window._downloadID)]));
} catch (e) {
    debug('no torrent from the extension: %s', e.message);
    showFallback();
}
