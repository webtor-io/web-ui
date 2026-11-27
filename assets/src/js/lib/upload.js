import { getLang, langPath, makeI18n } from './i18n';
import { humanBytes } from './humanBytes';
import { clearBusy, holdVaultButton, markBusy } from './drop';

// The uploader: the user's own files go from the browser straight to the
// seeder through the proxy, in parts, with a write token from web-ui
// (POST /upload/token). Loaded by lib/drop.js only when someone drops or
// picks files. Nothing lives at module level: this module can be bundled
// twice, so a run keeps its state in its own closure, on window and in the
// DOM.

const PART = 16777216;
const FILES_AT_ONCE = 2;
// A file whose path the owner deleted gets the next free name, up to this
// many times: the seeder also refuses a path under a deleted file, whatever
// its last segment.
const RENAMES = 5;
// The proxy's limiter says Retry-After: 5, but sends no
// Access-Control-Expose-Headers, so a cross-origin XHR cannot read it.
const retry429 = 5000;
const NODE_FULL_MS = 60000;
// How long a finished upload waits for its two events before it leaves.
const EVENTS_WAIT_MS = 1000;
// The failed files the verdict names; the rest is an ellipsis.
const FAILED_NAMED = 10;
// The busy mark is written at most this often: progress events come many
// times a second, and every write reaches every tab of the site.
const BUSY_WRITE_MS = 5000;
// A toast is all a user who left the page sees of the outcome: it stays
// long enough to be read.
const OUTCOME_TOAST_MS = 10000;
const NETWORK = new Set([0, 502, 503, 504]);
const backoff = (n) => Math.min(1000 * 2 ** n, 30000);
// result of upload-end when several causes met: the first one listed.
const RESULTS = ['auth', 'quota', 'size', 'gone', 'errors'];

// fill puts params into a {{.Name}} message.
export function fill(msg, params) {
    return msg.replace(/\{\{\s*\.(\w+)\s*\}\}/g, (m, k) => (k in params ? String(params[k]) : m));
}

// safeSegment is a path segment as the seeder keeps it: NFC, no '~' (the
// proxy's chain separator), at most 255 UTF-8 bytes with the extension kept.
export function safeSegment(name) {
    const s = name.normalize('NFC').replaceAll('~', '～');
    const enc = new TextEncoder();
    if (enc.encode(s).length <= 255) return s;
    const dot = s.lastIndexOf('.');
    const ext = dot > 0 ? s.slice(dot) : '';
    const room = 255 - enc.encode(ext).length;
    let stem = '';
    let size = 0;
    for (const ch of dot > 0 ? s.slice(0, dot) : s) {
        size += enc.encode(ch).length;
        if (size > room) break;
        stem += ch;
    }
    return stem + ext;
}

const firstSegment = (rel) => (rel || '').split('/')[0];

// pickedFolder is the folder "Upload a folder" picked: its files come
// without entries, each with a relative path under it. '' otherwise.
function pickedFolder(items) {
    const root = firstSegment(items[0]?.file?.webkitRelativePath);
    return root && items.every((i) => !i.entry && firstSegment(i.file?.webkitRelativePath) === root) ? root : '';
}

const pad = (n) => String(n).padStart(2, '0');

// rootName names a new upload: the folder dropped or picked, the one file
// without its extension, or the time of the drop.
export function rootName(items, now = new Date()) {
    const folder = pickedFolder(items);
    if (folder) return folder;
    if (items.length === 1) {
        const { entry, file } = items[0];
        const name = entry?.name ?? file.name;
        const dot = name.lastIndexOf('.');
        return entry?.isDirectory || dot <= 0 ? name : name.slice(0, dot);
    }
    return `Upload ${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}:${pad(now.getMinutes())}`;
}

// nextName is the path with " (n)" added to, or counted up in, its last
// segment's name.
export function nextName(path) {
    const slash = path.lastIndexOf('/') + 1;
    const name = path.slice(slash);
    const dot = name.lastIndexOf('.');
    let stem = dot > 0 ? name.slice(0, dot) : name;
    const ext = dot > 0 ? name.slice(dot) : '';
    const m = stem.match(/^(.*) \((\d+)\)$/);
    let n = 2;
    if (m) {
        stem = m[1];
        n = Number(m[2]) + 1;
    }
    return `${path.slice(0, slash)}${stem} (${n})${ext}`;
}

// sizeBucket is the size of a drop as an event property.
export function sizeBucket(bytes) {
    if (bytes < 104857600) return '<100M';
    if (bytes < 1073741824) return '<1G';
    if (bytes < 10737418240) return '<10G';
    return '10G+';
}

// listFiles lists the files of a drop or a pick, with their paths relative
// to it. A file or folder the browser cannot read is {rel, error: true}.
export async function listFiles(items) {
    const out = [];
    for (const { entry, file } of items) {
        if (entry) await walk(entry, out);
        else out.push({ rel: file.webkitRelativePath || file.name, file });
    }
    return out;
}

// walk lists entry's files into out. An error callback ends the entry as an
// error rather than leaving its promise pending.
async function walk(entry, out) {
    const rel = entry.fullPath.replace(/^\//, '');
    if (!entry.isDirectory) {
        const file = await new Promise((ok) => entry.file(ok, () => ok(null)));
        out.push(file ? { rel, file } : { rel, error: true });
        return;
    }
    const reader = entry.createReader();
    for (;;) {
        const batch = await new Promise((ok) => reader.readEntries(ok, () => ok(null)));
        if (!batch) {
            out.push({ rel, error: true });
            return;
        }
        if (!batch.length) return;
        for (const e of batch) await walk(e, out);
    }
}

function locale() {
    return import(
        /* webpackChunkName: "locale-upload-[request]" */
        `../../../../locales/${getLang()}.json?prefix=upload`
    ).then((m) => makeI18n(m.default || m));
}

// track sends an Umami event, when there is a tracker, and answers its
// promise. A tracker that throws or fails never breaks an upload.
function track(name, data) {
    if (!window.umami) return null;
    try {
        return Promise.resolve(window.umami.track(name, data)).catch(() => {});
    } catch {
        return null;
    }
}

// transport is the network: a failed request is {status: 0}, not an
// exception.
function transport() {
    return {
        async token(body) {
            try {
                const res = await fetch(langPath('/upload/token'), {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'X-CSRF-TOKEN': window._CSRF,
                        'X-SESSION-ID': window._sessionID,
                    },
                    body: JSON.stringify(body),
                });
                return { status: res.status, json: await res.json().catch(() => ({})) };
            } catch {
                return { status: 0 };
            }
        },
        put(url, blob, onProgress) {
            return new Promise((resolve) => {
                const xhr = new XMLHttpRequest();
                xhr.open('PUT', url);
                xhr.upload.onprogress = (e) => onProgress(e.loaded);
                xhr.onload = () => resolve({ status: xhr.status, text: xhr.responseText });
                xhr.onerror = xhr.ontimeout = xhr.onabort = () => resolve({ status: 0, text: '' });
                xhr.send(blob);
            });
        },
        wait: (ms) => new Promise((r) => setTimeout(r, ms)),
    };
}

// startUpload sends items into a new upload, or into opts.id ("Add files",
// into the folder opts.dir), showing its progress in log, the page's
// [data-upload-log] (partials/upload/log.html): the log of a torrent's load,
// fed from here. opts: id, name (the upload's root), dir, upsell (say what a
// subscription gives), vault (Vault is set up), transport (tests).
export async function startUpload(log, items, opts = {}) {
    // A second action while the uploader chunk was loading: one run a tab.
    if (window.__uploadBusy) return;
    const doc = log.ownerDocument;
    holdVaultButton(doc);
    const tr = opts.transport || transport();
    const startedAt = Date.now();
    const add = opts.id ? 1 : 0;
    let id = opts.id;
    // The mark other tabs of this browser see while this one writes into id;
    // network events keep it fresh, a timer would not run in a hidden tab.
    let wrote = 0;
    const touch = () => {
        if (id && Date.now() - wrote >= BUSY_WRITE_MS) {
            wrote = Date.now();
            markBusy(id);
        }
    };
    const onLeave = (e) => {
        e.preventDefault();
        e.returnValue = '';
    };
    let t;
    let files = [];
    let total = 0;
    let said;
    let startSent = null;
    let pl;
    let run;
    const causes = new Set();
    window.__uploadBusy = true;
    touch();
    window.addEventListener('beforeunload', onLeave);
    const wake = globalThis.navigator?.wakeLock?.request('screen').catch(() => null);
    try {
        files = await listFiles(items);
        if (!files.length) return;
        ({ t } = await locale());
        const { initProgressLog } = await import('./progressLog');
        const queue = files.filter((f) => !f.error);
        total = queue.reduce((n, f) => n + f.file.size, 0);

        // The log, cleared of an earlier run: one line for the run, the
        // note that it is temporary under it, then the verdicts. The links
        // of its footer are this run's own.
        pl = initProgressLog(log);
        pl.clear();
        const footer = log.querySelector('.alert-close-wrapper');
        const open = log.querySelector('[data-upload-open]');
        const signIn = log.querySelector('[data-upload-sign-in]');
        const gotIt = log.querySelector('.closeable-close');
        open.hidden = signIn.hidden = true;
        gotIt.hidden = false;
        run = pl.inProgress('upload', t('upload.uploading'));
        pl.info(t('upload.state.temporary') + (opts.vault ? ' ' + t('upload.state.saveHint') : ''));
        // On the home page the log is above the search field, out of sight
        // of a drop on a phone.
        log.scrollIntoView?.({ block: 'nearest' });

        // The line: files stored of files dropped, and the bytes sent as a
        // percent, which fills the bar. A pause has the line to itself until
        // it is over, whatever another file sends meanwhile.
        let stored = 0;
        let paused = false;
        const paint = () => {
            if (paused) return;
            let sent = 0;
            for (const f of queue) sent += f.sent || 0;
            const pct = total ? Math.floor((100 * sent) / total) : 0;
            run.updateStatus((files.length > 1 ? `${stored} / ${files.length} · ` : '') + pct + '%');
        };
        // A file that ended in an error is named at the end, under the name
        // it was dropped with.
        const fail = (f) => {
            causes.add('errors');
            f.failed = true;
        };
        for (const f of files) if (f.error) fail(f);
        paint();

        // A stop's text is the run's verdict, the first one; it is also
        // remembered for the toast when the log has left the document. A
        // pause is not an outcome.
        const stop = (text, more) => {
            said ??= text;
            run.error(more ? text + '\n' + more : text);
        };
        const pause = (key) => {
            paused = true;
            run.updateStatus(t(key));
        };
        const resume = () => {
            if (!paused) return;
            paused = false;
            paint();
        };
        const upsell = () => (opts.upsell ? t('upload.upsell') : '');

        // stopped: nothing more goes to this upload, no PUT, no token.
        // closed: no new file starts; the files in flight finish.
        let stopped = false;
        let closed = false;
        const gone = (text) => {
            causes.add('gone');
            stopped = true;
            stop(text);
        };
        const sizeOut = (cap) => {
            causes.add('size');
            closed = true;
            stop(fill(t('upload.quotaOut'), { Cap: humanBytes(cap) }), upsell());
        };
        // refused handles a token request's answer other than 200; reRequest:
        // the request after a PUT's 413 size, whose 413 quota means the
        // same.
        const refused = (res, reRequest) => {
            const j = res.json || {};
            if (res.status === 413 && j.code === 'quota') {
                if (reRequest) return sizeOut(j.cap);
                causes.add('quota');
                stopped = true;
                return stop(fill(t('upload.quotaFull'), { Used: humanBytes(j.used), Cap: humanBytes(j.cap) }), upsell());
            }
            if (res.status === 401) {
                causes.add('auth');
                stopped = true;
                signIn.href = langPath('/login') + '?return-url=' + encodeURIComponent(window.location.pathname);
                signIn.hidden = false;
                return stop(t('upload.signInAgain'));
            }
            if (res.status === 409 && j.code === 'frozen') return gone(t('upload.state.frozen'));
            if (res.status === 404 || res.status === 410) return gone(t('upload.unavailable'));
            causes.add('errors');
            stopped = true;
            stop(t('upload.unavailable'));
        };

        // Tokens. ask retries a request the network failed; it answers null
        // when the upload stopped meanwhile.
        let tok = null;
        let tokenReq = null;
        const ask = async (body) => {
            for (let n = 0; ; n++) {
                const res = await tr.token(body);
                if (res.status === 200 && !id) id = res.json.id;
                touch();
                if (!NETWORK.has(res.status)) {
                    resume();
                    return res;
                }
                pause('upload.offline');
                await tr.wait(backoff(n));
                if (stopped) return null;
            }
        };
        // replace swaps the token a PUT was refused on, once for every file
        // that was: a file that comes after it was swapped retries with the
        // new one. A token got for a 413 size is marked: a 413 size on it is
        // final. want is a number or a function counted at request time.
        const replace = async (from, want, sized) => {
            while (tokenReq) await tokenReq;
            if (stopped || tok !== from || (sized && closed)) return;
            tokenReq = (async () => {
                try {
                    const res = await ask({ id, want: typeof want === 'function' ? want() : want });
                    if (!res) return;
                    if (res.status === 200) tok = { ...res.json, sized };
                    else refused(res, sized);
                } finally {
                    tokenReq = null;
                }
            })();
            await tokenReq;
        };
        // A file is started once the seeder took a part of it: it already
        // counts in what the upload holds.
        const unstarted = () => queue.reduce((n, f) => (f.started || f.over ? n : n + f.file.size), 0);

        // Paths, safe segment by segment. A new upload's one folder is its
        // root. "Add files" go under the open folder, or under the root;
        // the root folder itself dropped into the root resumes it.
        const oneFolder = !!pickedFolder(items) || (items.length === 1 && !!items[0].entry?.isDirectory);
        const resumes = opts.dir?.length === 1 && items.length === 1 && !!items[0].entry?.isDirectory
            && safeSegment(items[0].entry.name) === safeSegment(opts.dir[0]);
        let prefix;
        if ((!opts.id && oneFolder) || resumes) prefix = '';
        else if (opts.dir) prefix = opts.dir.join('/');
        else prefix = opts.id ? opts.name || rootName(items) : rootName(items);
        for (const f of queue) f.path = (prefix ? prefix + '/' + f.rel : f.rel).split('/').map(safeSegment).join('/');

        let startTracked = false;
        const accepted = () => {
            if (startTracked) return;
            startTracked = true;
            startSent = track('upload-start', { files: queue.length, sizeBucket: sizeBucket(total), add });
        };
        const done = (f) => {
            f.done = true;
            f.sent = f.file.size;
            stored++;
            // The files stored so far open in a new tab while the rest go
            // on; "got it" waits for the end.
            if (stored === 1) {
                open.href = langPath('/' + id);
                open.hidden = false;
                gotIt.hidden = true;
                footer.classList.remove('hidden');
            }
            paint();
        };

        // put sends one part, waiting out the pauses; it answers the
        // seeder's answer and the token it went with, or no answer when the
        // upload stopped during a pause.
        const put = async (f, offset, end) => {
            for (let n = 0; ;) {
                const tk = tok;
                const q = new URLSearchParams({ token: tk.token, 'api-key': tk.apiKey, offset, size: f.file.size });
                const url = `${tk.url}/${f.path.split('/').map(encodeURIComponent).join('/')}?${q}`;
                const res = await tr.put(url, f.file.slice(offset, end), (loaded) => {
                    touch();
                    f.sent = offset + (Number(loaded) || 0);
                    paint();
                });
                touch();
                if (NETWORK.has(res.status)) {
                    pause('upload.offline');
                    await tr.wait(backoff(n++));
                } else if (res.status === 507) {
                    pause('upload.nodeFull');
                    await tr.wait(NODE_FULL_MS);
                } else if (res.status === 429) {
                    await tr.wait(retry429);
                } else {
                    resume();
                    return [res, tk];
                }
                if (stopped) return [null, tk];
            }
        };

        const send = async (f) => {
            let offset = 0;
            let renames = 0;
            let renewed = false;
            try {
                while (!stopped) {
                    const end = Math.min(offset + PART, f.file.size);
                    const [res, tk] = await put(f, offset, end);
                    if (!res) return;
                    const text = String(res.text ?? '').trim();
                    const s = res.status;
                    if (s !== 403) renewed = false;
                    if (s === 204 || s === 201) {
                        accepted();
                        f.started = true;
                        if (s === 201) return done(f);
                        offset = f.sent = end;
                        paint();
                    } else if (s === 409 && /^\d+$/.test(text)) {
                        f.started = true;
                        offset = f.sent = Number(text);
                    } else if (s === 409 && (text === 'exists' || (text === 'deleted' && renames++ < RENAMES))) {
                        f.path = nextName(f.path);
                        offset = 0;
                    } else if (s === 409 && text === 'frozen') {
                        return gone(t('upload.state.frozen'));
                    } else if (s === 410 || (s === 409 && text === 'root')) {
                        return gone(t('upload.unavailable'));
                    } else if (s === 403) {
                        // An expired token is renewed, without asking for
                        // more; a second 403 on a fresh one is final.
                        if (renewed) return gone(t('upload.unavailable'));
                        renewed = true;
                        await replace(tk, 0, false);
                    } else if (s === 413 && text === 'size') {
                        // The reserve ran short: ask once for what the files
                        // not started yet need, and start this one over.
                        if (tk.sized) return sizeOut(tk.cap);
                        await replace(tk, unstarted, true);
                        if (closed) return;
                        offset = 0;
                    } else if (s === 413 && text === 'files') {
                        causes.add('errors');
                        closed = true;
                        return stop(t('upload.tooManyFiles'));
                    } else {
                        // 400 and the rest, the last 409 deleted included:
                        // this file fails, the others go on.
                        return fail(f);
                    }
                }
            } finally {
                f.over = true;
            }
        };

        if (queue.length) {
            const first = await ask(opts.id ? { id: opts.id, want: Math.max(1, total) } : { name: rootName(items), want: Math.max(1, total) });
            if (first.status === 200) tok = { ...first.json, sized: false };
            else refused(first, false);
            let next = 0;
            const worker = async () => {
                for (;;) {
                    // New files wait for a token being replaced.
                    while (tokenReq) await tokenReq;
                    if (stopped || closed || next >= queue.length) return;
                    await send(queue[next++]);
                }
            };
            await Promise.all(Array.from({ length: FILES_AT_ONCE }, worker));
        }
    } finally {
        window.removeEventListener('beforeunload', onLeave);
        window.__uploadBusy = false;
        if (id) clearBusy(id);
        wake?.then((l) => l?.release()).catch(() => {});
    }

    const allDone = files.every((f) => f.done);
    const result = allDone ? 'done' : RESULTS.find((c) => causes.has(c)) ?? 'errors';
    const endSent = track('upload-end', { result, sizeBucket: sizeBucket(total), minutes: Math.round((Date.now() - startedAt) / 60000), add });
    if (allDone) {
        run.done(t('upload.done'));
    } else {
        const failed = files.filter((f) => f.failed).map((f) => f.rel);
        if (failed.length) {
            const named = failed.slice(0, FAILED_NAMED);
            if (failed.length > FAILED_NAMED) named.push('…');
            pl.warn('failed', [t('upload.someFailed'), ...named].join('\n'));
        }
    }
    // Leaving would cut the events' requests: wait for them, a little.
    if (allDone && log.isConnected && window.umami) {
        await Promise.race([Promise.allSettled([startSent, endSent]), tr.wait(EVENTS_WAIT_MS)]);
    }
    // The log is still there: show the files, the way a torrent's load does,
    // or stay on the verdicts.
    if (log.isConnected) {
        if (allDone) {
            pl.push({ level: 'redirect', location: langPath('/' + id) + (opts.dir?.length > 1 ? '?pwd=' + encodeURIComponent('/' + opts.dir.join('/')) : '') });
        } else {
            log.querySelector('.closeable-close').hidden = false;
            run.close();
        }
        return;
    }
    // An async navigation took the log away: the outcome is a toast, once
    // the tab is seen (a hidden tab would fade it in and out unseen).
    const show = () => {
        if (said === undefined && allDone) window.toast?.success(t('upload.done'), OUTCOME_TOAST_MS);
        else window.toast?.error(said ?? t('upload.someFailed'), OUTCOME_TOAST_MS);
    };
    if (doc.visibilityState === 'hidden') doc.addEventListener('visibilitychange', show, { once: true });
    else show();
}
