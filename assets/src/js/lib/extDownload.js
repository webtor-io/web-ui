import {torrentBytes} from './extTorrentBytes';

// The Chrome extension hands /ext/download the .torrent it intercepted over
// window messages: it announces itself ({webtorInjected}), the page asks for
// the download ({downloadId}), the extension answers ({torrent}).
//
// Either wait used to be endless: an extension that is missing, outdated, or
// does not recognise the page (a build that knows only /ext/download, not
// /ru/ext/download) left a blank page for good, and so did an answer without
// bytes. Each step now has a deadline, and an answer without bytes fails at
// once; the page then says what happened (ext/download.js).

// A working extension answers within a second; ten leave room for a slow
// machine without making a broken one look like it is still thinking.
export const EXT_TIMEOUT_MS = 10000;

export class ExtDownloadError extends Error {}

// waitForMessage resolves with pick(data) for the first message the page
// posted to itself (event.source === win) for which pick returns something
// other than undefined; an Error from pick rejects. The listener goes away
// either way.
function waitForMessage(win, pick, ms, what) {
    return new Promise((resolve, reject) => {
        const done = () => {
            clearTimeout(timer);
            win.removeEventListener('message', onMessage);
        };
        const timer = setTimeout(() => {
            done();
            reject(new ExtDownloadError(what));
        }, ms);
        function onMessage(event) {
            if (event.source !== win) return;
            const r = pick(event.data);
            if (r === undefined) return;
            done();
            if (r instanceof Error) reject(r);
            else resolve(r);
        }
        win.addEventListener('message', onMessage);
    });
}

// receiveTorrent returns the .torrent bytes the extension sends for
// downloadId, or rejects with an ExtDownloadError.
export async function receiveTorrent(win, downloadId, ms = EXT_TIMEOUT_MS) {
    if (!win.__webtorInjected) {
        await waitForMessage(win, (d) => (d && d.webtorInjected ? true : undefined),
            ms, 'the extension did not announce itself');
    }
    const answer = waitForMessage(win, (d) => {
        if (!d || !d.torrent) return undefined;
        const bytes = torrentBytes(d.torrent);
        if (!bytes || bytes.length === 0) {
            return new ExtDownloadError(`the extension sent a torrent without bytes, ver=${d.ver}`);
        }
        return bytes;
    }, ms, 'the extension sent no torrent');
    win.postMessage({downloadId}, '*');
    return answer;
}
