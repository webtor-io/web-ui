// The uploader: paths, tokens and parts, every answer of the token request
// and of the seeder's PUT, the progress log, the busy flag and mark, the
// outcome toast and the two Umami events. The transport is a fake that
// answers by script; its wait resolves at once, so a loop that should have
// ended makes the fake throw instead of hanging the run.
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';

const U0 = '9c5db5752b7c0123456789abcdef0123456789ab';
const BASE = `https://up.example.invalid/${U0}`;
const CAP = 10737418240;
const PART = 16777216;
const BUSY = 'Wait until the upload finishes';

// lib/i18n.js reads this webpack constant on import.
globalThis.__SUPPORTED_LOCALES__ = ['en'];

// newWindow is a page's window. jsdom has no innerText, which the log writes
// its verdicts with: here it is the text, as a browser shows it.
function newWindow(url = 'https://webtor.io/') {
    const w = new JSDOM('<!doctype html><body></body>', { url }).window;
    Object.defineProperty(w.HTMLElement.prototype, 'innerText', {
        configurable: true,
        get() { return this.textContent; },
        set(v) { this.textContent = v; },
    });
    return w;
}
// useWindow puts w in place of the page's window; lib/async.js, which the
// log's redirect goes through, reads its event classes as globals.
function useWindow(w) {
    global.window = w;
    global.document = w.document;
    global.CustomEvent = w.CustomEvent;
    global.HTMLElement = w.HTMLElement;
}
useWindow(newWindow());

const { fill, safeSegment, rootName, defaultName, nextName, sizeBucket, startUpload } = await import('./upload.js');
const { makeI18n } = await import('./i18n.js');
const { default: loadAsyncView } = await import('./loadAsyncView.js');
// The uploader's chunks, loaded once here: no test's first run waits for the
// loader.
await import('../../../../locales/en.json?prefix=upload');
await import('./progressLog.js');

// The log as partials/upload/log.html draws it, the job log inside, the
// template actions resolved: a text is its key, as t() answers here.
const partial = (p) => readFileSync(new URL('../../../../templates/partials/' + p, import.meta.url), 'utf8');
const jobLog = partial('job_log.html').replace(/\{\{[\s\S]*?\}\}/g, (a) => (a.includes('showSteps') ? 'action.showSteps' : ''));
const uploadLog = partial('upload/log.html').replace(/\{\{[\s\S]*?\}\}/g, (a) => {
    if (a.includes('"job/log"')) return jobLog;
    const key = /^\{\{-?\s*t\s+\S+\s+"([^"]+)"/.exec(a);
    return key ? key[1] : '';
});

const tick = () => new Promise((r) => setTimeout(r, 0));
// until takes ticks until cond holds, and fails if it does not within two
// seconds: a run the test does not await takes several macrotasks, more on
// a loaded machine.
async function until(cond, what) {
    for (const end = Date.now() + 2000; !cond();) {
        if (Date.now() > end) assert.fail(`timed out waiting for ${what}`);
        await tick();
    }
}
const never = () => new Promise(() => {});
function defer() {
    let resolve;
    const promise = new Promise((r) => { resolve = r; });
    return { promise, resolve };
}

function ownWindow(t, url) {
    const prev = global.window;
    const w = newWindow(url);
    useWindow(w);
    t.after(() => useWindow(prev));
    return w;
}

// Files, entries and drop items.
const file = (name, size = 10, rel = '') => ({ name, size, webkitRelativePath: rel, slice: (a, b) => ({ a, b }) });
const asFile = (name, size = 10) => ({ entry: null, file: file(name, size) });
const picked = (rel, size = 10) => ({ entry: null, file: file(rel.split('/').pop(), size, rel) });
function fileEntry(fullPath, size = 10, { fail = false } = {}) {
    const name = fullPath.split('/').pop();
    const f = file(name, size);
    return { isFile: true, isDirectory: false, name, fullPath, file: (ok, err) => (fail ? err(new Error('unreadable')) : ok(f)) };
}
// folder is a directory entry at path with spec's files (rel: size) or
// subfolders (name: spec).
function folder(path, spec, { throws = false } = {}) {
    const kids = Object.entries(spec).map(([k, v]) => (typeof v === 'number' ? fileEntry(`/${path}/${k}`, v) : folder(`${path}/${k}`, v)));
    return {
        isFile: false, isDirectory: true, name: path.split('/').pop(), fullPath: '/' + path,
        createReader() {
            let done = false;
            return {
                readEntries(ok) {
                    if (throws) throw new Error('walk');
                    ok(done ? [] : kids);
                    done = true;
                },
            };
        },
    };
}
const item = (entry) => ({ entry, file: file(entry.name, 0) });

const ok201 = { status: 201, text: '' };
const grant = (n, extra = {}) => ({ status: 200, json: { id: U0, url: BASE, token: 't' + n, apiKey: 'k', cap: CAP, used: 0, ...extra } });

function parsePut(url) {
    const u = new URL(url);
    const prefix = `/${U0}/`;
    assert.ok(u.pathname.startsWith(prefix), url);
    return {
        url,
        path: u.pathname.slice(prefix.length).split('/').map(decodeURIComponent).join('/'),
        offset: Number(u.searchParams.get('offset')),
        size: Number(u.searchParams.get('size')),
        token: u.searchParams.get('token'),
        apiKey: u.searchParams.get('api-key'),
    };
}
const last = (p) => p.path.split('/').pop();

// rig is one run's world: the upload log in the document, a fake transport
// that answers by token(body, n) and put(p, onProgress, n), and the log's
// redirect written down. Everything goes into one log, in order.
function rig(t, { token, put, wait, onAssign, readable } = {}) {
    const log = [];
    const calls = { token: [], put: [], wait: [] };
    const tr = {
        async token(body) {
            calls.token.push(body);
            log.push(['token', body]);
            return (token || ((b, n) => grant(n)))(body, calls.token.length);
        },
        async put(url, blob, onProgress) {
            const p = parsePut(url);
            calls.put.push(p);
            log.push(['put', p.path]);
            return (put || (() => ok201))(p, onProgress, calls.put.length);
        },
        async wait(ms) {
            calls.wait.push(ms);
            log.push(['wait', ms]);
            if (wait) return wait(ms);
        },
        readable: readable && (async (blob) => readable(blob)),
    };
    const page = document.createElement('div');
    page.innerHTML = uploadLog;
    const host = page.querySelector('[data-upload-log]');
    // The redirect is a click on a link into "main", which lib/async.js
    // takes on a page.
    const assigned = [];
    host.addEventListener('click', (e) => {
        const a = e.target.closest('a[data-async-target]');
        if (!a) return;
        e.preventDefault();
        const u = new URL(a.href);
        assigned.push(u.pathname + u.search);
        log.push(['assign', u.pathname + u.search]);
        onAssign?.();
    });
    document.body.append(page);
    t.after(() => {
        page.remove();
        delete window.__uploadBusy;
    });
    // status: the verdicts and notes under the line; meta: the line's
    // numbers or pause.
    const status = () => host.querySelector('.job-results').textContent;
    const meta = () => host.querySelector('.job-stage-meta').textContent;
    const run = (items, opts = {}) => startUpload(host, items, { ...opts, transport: tr });
    return { tr, calls, log, assigned, host, status, meta, run };
}

// umami puts a fake tracker on the window for one test; answer(name) is
// what track returns.
function umami(t, log, answer = () => Promise.resolve()) {
    window.umami = { track: (name, data) => { log.push(['track:' + name, data]); return answer(name); } };
    t.after(() => { delete window.umami; });
}
const ends = (log) => log.filter(([k]) => k === 'track:upload-end').map(([, d]) => d.result);
const starts = (log) => log.filter(([k]) => k === 'track:upload-start');

// Scenarios more than one test runs. Each is fresh: its held answers are
// its own.
const three = () => [asFile('a', 10), asFile('b', 20), asFile('c', 30)];

// 410 on b; a's 201 comes only after it.
function gone410() {
    const a = defer();
    return {
        items: three(),
        put: (p) => {
            if (last(p) === 'a') return a.promise;
            if (last(p) === 'b') {
                setTimeout(() => a.resolve(ok201), 0);
                return { status: 410, text: '' };
            }
            return ok201;
        },
    };
}

// 409 frozen on b and on every later write; a's 201 after it.
function frozen409() {
    const a = defer();
    let frozen = 0;
    return {
        items: three(),
        put: (p) => {
            if (last(p) === 'a') return a.promise;
            if (++frozen === 2) throw new Error('frozen loop');
            setTimeout(() => a.resolve(ok201), 0);
            return { status: 409, text: 'frozen' };
        },
    };
}

// 409 root on b; a's 201 after it.
function root409() {
    const a = defer();
    return {
        items: three(),
        put: (p, _, n) => {
            if (n === 4) throw new Error('root loop');
            if (last(p) === 'a') return a.promise;
            if (last(p) === 'b') {
                setTimeout(() => a.resolve(ok201), 0);
                return { status: 409, text: 'root' };
            }
            return ok201;
        },
    };
}

// b gets 413 size on every token; a's 201 waits for b's second.
function sizeFinal({ onSecond } = {}) {
    const a = defer();
    let b = 0;
    return {
        items: three(),
        token: (body, n) => {
            if (n > 2) throw new Error('size loop');
            return grant(n);
        },
        put: (p) => {
            if (last(p) === 'a') return a.promise;
            if (last(p) === 'b') {
                if (++b === 2) {
                    onSecond?.();
                    setTimeout(() => a.resolve(ok201), 0);
                }
                return { status: 413, text: 'size' };
            }
            return ok201;
        },
    };
}

// b gets 413 size on t1, the re-request 413 quota; a's 201 after that.
function quotaOnReRequest() {
    const a = defer();
    return {
        items: three(),
        token: (body, n) => {
            if (n === 1) return grant(1);
            setTimeout(() => a.resolve(ok201), 0);
            return { status: 413, json: { code: 'quota', used: 10, cap: 10 } };
        },
        put: (p) => {
            if (last(p) === 'a') return a.promise;
            if (last(p) === 'b') return { status: 413, text: 'size' };
            return ok201;
        },
    };
}

// 400 on Name/a/x.mkv, the seeder's answer to a file where a folder is.
function bad400() {
    let x = 0;
    return {
        items: [item(folder('a', { 'x.mkv': 10 })), item(fileEntry('/b.mkv', 20)), item(fileEntry('/c.mkv', 30))],
        opts: { id: U0, name: 'Name' },
        put: (p) => {
            if (p.path.startsWith('Name/a/')) {
                if (++x === 2) throw new Error('rename loop');
                return { status: 400, text: '' };
            }
            return ok201;
        },
    };
}

// Every PUT 403; a third token request would be a loop.
function forbidden403() {
    return {
        items: [asFile('a', 10)],
        token: (body, n) => {
            if (n > 2) throw new Error('renew loop');
            return grant(n);
        },
        put: () => ({ status: 403, text: '' }),
    };
}

const firstToken = (answer) => ({ items: [asFile('a', 10)], token: () => answer });
const quotaFull = () => firstToken({ status: 413, json: { code: 'quota', used: 9 << 30, cap: 10 << 30 } });
const auth401 = () => firstToken({ status: 401, json: {} });
const files413 = () => ({ items: [asFile('a', 10)], put: () => ({ status: 413, text: 'files' }) });
const allDone = () => ({ items: [asFile('a', 10), asFile('b', 20)] });

function launch(t, sc, { opts = {}, ...more } = {}) {
    const r = rig(t, { token: sc.token, put: sc.put, wait: sc.wait, ...more });
    r.promise = r.run(sc.items, { ...sc.opts, ...opts });
    return r;
}

test('fill', () => {
    assert.equal(fill('A {{.Used}} B {{ .Cap }}', { Used: '1', Cap: '2' }), 'A 1 B 2');
});

test('safeSegment', () => {
    assert.equal(safeSegment('a~b.mkv'), 'a～b.mkv');
    assert.equal(safeSegment('é'), 'é');
    const long = safeSegment('я'.repeat(150) + '.mkv');
    assert.ok(new TextEncoder().encode(long).length <= 255, long);
    assert.ok(long.endsWith('.mkv'));
});

test('rootName', () => {
    const loose = 'Files 2026-11-18 09.05';
    assert.equal(rootName([item(folder('Photos', { a: 1 }))], loose), 'Photos');
    assert.equal(rootName([item(fileEntry('/IMG_0001.JPG'))], loose), 'IMG_0001');
    assert.equal(rootName([item(fileEntry('/a')), item(fileEntry('/b'))], loose), loose);
    // Upload a folder: files without entries, by their relative paths.
    assert.equal(rootName([picked('Photos/a'), picked('Photos/s/b'), picked('Photos/c')], loose), 'Photos');
    assert.equal(rootName([picked('Photos/IMG.JPG')], loose), 'Photos');
    assert.equal(rootName([picked('Photos/a'), picked('Other/b')], loose), loose);
});

// A drop of loose files is named in the page's language, by the locale's
// date. It is the upload's root folder for good: no '/' (es, fr, it, pt and
// zh write dates with one, and it would split the name into folders), no ':'
// (Windows refuses it in a folder name), nothing left of the template.
test('defaultName: the drop\'s time in the page\'s language, as a folder can be named', () => {
    const at = new Date(2026, 10, 27, 14, 5);
    const name = (lang) => {
        const msgs = JSON.parse(readFileSync(new URL(`../../../../locales/${lang}.json`, import.meta.url), 'utf8'));
        return defaultName(makeI18n(msgs).t, lang, at);
    };
    assert.equal(name('en'), 'Files 2026-11-27 14.05');
    assert.equal(name('ru'), 'Файлы 27.11.2026 14.05');
    for (const lang of ['en', 'ru', 'de', 'es', 'fr', 'it', 'nl', 'pl', 'pt', 'tr', 'cs', 'zh']) {
        const n = name(lang);
        assert.doesNotMatch(n, /[/\\:{}]/, `${lang}: ${n}`);
        assert.match(n, /2026/, `${lang}: ${n}`);
        assert.match(n, /\b27\b/, `${lang}: ${n}`);
        assert.match(n, /14\.05$/, `${lang}: ${n}`);
        assert.notEqual(n.trim().replace(/[\d. -]/g, ''), '', `${lang}: ${n} has no word`);
    }
});

// Loose files go into one folder, the new upload's, named by defaultName;
// the token asks for that same name. (Here t() answers with the key: no
// locale bundle is loaded in a test.)
test('loose files go into one folder named by defaultName', async (t) => {
    const r = rig(t);
    await r.run([asFile('a', 1), asFile('b', 2)]);
    const name = defaultName((k) => k, 'en');
    assert.deepEqual(r.calls.token[0], { name, want: 3 });
    assert.deepEqual(r.calls.put.map((p) => p.path).sort(), [`${name}/a`, `${name}/b`]);
});

test('a picked folder keeps its name', async (t) => {
    const r = rig(t);
    await r.run([picked('Photos/a', 1), picked('Photos/s/b', 2), picked('Photos/c', 3)]);
    assert.deepEqual(r.calls.token[0], { name: 'Photos', want: 6 });
    assert.deepEqual(r.calls.put.map((p) => p.path).sort(), ['Photos/a', 'Photos/c', 'Photos/s/b']);
    const one = rig(t);
    await one.run([picked('Photos/IMG.JPG')]);
    assert.deepEqual(one.calls.put.map((p) => p.path), ['Photos/IMG.JPG']);
});

test('nextName', () => {
    assert.equal(nextName('R/a/b.mkv'), 'R/a/b (2).mkv');
    assert.equal(nextName('R/b (2).mkv'), 'R/b (3).mkv');
    assert.equal(nextName('R/b'), 'R/b (2)');
});

test('sizeBucket', () => {
    assert.equal(sizeBucket(0), '<100M');
    assert.equal(sizeBucket(104857599), '<100M');
    assert.equal(sizeBucket(104857600), '<1G');
    assert.equal(sizeBucket(1073741824), '<10G');
    assert.equal(sizeBucket(10737418239), '<10G');
    assert.equal(sizeBucket(10737418240), '10G+');
});

test('an empty folder asks for nothing', async (t) => {
    const r = rig(t);
    umami(t, r.log);
    await r.run([item(folder('Empty', {}))]);
    assert.equal(r.calls.token.length, 0);
    assert.equal(r.calls.put.length, 0);
    assert.equal(r.log.filter(([k]) => k.startsWith('track:')).length, 0);
    assert.equal(r.assigned.length, 0);
    assert.ok(r.host.classList.contains('hidden'), 'the log is not shown');
    assert.equal(window.__uploadBusy, false);
});

test('a failing entry does not leave the tab busy', async (t) => {
    const r = rig(t);
    umami(t, r.log);
    await r.run([item(fileEntry('/a', 10)), item(fileEntry('/b', 20, { fail: true })), item(fileEntry('/c', 30))]);
    assert.deepEqual(r.calls.put.map(last).sort(), ['a', 'c']);
    assert.equal(r.calls.token[0].want, 40);
    assert.equal(r.assigned.length, 0);
    assert.deepEqual(ends(r.log), ['errors']);
    assert.equal(window.__uploadBusy, false);

    const r2 = rig(t);
    await assert.rejects(r2.run([item(folder('Photos', { a: 1 }, { throws: true }))]), /walk/);
    assert.equal(r2.calls.token.length, 0);
    assert.equal(window.__uploadBusy, false);
});

test('a second 403 in a row stops the upload', async (t) => {
    const r = launch(t, forbidden403());
    await r.promise;
    assert.equal(r.calls.token.length, 2);
    assert.deepEqual(r.calls.token[1], { id: U0, want: 0 });
    assert.equal(r.calls.put.length, 2);
    assert.match(r.status(), /upload\.unavailable/);
    assert.equal(r.assigned.length, 0);
    assert.equal(window.__uploadBusy, false);
});

test('409 root stops the upload', async (t) => {
    const r = launch(t, root409());
    await r.promise;
    assert.deepEqual(r.calls.put.map(last), ['a', 'b']);
    assert.equal(r.calls.token.length, 1);
    assert.match(r.status(), /upload\.unavailable/);
    assert.equal(r.assigned.length, 0);
});

test('token answers', async (t) => {
    for (const [name, answer, text, result] of [
        ['404', { status: 404, json: {} }, 'upload.unavailable', 'gone'],
        ['410', { status: 410, json: {} }, 'upload.unavailable', 'gone'],
        ['409 frozen', { status: 409, json: { code: 'frozen' } }, 'upload.state.frozen', 'gone'],
        ['500', { status: 500, json: {} }, 'upload.unavailable', 'errors'],
        ['403', { status: 403, json: {} }, 'upload.unavailable', 'errors'],
    ]) {
        await t.test(name, async (t) => {
            const r = launch(t, firstToken(answer));
            umami(t, r.log);
            await r.promise;
            assert.ok(r.status().includes(text), r.status());
            if (text !== 'upload.unavailable') assert.ok(!r.status().includes('upload.unavailable'));
            assert.deepEqual(ends(r.log), [result]);
            assert.equal(r.calls.put.length, 0);
            assert.equal(r.assigned.length, 0);
        });
    }
});

test('the network drops three times', async (t) => {
    await t.test('on the token', async (t) => {
        let r;
        r = rig(t, {
            token: (body, n) => {
                if (n === 2) assert.equal(r.meta(), 'upload.offline');
                return n <= 3 ? { status: 0 } : grant(n);
            },
        });
        await r.run([asFile('a', 10)]);
        assert.deepEqual(r.calls.wait, [1000, 2000, 4000]);
        assert.equal(r.calls.put.length, 1);
        assert.equal(r.assigned.length, 1);
    });
    await t.test('on the PUT', async (t) => {
        const r = rig(t, { put: (p, _, n) => (n <= 3 ? { status: 503, text: '' } : ok201) });
        await r.run([asFile('a', 10)]);
        assert.deepEqual(r.calls.wait, [1000, 2000, 4000]);
        assert.deepEqual(r.calls.put.map((p) => p.offset), [0, 0, 0, 0]);
        assert.equal(r.calls.token.length, 1);
        assert.equal(r.assigned.length, 1);
    });
});

test('an empty drop asks for one byte', async (t) => {
    const r = rig(t);
    await r.run([asFile('empty.txt', 0)]);
    assert.deepEqual(r.calls.token[0], { name: 'empty', want: 1 });
    assert.equal(r.calls.put[0].size, 0);
    assert.equal(r.assigned.length, 1);
});

test('parts of 16 MiB, one at a time per file, two files at once', async (t) => {
    const flying = new Map();
    let most = 0;
    let overlap = false;
    let r;
    let lineAtThird;
    r = rig(t, {
        put: async (p) => {
            if (p.path === 'Photos/big' && p.offset === 2 * PART) lineAtThird = r.meta();
            if (flying.has(p.path)) overlap = true;
            flying.set(p.path, true);
            most = Math.max(most, flying.size);
            await tick();
            flying.delete(p.path);
            return p.offset + PART >= p.size ? ok201 : { status: 204, text: '' };
        },
    });
    const big = 41943040;
    await r.run([item(folder('Photos', { big, b: 10, c: 20, d: 30 }))]);
    assert.deepEqual(r.calls.token[0], { name: 'Photos', want: big + 60 });
    const parts = r.calls.put.filter((p) => p.path === 'Photos/big');
    assert.deepEqual(parts.map((p) => p.offset), [0, PART, 2 * PART]);
    assert.ok(parts.every((p) => p.size === big));
    assert.equal(most, 2);
    assert.equal(overlap, false);
    assert.equal(r.calls.put.length, 6);
    assert.ok(r.calls.put.every((p) => p.token === 't1' && p.apiKey === 'k'));
    // The line counts the parts sent: big's two are 80% of the drop, less
    // the small files not stored yet.
    assert.match(lineAtThird, /^\d \/ 4 · (79|80)%$/);
});

test('a throwing transport rejects the run', async (t) => {
    const r = rig(t, {
        put: (p, _, n) => {
            if (n === 1) throw new Error('boom');
            return ok201;
        },
    });
    await assert.rejects(r.run([asFile('a', 10)]), /boom/);
    assert.equal(r.calls.put.length, 1);
    assert.equal(window.__uploadBusy, false);
});

test('409 with an offset resumes there', async (t) => {
    const r = rig(t, { put: (p, _, n) => (n === 1 ? { status: 409, text: '1234' } : ok201) });
    await r.run([asFile('a', 5000)]);
    assert.deepEqual(r.calls.put.map((p) => p.offset), [0, 1234]);
});

test('409 exists takes the next name', async (t) => {
    const r = rig(t, { put: (p, _, n) => (n === 1 ? { status: 409, text: 'exists' } : ok201) });
    await r.run([item(fileEntry('/x.mkv', 10))]);
    assert.deepEqual(r.calls.put.map((p) => [p.path, p.offset]), [['x/x.mkv', 0], ['x/x (2).mkv', 0]]);
});

test('409 deleted renames up to 5 times', async (t) => {
    let n = 0;
    const r = rig(t, {
        put: (p) => {
            if (!p.path.startsWith('Name/a/')) return ok201;
            if (++n === 7) throw new Error('rename loop');
            return { status: 409, text: 'deleted' };
        },
    });
    await r.run([item(folder('a', { 'x.mkv': 10 })), item(fileEntry('/b.mkv', 20))], { id: U0, name: 'Name' });
    assert.deepEqual(r.calls.put.filter((p) => p.path.startsWith('Name/a/')).map((p) => p.path),
        ['Name/a/x.mkv', 'Name/a/x (2).mkv', 'Name/a/x (3).mkv', 'Name/a/x (4).mkv', 'Name/a/x (5).mkv', 'Name/a/x (6).mkv']);
    assert.ok(r.calls.put.some((p) => p.path === 'Name/b.mkv'));
    assert.equal(window.__uploadBusy, false);
    // Named as it was dropped, not under the last name it tried.
    const failed = r.host.querySelector('pre.warn-summary[task-summary="failed"]').textContent;
    assert.match(failed, /\na\/x\.mkv$/);
    assert.doesNotMatch(failed, /\(6\)|b\.mkv/);
});

test('409 deleted twice, then 201', async (t) => {
    const r = rig(t, {
        put: (p) => (['Name/a/x.mkv', 'Name/a/x (2).mkv'].includes(p.path) ? { status: 409, text: 'deleted' } : ok201),
    });
    await r.run([item(folder('a', { 'x.mkv': 10 }))], { id: U0, name: 'Name' });
    const under = r.calls.put.filter((p) => p.path.startsWith('Name/a/'));
    assert.deepEqual(under.map((p) => [p.path, p.offset]), [['Name/a/x.mkv', 0], ['Name/a/x (2).mkv', 0], ['Name/a/x (3).mkv', 0]]);
    assert.equal(r.host.querySelector('pre.warn-summary[task-summary="failed"]'), null, 'no file failed');
    assert.equal(r.assigned.length, 1);
});

test('busy mark', async (t) => {
    let clock = Date.now();
    t.mock.method(Date, 'now', () => clock);

    await t.test('kept fresh by the network events of the run', async (t) => {
        const w = ownWindow(t, 'https://example.com/');
        const key = 'upload-busy:' + U0;
        const mark = () => w.localStorage.getItem(key);
        assert.equal(mark(), null);
        const bCalled = defer();
        const bHold = defer();
        let atAssign;
        const r = rig(t, {
            token: (body, n) => {
                assert.equal(mark(), String(clock));
                clock += 6000;
                return grant(n);
            },
            put: async (p, onProgress) => {
                if (last(p) === 'a') {
                    assert.equal(mark(), String(clock));
                    clock += 6000;
                    onProgress(1);
                    assert.equal(mark(), String(clock));
                    clock += 1000;
                    onProgress(2);
                    assert.equal(mark(), String(clock - 1000));
                    await bCalled.promise;
                    clock += 6000;
                    return ok201;
                }
                if (last(p) === 'b') {
                    bCalled.resolve();
                    return bHold.promise;
                }
                assert.equal(mark(), String(clock));
                bHold.resolve(ok201);
                return ok201;
            },
            onAssign: () => { atAssign = mark(); },
        });
        await r.run([asFile('a'), asFile('b'), asFile('c')], { id: U0, name: 'Photos' });
        assert.equal(mark(), null);
        assert.equal(atAssign, null);
        assert.equal(r.calls.put.length, 3);

        // A new upload: no mark until its ID is known.
        const r2 = rig(t, {
            token: (body, n) => {
                assert.equal(w.localStorage.length, 0);
                return grant(n);
            },
            put: () => {
                assert.equal(mark(), String(clock));
                return ok201;
            },
        });
        await r2.run([asFile('a')]);
        assert.equal(r2.calls.put.length, 1);

        // A stop takes the mark away as well.
        const r3 = rig(t, { put: () => ({ status: 410, text: '' }) });
        await r3.run([asFile('a')], { id: U0, name: 'Photos' });
        assert.equal(mark(), null);
    });

    await t.test('no storage', async (t) => {
        const w = ownWindow(t);
        Object.defineProperty(w, 'localStorage', { get() { throw new Error('denied'); } });
        const busy = [];
        const r = rig(t, { put: () => { busy.push(window.__uploadBusy); return ok201; } });
        await r.run([asFile('a'), asFile('b'), asFile('c')], { id: U0, name: 'Photos' });
        assert.deepEqual(busy, [true, true, true]);
        assert.equal(window.__uploadBusy, false);
    });
});

test('busy flag', async (t) => {
    const seen = [];
    const beforeunload = () => {
        const e = new window.Event('beforeunload', { cancelable: true });
        window.dispatchEvent(e);
        return e.defaultPrevented;
    };
    await t.test('all stored', async (t) => {
        let atAssign;
        const r = rig(t, {
            token: (b, n) => { seen.push(window.__uploadBusy, beforeunload()); return grant(n); },
            put: () => { seen.push(window.__uploadBusy, beforeunload()); return ok201; },
            onAssign: () => { atAssign = window.__uploadBusy; },
        });
        await r.run([asFile('a'), asFile('b')]);
        assert.ok(seen.length === 6 && seen.every(Boolean), String(seen));
        assert.equal(atAssign, false);
        assert.equal(beforeunload(), false);
    });
    await t.test('410', async (t) => {
        const r = launch(t, gone410());
        await r.promise;
        assert.equal(window.__uploadBusy, false);
        assert.equal(beforeunload(), false);
    });
    await t.test('the first token refused', async (t) => {
        const r = launch(t, quotaFull());
        await r.promise;
        assert.equal(window.__uploadBusy, false);
        assert.equal(beforeunload(), false);
    });
});

test('two runs at once start one', async (t) => {
    const r = rig(t);
    const p1 = r.run([asFile('a', 10)]);
    const p2 = r.run([asFile('b', 20)]);
    await Promise.all([p1, p2]);
    assert.equal(r.calls.token.length, 1);
    assert.equal(r.calls.token[0].want, 10);
    assert.deepEqual(r.calls.put.map(last), ['a']);
    assert.equal(window.__uploadBusy, false);
    await r.run([asFile('b', 20)]);
    assert.equal(r.calls.token.length, 2);
    assert.equal(r.calls.token[1].want, 20);
});

test('renewal asks for nothing more', async (t) => {
    const r = rig(t, { put: (p, _, n) => (n === 1 ? { status: 403, text: '' } : ok201) });
    await r.run([asFile('a', 10), asFile('b', 20), asFile('c', 30)]);
    assert.equal(r.calls.token[0].want, 60);
    assert.equal(r.calls.token.length, 2);
    assert.deepEqual(r.calls.token[1], { id: U0, want: 0 });
    const first = r.calls.put[0].path;
    assert.deepEqual(r.calls.put.filter((p) => p.path === first).map((p) => [p.token, p.offset]), [['t1', 0], ['t2', 0]]);
    assert.equal(r.assigned.length, 1);
});

test('413 size asks for a token once', async (t) => {
    const aDone = defer();
    const r = rig(t, {
        token: (body, n) => {
            if (n > 2) throw new Error('size loop');
            return grant(n);
        },
        put: async (p) => {
            if (last(p) === 'a') {
                setTimeout(aDone.resolve, 0);
                return ok201;
            }
            if (p.token === 't1') {
                if (last(p) === 'b') await aDone.promise;
                return { status: 413, text: 'size' };
            }
            return ok201;
        },
    });
    await r.run([asFile('a', 10), asFile('b', 20), asFile('c', 30)]);
    assert.equal(r.calls.token.length, 2);
    assert.deepEqual(r.calls.token[1], { id: U0, want: 50 });
    assert.deepEqual(r.calls.put.filter((p) => last(p) === 'b').map((p) => [p.token, p.offset]), [['t1', 0], ['t2', 0]]);
    assert.doesNotMatch(r.status(), /upload\.quotaOut/);
    assert.equal(r.assigned.length, 1);
    assert.equal(r.host.querySelector('pre.warn-summary[task-summary="failed"]'), null, 'no file failed');
});

test('a second 413 size is final', async (t) => {
    const r = launch(t, sizeFinal());
    await r.promise;
    assert.equal(r.calls.token.length, 2);
    assert.deepEqual(r.calls.put.filter((p) => last(p) === 'b').map((p) => p.token), ['t1', 't2']);
    assert.equal(r.calls.put.filter((p) => last(p) === 'a').length, 1);
    assert.equal(r.calls.put.filter((p) => last(p) === 'c').length, 0);
    assert.match(r.status(), /upload\.quotaOut/);
    assert.doesNotMatch(r.status(), /upload\.quotaFull/);
    assert.equal(r.assigned.length, 0);
    assert.equal(window.__uploadBusy, false);
});

test('413 quota on the re-request ends as 413 size', async (t) => {
    const r = launch(t, quotaOnReRequest());
    await r.promise;
    assert.equal(r.calls.token.length, 2);
    assert.match(r.status(), /upload\.quotaOut/);
    assert.doesNotMatch(r.status(), /upload\.quotaFull/);
    assert.equal(r.calls.put.filter((p) => last(p) === 'c').length, 0);
    assert.equal(r.assigned.length, 0);
    assert.equal(window.__uploadBusy, false);
});

test('a renewed token may ask again', async (t) => {
    const r = rig(t, {
        token: (body, n) => {
            if (n > 4) throw new Error('size loop');
            return grant(n);
        },
        put: (p) => ({ t1: { status: 413, text: 'size' }, t2: { status: 403, text: '' }, t3: { status: 413, text: 'size' } })[p.token] || ok201,
    });
    await r.run([item(fileEntry('/b', 20))]);
    assert.deepEqual(r.calls.token, [{ name: 'b', want: 20 }, { id: U0, want: 20 }, { id: U0, want: 0 }, { id: U0, want: 20 }]);
    assert.deepEqual(r.calls.put.map((p) => p.token), ['t1', 't2', 't3', 't4']);
    assert.doesNotMatch(r.status(), /upload\.quotaOut/);
    assert.equal(r.assigned.length, 1);
});

test('410 stops the upload', async (t) => {
    const r = launch(t, gone410());
    await r.promise;
    assert.deepEqual(r.calls.put.map(last), ['a', 'b']);
    assert.equal(r.calls.token.length, 1);
    assert.match(r.status(), /upload\.unavailable/);
    assert.equal(r.assigned.length, 0);
});

test('409 frozen stops the upload', async (t) => {
    const r = launch(t, frozen409());
    await r.promise;
    assert.deepEqual(r.calls.put.map(last), ['a', 'b']);
    assert.equal(r.calls.token.length, 1);
    assert.match(r.status(), /upload\.state\.frozen/);
    assert.doesNotMatch(r.status(), /upload\.unavailable/);
    assert.equal(r.assigned.length, 0);
    assert.equal(window.__uploadBusy, false);
});

test('vault button is held while an upload runs', async (t) => {
    const w = ownWindow(t);
    w.document.body.innerHTML = `<div id="pledge-button"><form><button type="submit" id="save"><svg><path id="icon"/></svg></button></form></div>
<div id="pledge-modal"><form action="/en/vault/add" method="post"><button type="submit" id="store"></button></form><form method="dialog"><button id="cancel"></button></form></div>
<button data-upload-add data-upload-id="${U0}" data-busy="${BUSY}"></button><button id="other"></button>`;
    for (const f of w.document.forms) f.addEventListener('submit', (e) => e.preventDefault());
    const said = [];
    w.toast = { error: (m) => said.push(m) };
    const el = (id) => w.document.getElementById(id);
    const click = (node) => node.dispatchEvent(new w.MouseEvent('click', { bubbles: true, cancelable: true }));
    const add = w.document.querySelector('[data-upload-add]');
    // 1
    assert.equal(click(el('save')), true);
    assert.equal(said.length, 0);
    const r = rig(t, {
        put: (p, _, n) => {
            if (n !== 1) return ok201;
            // 2
            assert.equal(click(el('save')), false);
            assert.deepEqual(said, [BUSY]);
            assert.equal(click(el('icon')), false);
            assert.equal(click(el('store')), false);
            assert.equal(click(el('cancel')), true);
            assert.equal(said.length, 3);
            // 3
            add.remove();
            assert.equal(click(el('save')), true);
            assert.equal(click(el('store')), true);
            assert.equal(said.length, 3);
            w.document.body.append(add);
            // 4
            assert.equal(click(el('other')), true);
            return ok201;
        },
    });
    await r.run([asFile('a')], { id: U0, name: 'Photos' });
    // 5
    assert.equal(click(el('save')), true);
    assert.equal(click(el('store')), true);
    assert.equal(said.length, 3);
    // 6
    let during;
    const r2 = rig(t, { put: () => { during = click(el('save')); return ok201; } });
    await r2.run([asFile('a')], { id: U0, name: 'Photos' });
    assert.equal(during, false);
    assert.equal(said.length, 4);
});

test('413 quota on the first token', async (t) => {
    for (const upsell of [true, false]) {
        await t.test(`upsell ${upsell}`, async (t) => {
            const r = launch(t, quotaFull(), { opts: { upsell } });
            await r.promise;
            assert.match(r.status(), /upload\.quotaFull/);
            assert.equal(r.status().includes('upload.upsell'), upsell);
            assert.equal(r.calls.put.length, 0);
        });
    }
});

test('413 size twice and 413 files', async (t) => {
    for (const upsell of [true, false]) {
        await t.test(`size, upsell ${upsell}`, async (t) => {
            const r = rig(t, { put: () => ({ status: 413, text: 'size' }) });
            await r.run([asFile('a')], { upsell });
            assert.equal(r.calls.token.length, 2);
            assert.match(r.status(), /upload\.quotaOut/);
            assert.equal(r.status().includes('upload.upsell'), upsell);
        });
    }
    await t.test('files', async (t) => {
        const r = launch(t, files413(), { opts: { upsell: true } });
        await r.promise;
        assert.match(r.status(), /upload\.tooManyFiles/);
        assert.doesNotMatch(r.status(), /upload\.upsell/);
        assert.equal(r.calls.token.length, 1);
    });
});

test('400 on one file does not stop the rest and does not rename', async (t) => {
    const r = launch(t, bad400());
    await r.promise;
    assert.deepEqual(r.calls.put.filter((p) => p.path.startsWith('Name/a/')).map((p) => p.path), ['Name/a/x.mkv']);
    assert.equal(r.calls.wait.length, 0);
    assert.equal(r.calls.token.length, 1);
    assert.deepEqual(r.calls.put.filter((p) => !p.path.startsWith('Name/a/')).map((p) => p.path).sort(), ['Name/b.mkv', 'Name/c.mkv']);
    assert.doesNotMatch(r.status(), /upload\.unavailable/);
    assert.equal(r.assigned.length, 0);
    assert.equal(window.__uploadBusy, false);
});

test('pauses: 507, 429 and the network', async (t) => {
    await t.test('507', async (t) => {
        const r = rig(t, { put: (p, _, n) => (n === 1 ? { status: 507, text: '' } : ok201) });
        await r.run([asFile('a')]);
        assert.deepEqual(r.calls.wait, [60000]);
        assert.deepEqual(r.calls.put.map((p) => p.offset), [0, 0]);
    });
    await t.test('a pause keeps the line while another file goes on', async (t) => {
        const held = defer();
        let r;
        let during;
        let spoken;
        r = rig(t, {
            put: (p, onProgress, n) => {
                if (last(p) === 'a' && n === 1) return { status: 507, text: '' };
                if (last(p) !== 'b') return ok201;
                return (async () => {
                    await tick();
                    onProgress(5);
                    during = r.meta();
                    spoken = r.host.querySelector('[data-job-announce]').textContent;
                    held.resolve();
                    return ok201;
                })();
            },
            wait: () => held.promise,
        });
        await r.run([asFile('a', 10), asFile('b', 20)]);
        assert.equal(during, 'upload.nodeFull');
        assert.equal(r.meta(), '2 / 2 · 100%');
        // Said to a screen reader too: the line's numbers are not.
        assert.equal(spoken, 'upload.nodeFull');
    });
    await t.test('429 waits its constant, not what the answer says', async (t) => {
        const r = rig(t, { put: (p, _, n) => (n === 1 ? { status: 429, text: '', retryAfter: '30' } : ok201) });
        await r.run([asFile('a')]);
        assert.deepEqual(r.calls.wait, [5000]);
        assert.deepEqual(r.calls.put.map((p) => p.offset), [0, 0]);
    });
    await t.test('three drops', async (t) => {
        let r;
        const texts = [];
        const said = [];
        r = rig(t, { put: (p, _, n) => { texts.push(r.meta()); said.push(r.host.querySelector('[data-job-announce]').textContent); return n <= 3 ? { status: 0 } : ok201; } });
        await r.run([asFile('a')]);
        assert.deepEqual(r.calls.wait, [1000, 2000, 4000]);
        assert.deepEqual(texts, ['0%', 'upload.offline', 'upload.offline', 'upload.offline']);
        assert.deepEqual(said, ['upload.state.temporary', 'upload.offline', 'upload.offline', 'upload.offline']);
        // The connection came back: the numbers are back in place of the
        // pause.
        assert.equal(r.meta(), '100%');
    });
});

test('401 on the token asks to sign in again', async (t) => {
    const r = launch(t, auth401());
    await r.promise;
    assert.match(r.status(), /upload\.signInAgain/);
    const signIn = r.host.querySelector('[data-upload-sign-in]');
    assert.equal(signIn.getAttribute('href'), '/login?return-url=%2F');
    assert.equal(signIn.hidden, false);
    assert.equal(r.host.querySelector('.alert-close-wrapper').classList.contains('hidden'), false);
    assert.equal(r.calls.put.length, 0);
});

test('add files', async (t) => {
    const r = rig(t);
    await r.run([item(folder('Other', { a: 1, s: { b: 2 } }))], { id: U0, name: 'Photos' });
    assert.ok(r.calls.put.every((p) => p.path.startsWith('Photos/Other/')), JSON.stringify(r.calls.put));
    assert.deepEqual(r.calls.token[0], { id: U0, want: 3 });
});

// A page that does not know the upload's name gives no folder (lib/drop.js):
// the root is named here, as a new upload's is, and a folder of that name
// dropped resumes it.
test('add files to an upload whose name the page does not know', async (t) => {
    const loose = rig(t);
    await loose.run([asFile('a', 1), asFile('b', 2)], { id: U0, name: '' });
    const name = defaultName((k) => k, 'en');
    assert.deepEqual(loose.calls.put.map((p) => p.path).sort(), [`${name}/a`, `${name}/b`]);
    assert.deepEqual(loose.calls.token[0], { id: U0, want: 3 });

    const one = rig(t);
    await one.run([asFile('IMG.JPG')], { id: U0, name: '' });
    assert.deepEqual(one.calls.put.map((p) => p.path), ['IMG/IMG.JPG']);

    const folder1 = rig(t);
    await folder1.run([item(folder('Photos', { a: 1, s: { b: 2 } }))], { id: U0, name: '' });
    assert.deepEqual(folder1.calls.put.map((p) => p.path).sort(), ['Photos/a', 'Photos/s/b']);
});

test('add files into the open folder', async (t) => {
    const sub = { id: U0, name: 'Photos', dir: ['Photos', 'sub'] };
    const r = rig(t);
    await r.run([asFile('a.mkv', 5)], sub);
    assert.deepEqual(r.calls.put.map((p) => p.path), ['Photos/sub/a.mkv']);
    assert.deepEqual(r.calls.token[0], { id: U0, want: 5 });
    assert.deepEqual(r.assigned, [`/${U0}?pwd=%2FPhotos%2Fsub`]);

    const f = rig(t);
    await f.run([item(folder('f', { 1: 1, 2: 2 }))], sub);
    assert.deepEqual(f.calls.put.map((p) => p.path).sort(), ['Photos/sub/f/1', 'Photos/sub/f/2']);

    const root = rig(t);
    await root.run([asFile('a.mkv')], { id: U0, name: 'Photos', dir: ['Photos'] });
    assert.deepEqual(root.calls.put.map((p) => p.path), ['Photos/a.mkv']);
    assert.deepEqual(root.assigned, [`/${U0}`]);
});

// Done, the redirect goes to the folder the files went into. When the page
// shows that folder already it makes no history entry (progressLog.js), as
// html/template wrote the URL: a folder's link, the root's crumb.
test('done on the page it went into: no history entry', async (t) => {
    for (const [path, dir] of [
        [`/${U0}`, ['Photos']],
        [`/${U0}?pwd=%2fPhotos`, ['Photos']],
        [`/${U0}?pwd=%2fPhotos%2fsub`, ['Photos', 'sub']],
        [`/${U0}?pwd=%2fPhotos%2f%d0%a4`, ['Photos', 'Ф']],
    ]) {
        await t.test(path, async (t) => {
            ownWindow(t, 'https://webtor.io' + path);
            const r = rig(t);
            let push;
            r.host.addEventListener('click', (e) => { push = e.target.getAttribute('data-async-push-state'); });
            await r.run([asFile('a')], { id: U0, name: 'Photos', dir });
            assert.equal(r.assigned.length, 1);
            assert.equal(push, 'false', r.assigned[0]);
        });
    }
});

test('the same folder resumes', async (t) => {
    const photos = () => item(folder('Photos', { a: 1, s: { b: 6 } }));
    for (const [name, opts, items, want] of [
        ['into the root', { dir: ['Photos'] }, () => [photos()], ['Photos/a', 'Photos/s/b']],
        ['into a subfolder', { dir: ['Photos', 'sub'] }, () => [photos()], ['Photos/sub/Photos/a', 'Photos/sub/Photos/s/b']],
        ['with a file beside it', { dir: ['Photos'] }, () => [photos(), item(fileEntry('/c'))], ['Photos/Photos/a', 'Photos/Photos/s/b', 'Photos/c']],
        ['another folder', { dir: ['Photos'] }, () => [item(folder('Other', { a: 1 }))], ['Photos/Other/a']],
        ['a file named as the root', { dir: ['Photos'] }, () => [item(fileEntry('/Photos'))], ['Photos/Photos']],
        ['a normalized root', { name: 'My～Photos', dir: ['My～Photos'] }, () => [item(folder('My~Photos', { a: 1 }))], ['My～Photos/a']],
        ['a raw root', { name: 'My~Photos', dir: ['My~Photos'] }, () => [item(folder('My~Photos', { a: 1 }))], ['My～Photos/a']],
        ['a decomposed root', { name: 'Café', dir: ['Café'] }, () => [item(folder('Café', { a: 1 }))], ['Café/a']],
    ]) {
        await t.test(name, async (t) => {
            const first = name === 'into the root';
            const sent = new Set();
            const r = rig(t, {
                put: (p) => {
                    if (!first) return ok201;
                    if (p.path === 'Photos/s/b' && !sent.has(p.path)) {
                        sent.add(p.path);
                        return { status: 409, text: '5' };
                    }
                    return ok201;
                },
            });
            await r.run(items(), { id: U0, name: 'Photos', ...opts });
            assert.deepEqual([...new Set(r.calls.put.map((p) => p.path))].sort(), want);
            if (first) {
                assert.deepEqual(r.calls.put.filter((p) => p.path === 'Photos/a').length, 1);
                assert.deepEqual(r.calls.put.filter((p) => p.path === 'Photos/s/b').map((p) => p.offset), [0, 5]);
                assert.deepEqual(r.calls.token[0], { id: U0, want: 7 });
                assert.deepEqual(r.assigned, [`/${U0}`]);
            }
        });
    }
});

test('upload events', async (t) => {
    await t.test('start once, end before leaving', async (t) => {
        const r = rig(t);
        umami(t, r.log);
        await r.run([asFile('a'), asFile('b')]);
        const tracks = r.log.filter(([k]) => k.startsWith('track:'));
        assert.deepEqual(tracks, [
            ['track:upload-start', { files: 2, sizeBucket: '<100M', add: 0 }],
            ['track:upload-end', { result: 'done', sizeBucket: '<100M', minutes: 0, add: 0 }],
        ]);
        const at = (k) => r.log.findIndex(([n]) => n === k);
        assert.ok(at('put') < at('track:upload-start') && at('track:upload-start') < at('track:upload-end'));
        assert.ok(at('track:upload-end') < at('assign'));
        assert.deepEqual(Object.keys(tracks[0][1]).sort(), ['add', 'files', 'sizeBucket']);
        assert.deepEqual(Object.keys(tracks[1][1]).sort(), ['add', 'minutes', 'result', 'sizeBucket']);
    });
    await t.test('the page does not wait for a slow tracker', async (t) => {
        const r = rig(t);
        umami(t, r.log, never);
        r.run([asFile('a'), asFile('b')]);
        await until(() => r.assigned.length === 1, 'the page to be left');
        assert.equal(r.calls.put.length, 2);
        assert.ok(r.calls.wait.includes(1000));
    });
    await t.test('a fast upload waits for both events', async (t) => {
        const start = defer();
        const r = rig(t, { wait: never });
        umami(t, r.log, (name) => (name === 'upload-start' ? start.promise : Promise.resolve()));
        r.run([asFile('a')]);
        await until(() => r.log.some(([k]) => k === 'track:upload-end'), 'the end event');
        await tick();
        assert.equal(r.assigned.length, 0);
        start.resolve();
        await until(() => r.assigned.length === 1, 'the page to be left');
    });
    await t.test('quota before the first byte', async (t) => {
        const r = launch(t, quotaFull());
        umami(t, r.log);
        await r.promise;
        const tracks = r.log.filter(([k]) => k.startsWith('track:'));
        assert.equal(tracks.length, 1);
        assert.equal(tracks[0][0], 'track:upload-end');
        assert.equal(tracks[0][1].result, 'quota');
        assert.equal(tracks[0][1].add, 0);
    });
    await t.test('add', async (t) => {
        const r = rig(t);
        umami(t, r.log);
        await r.run([asFile('a')], { id: U0, name: 'Photos', dir: ['Photos'] });
        assert.deepEqual(r.log.filter(([k]) => k.startsWith('track:')).map(([, d]) => d.add), [1, 1]);
    });
    await t.test('no tracker, no wait', async (t) => {
        const r = rig(t);
        await r.run([asFile('a'), asFile('b')]);
        assert.equal(r.assigned.length, 1);
        assert.equal(r.calls.wait.length, 0);
    });
    await t.test('a failing tracker does not break the upload', async (t) => {
        const r = rig(t);
        umami(t, r.log, (name) => {
            if (name === 'upload-start') throw new Error('tracker');
            return Promise.reject(new Error('tracker'));
        });
        await r.run([asFile('a'), asFile('b')]);
        assert.equal(r.calls.put.length, 2);
        assert.equal(r.assigned.length, 1);
    });
    await t.test('upload-end names the outcome', async (t) => {
        for (const [name, sc, start, result] of [
            ['410 on the first PUT', () => ({ items: [asFile('a')], put: () => ({ status: 410, text: '' }) }), false, 'gone'],
            ['410 stops the upload', gone410, null, 'gone'],
            ['409 frozen stops the upload', frozen409, null, 'gone'],
            ['token 401', auth401, false, 'auth'],
            ['a second 413 size is final', sizeFinal, true, 'size'],
            ['413 quota on the re-request', quotaOnReRequest, true, 'size'],
            ['400 on one file', bad400, true, 'errors'],
            ['413 files on the first PUT', files413, false, 'errors'],
            ['a second 403 in a row', forbidden403, false, 'gone'],
            ['409 root stops the upload', root409, null, 'gone'],
        ]) {
            await t.test(name, async (t) => {
                const r = launch(t, sc());
                umami(t, r.log);
                await r.promise;
                assert.deepEqual(ends(r.log), [result]);
                if (start !== null) assert.equal(starts(r.log).length, start ? 1 : 0);
                assert.equal(r.assigned.length, 0);
            });
        }
    });
});

test('a stored file opens the upload, all stored go there', async (t) => {
    let r;
    const footer = () => r.host.querySelector('.alert-close-wrapper');
    const open = () => r.host.querySelector('[data-upload-open]');
    const gotIt = () => r.host.querySelector('.closeable-close');
    const seen = [];
    r = rig(t, { put: () => { seen.push(footer().classList.contains('hidden'), open().hidden); return ok201; } });
    await r.run([asFile('a')]);
    assert.deepEqual(seen, [true, true], 'nothing to open before the first file is stored');
    assert.equal(open().getAttribute('href'), `/${U0}`);
    assert.equal(open().hidden, false);
    assert.equal(open().target, '_blank');
    assert.equal(open().rel, 'noopener');
    assert.equal(footer().classList.contains('hidden'), false);
    assert.equal(gotIt().hidden, true, 'no "got it" while the files go on');
    assert.match(r.status(), /upload\.done/);
    assert.deepEqual(r.assigned, [`/${U0}`]);

    // A stop after a stored file: the link stays, "got it" is back.
    const g = launch(t, gone410());
    await g.promise;
    assert.equal(g.host.querySelector('[data-upload-open]').hidden, false);
    assert.equal(g.host.querySelector('.closeable-close').hidden, false);
    assert.equal(g.host.querySelector('.alert-close-wrapper').classList.contains('hidden'), false);
    assert.ok(g.host.querySelector('.job-stage').classList.contains('job-stage-done'));
    assert.equal(g.assigned.length, 0);
});

// On the upload's page "Open" would open that same page: there is none,
// and the footer waits for the end, as "got it" does.
test("on the upload's page nothing opens it again", async (t) => {
    const page = { id: U0, name: 'Photos', dir: ['Photos'] };
    let r;
    const seen = [];
    r = rig(t, { put: () => { seen.push(r.host.querySelector('.alert-close-wrapper').classList.contains('hidden')); return ok201; } });
    await r.run([asFile('a'), asFile('b')], page);
    assert.deepEqual(seen, [true, true]);
    assert.equal(r.host.querySelector('[data-upload-open]').hidden, true);
    assert.deepEqual(r.assigned, [`/${U0}`]);

    const g = launch(t, gone410(), { opts: page });
    await g.promise;
    assert.ok(g.calls.put.some((p) => p.path === 'Photos/a'), 'a was stored');
    assert.equal(g.host.querySelector('[data-upload-open]').hidden, true);
    assert.equal(g.host.querySelector('.closeable-close').hidden, false);
    assert.equal(g.host.querySelector('.alert-close-wrapper').classList.contains('hidden'), false);
});

// The links of the footer are the last run's: a run stopped before its
// first file shows neither the last one's "Open" nor its "Sign in".
test("the next run's footer starts over", async (t) => {
    const quota = async () => ({ status: 413, json: { code: 'quota', used: 9 << 30, cap: 10 << 30 } });
    const footer = (r) => ({
        open: r.host.querySelector('[data-upload-open]').hidden,
        signIn: r.host.querySelector('[data-upload-sign-in]').hidden,
        gotIt: r.host.querySelector('.closeable-close').hidden,
    });
    await t.test('after a stop with a file stored', async (t) => {
        const r = launch(t, gone410());
        await r.promise;
        assert.equal(footer(r).open, false);
        r.tr.token = quota;
        await r.run([asFile('z')]);
        assert.match(r.status(), /upload\.quotaFull/);
        assert.deepEqual(footer(r), { open: true, signIn: true, gotIt: false });
    });
    await t.test('after a lost session', async (t) => {
        const r = launch(t, auth401());
        await r.promise;
        assert.equal(footer(r).signIn, false);
        r.tr.token = quota;
        await r.run([asFile('z')]);
        assert.deepEqual(footer(r), { open: true, signIn: true, gotIt: false });
    });
});

// On the upload's page the log is in the card of the files
// (partials/list.html), which a file picked or another page of the listing
// renders anew (lib/loadAsyncView.js) with an empty log in it: the run's
// own goes on there, and with it its verdict -- not past its redirect or
// its toast, nor onto another upload's page.
test('the log goes on in the card rendered anew', async (t) => {
    const card = () => `<div class="js-list-card" data-resource-id="${U0}">${uploadLog}</div>`;
    // stage: "Add files" of the upload `of` in the header, the card with
    // the run's log in #content; swap renders #content anew.
    const stage = (t, r, of = U0) => {
        const page = document.createElement('div');
        page.innerHTML = `<button data-upload-add data-upload-id="${of}"></button><div id="content">${card()}</div>`;
        document.body.append(page);
        t.after(() => page.remove());
        const content = page.querySelector('#content');
        content.querySelector('[data-upload-log]').replaceWith(r.host);
        return { page, content, swap: () => loadAsyncView(content, card()) };
    };
    const opts = { id: U0, name: 'Photos', dir: ['Photos'] };
    const held = () => {
        const a = defer();
        return { a, put: (p) => (last(p) === 'a' ? a.promise : ok201) };
    };
    await t.test('a file picked while the files go', async (t) => {
        const { said } = toasts(t);
        const h = held();
        const r = rig(t, { put: h.put });
        const { content, swap } = stage(t, r);
        const p = r.run([asFile('a'), asFile('b')], opts);
        await until(() => r.calls.put.length === 2, 'both files sent');
        swap();
        assert.ok(content.contains(r.host), "the run's log is in the new card");
        assert.equal(document.querySelectorAll('[data-upload-log]').length, 1);
        h.a.resolve(ok201);
        await p;
        assert.equal(r.assigned.length, 1, 'done, it shows the files');
        assert.equal(said.length, 0);
    });
    await t.test("another upload's page", async (t) => {
        const { said } = toasts(t);
        const h = held();
        const r = rig(t, { put: h.put });
        const { swap } = stage(t, r, 'f'.repeat(40));
        const p = r.run([asFile('a'), asFile('b')], opts);
        await until(() => r.calls.put.length === 2, 'both files sent');
        swap();
        assert.equal(r.host.isConnected, false);
        h.a.resolve(ok201);
        await p;
        assert.deepEqual(said, ['ok:upload.done']);
    });
    await t.test('the verdict stays', async (t) => {
        const s = gone410();
        const r = rig(t, { put: s.put });
        const { content, swap } = stage(t, r);
        await r.run(s.items, opts);
        swap();
        assert.ok(content.contains(r.host));
        assert.match(r.status(), /upload\.unavailable/);
    });
    // Left after the end, by an async link or Back: the verdict stays
    // behind, and the page come back to shows its own log.
    await t.test('the verdict is not brought back to the page', async (t) => {
        const s = gone410();
        const r = rig(t, { put: s.put });
        const { page, swap } = stage(t, r);
        const add = page.querySelector('[data-upload-add]');
        await r.run(s.items, opts);
        add.remove();
        swap();
        page.prepend(add);
        swap();
        assert.equal(r.host.isConnected, false);
    });
    // Left while the files go, the run's log comes back with them.
    await t.test('left and back while the files go', async (t) => {
        const h = held();
        const r = rig(t, { put: h.put });
        const { page, content, swap } = stage(t, r);
        const add = page.querySelector('[data-upload-add]');
        const p = r.run([asFile('a'), asFile('b')], opts);
        await until(() => r.calls.put.length === 2, 'both files sent');
        add.remove();
        swap();
        page.prepend(add);
        swap();
        assert.ok(content.contains(r.host), "the run's log is in the new card");
        h.a.resolve(ok201);
        await p;
        assert.equal(r.assigned.length, 1);
    });
    await t.test('the redirect renders the page anew', async (t) => {
        const r = rig(t);
        const { swap } = stage(t, r);
        await r.run([asFile('a')], opts);
        assert.equal(r.assigned.length, 1);
        swap();
        assert.equal(r.host.isConnected, false);
    });
    await t.test("a verdict's log is not kept past the next run's redirect", async (t) => {
        const s = gone410();
        const r = rig(t, { put: s.put });
        const { swap } = stage(t, r);
        await r.run(s.items, opts);
        r.tr.put = async () => ok201;
        await r.run([asFile('z')], opts);
        assert.equal(r.assigned.length, 1);
        swap();
        assert.equal(r.host.isConnected, false);
    });
    await t.test('the outcome was a toast', async (t) => {
        const { said } = toasts(t);
        const h = held();
        const r = rig(t, { put: h.put });
        const { page, swap } = stage(t, r);
        const add = page.querySelector('[data-upload-add]');
        const p = r.run([asFile('a'), asFile('b')], opts);
        await until(() => r.calls.put.length === 2, 'both files sent');
        add.remove();
        swap();
        h.a.resolve(ok201);
        await p;
        assert.deepEqual(said, ['ok:upload.done']);
        page.prepend(add);
        swap();
        assert.equal(r.host.isConnected, false);
    });
});

test('the progress is the line of the log it is given', async (t) => {
    let r;
    const seen = [];
    const scrolled = [];
    r = rig(t, {
        put: (p, onProgress) => {
            if (last(p) === 'a') {
                seen.push(r.meta());
                onProgress(5);
                seen.push(r.meta());
            }
            return ok201;
        },
    });
    r.host.scrollIntoView = (o) => scrolled.push(o);
    assert.ok(r.host.classList.contains('hidden'));
    await r.run([asFile('a', 10), asFile('b', 20)]);
    assert.equal(r.host.classList.contains('hidden'), false);
    assert.deepEqual(scrolled, [{ block: 'nearest' }]);
    assert.equal(r.host.querySelector('.job-stage-title').textContent, 'upload.uploading');
    // Files stored of files dropped, and the bytes sent as a percent.
    assert.deepEqual(seen, ['0 / 2 · 0%', '0 / 2 · 16%']);
    assert.equal(r.meta(), '2 / 2 · 100%');
    assert.equal(document.querySelectorAll('.job-stage').length, 1);
    // The verdict is the answer: the line goes.
    assert.equal(r.host.querySelector('.job-stage').hidden, true);
});

// A stop after which no new file starts but the ones in flight finish: the
// line goes on while they do, the verdict comes once they have. a is in
// flight until the test lets it go; b's answers close the upload.
test('the files in flight go on in the line after a stop', async (t) => {
    const quotaOnSecond = (body, n) => (n === 1 ? grant(1) : { status: 413, json: { code: 'quota', used: 10, cap: 10 } });
    for (const [name, b, token, closedAt, verdict] of [
        ['413 files', { status: 413, text: 'files' }, undefined, (r) => r.calls.put.length === 2, /upload\.tooManyFiles/],
        ['a second 413 size', { status: 413, text: 'size' }, undefined, (r) => r.calls.put.length === 3, /upload\.quotaOut/],
        ['413 quota on the re-request', { status: 413, text: 'size' }, quotaOnSecond, (r) => r.calls.token.length === 2, /upload\.quotaOut/],
    ]) {
        await t.test(name, async (t) => {
            const a = defer();
            let progress;
            const r = rig(t, {
                token,
                put: (p, onProgress) => {
                    if (last(p) !== 'a') return b;
                    progress = onProgress;
                    return a.promise;
                },
            });
            const stage = () => r.host.querySelector('.job-stage');
            const run = r.run([asFile('a', 10), asFile('b', 20), asFile('c', 30)]);
            await until(() => closedAt(r), 'the stop');
            await tick();
            assert.equal(stage().hidden, false, 'the line stays while a goes on');
            assert.equal(r.host.querySelector('[task-summary="upload"]'), null, 'no verdict yet');
            assert.equal(r.host.querySelectorAll('pre.info-summary').length, 1, 'the note stays');
            progress(5);
            assert.equal(r.meta(), '0 / 3 · 8%');
            assert.equal(r.host.querySelector('.job-stage-bar').value, 8);
            assert.equal(window.__uploadBusy, true);
            a.resolve(ok201);
            await run;
            assert.match(r.status(), verdict);
            assert.equal(r.host.querySelectorAll('[task-summary="upload"]').length, 1);
            assert.equal(stage().hidden, true);
            assert.equal(r.host.querySelector('[data-upload-open]').hidden, false);
            assert.equal(r.host.querySelector('.closeable-close').hidden, false);
            assert.equal(r.host.querySelector('.alert-close-wrapper').classList.contains('hidden'), false);
            assert.equal(r.calls.put.filter((p) => last(p) === 'c').length, 0);
            assert.equal(r.assigned.length, 0);
            assert.equal(window.__uploadBusy, false);
        });
    }
    // A stop that sends nothing more is the verdict at once, before the
    // part in flight comes back.
    await t.test('410', async (t) => {
        const a = defer();
        const r = rig(t, { put: (p) => (last(p) === 'a' ? a.promise : { status: 410, text: '' }) });
        const run = r.run([asFile('a', 10), asFile('b', 20)]);
        await until(() => r.calls.put.length === 2, 'the stop');
        await tick();
        assert.match(r.status(), /upload\.unavailable/);
        assert.equal(r.host.querySelector('.job-stage').hidden, true);
        a.resolve(ok201);
        await run;
        assert.equal(r.host.querySelectorAll('[task-summary="upload"]').length, 1);
    });
    // A close, then a stop on the file in flight: the first verdict, said
    // at the stop, once.
    await t.test('413 files, then 410', async (t) => {
        const a = defer();
        const r = rig(t, { put: (p) => (last(p) === 'a' ? a.promise : { status: 413, text: 'files' }) });
        const run = r.run([asFile('a', 10), asFile('b', 20)]);
        await until(() => r.calls.put.length === 2, 'the close');
        await tick();
        assert.equal(r.host.querySelector('.job-stage').hidden, false);
        a.resolve({ status: 410, text: '' });
        await run;
        assert.match(r.status(), /upload\.tooManyFiles/);
        assert.doesNotMatch(r.status(), /upload\.unavailable/);
        assert.equal(r.host.querySelectorAll('[task-summary="upload"]').length, 1);
        assert.equal(r.host.querySelector('.job-stage').hidden, true);
    });
});

// toasts writes the outcome toasts down for one test: kind and text, and
// how long each one stays.
function toasts(t) {
    const said = [];
    const held = [];
    window.toast = {
        success: (m, ms) => { said.push('ok:' + m); held.push(ms); },
        error: (m, ms) => { said.push('err:' + m); held.push(ms); },
    };
    t.after(() => { delete window.toast; });
    return { said, held };
}


test('the log is gone', async (t) => {
    for (const [name, sc, check, opts] of [
        ['all stored', allDone, (s) => { assert.equal(s, 'ok:upload.done'); assert.doesNotMatch(s, /upload\.state\./); }],
        ['413 quota', quotaFull, (s) => { assert.match(s, /^err:.*upload\.quotaFull/); assert.doesNotMatch(s, /upload\.upsell/); }, { upsell: true }],
        ['a second 413 size is final', sizeFinal, (s) => { assert.match(s, /^err:.*upload\.quotaOut/); }],
        ['token 401', auth401, (s) => { assert.match(s, /^err:.*upload\.signInAgain/); }],
        ['410 stops the upload', gone410, (s) => { assert.match(s, /^err:.*upload\.unavailable/); }],
        ['409 frozen stops the upload', frozen409, (s) => { assert.match(s, /^err:.*upload\.state\.frozen/); assert.doesNotMatch(s, /upload\.unavailable/); }],
        ['413 files on the first PUT', files413, (s) => { assert.match(s, /^err:.*upload\.tooManyFiles/); }],
        ['400 on one file', bad400, (s) => { assert.equal(s, 'err:upload.someFailed'); assert.doesNotMatch(s, /upload\.state\./); }],
    ]) {
        await t.test(name, async (t) => {
            const { said, held } = toasts(t);
            // The transport takes the log out of the document in its first
            // token call, before it answers, as an async navigation would.
            const s = sc();
            const token = s.token || ((b, n) => grant(n));
            let r;
            r = rig(t, { token: (body, n) => { if (n === 1) r.host.remove(); return token(body, n); }, put: s.put });
            await r.run(s.items, { ...s.opts, ...opts });
            assert.equal(r.assigned.length, 0);
            assert.equal(said.length, 1, String(said));
            assert.deepEqual(held, [10000]);
            check(said[0], r);
            assert.equal(window.__uploadBusy, false);
        });
    }
    await t.test('left after the stop text was written', async (t) => {
        const { said } = toasts(t);
        let r;
        const sc = sizeFinal({ onSecond: () => setTimeout(() => r.host.remove(), 0) });
        r = rig(t, { token: sc.token, put: sc.put });
        await r.run(sc.items);
        assert.equal(said.length, 1);
        assert.match(said[0], /^err:.*upload\.quotaOut/);
        assert.equal(r.assigned.length, 0);
    });
    await t.test('the page was left while the events were being sent', async (t) => {
        const { said } = toasts(t);
        let r;
        r = rig(t, { wait: () => { r.host.remove(); } });
        umami(t, r.log, never);
        await r.run([asFile('a'), asFile('b')]);
        assert.equal(r.assigned.length, 0);
        assert.deepEqual(said, ['ok:upload.done']);
    });
    await t.test('a pause is not an outcome', async (t) => {
        const { said } = toasts(t);
        let r;
        r = rig(t, {
            token: (body, n) => { r.host.remove(); return grant(n); },
            put: (p, _, n) => (n <= 3 ? { status: 503, text: '' } : ok201),
        });
        await r.run([asFile('a')]);
        assert.deepEqual(r.calls.wait, [1000, 2000, 4000]);
        assert.deepEqual(said, ['ok:upload.done']);
        assert.equal(r.assigned.length, 0);
    });
    await t.test('the tab is hidden', async (t) => {
        const { said, held } = toasts(t);
        let vis = 'hidden';
        Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => vis });
        t.after(() => { delete document.visibilityState; });
        let r;
        r = rig(t, { token: (body, n) => { r.host.remove(); return grant(n); } });
        await r.run([asFile('a'), asFile('b')]);
        assert.equal(said.length, 0);
        assert.equal(r.assigned.length, 0);
        assert.equal(window.__uploadBusy, false);
        vis = 'visible';
        document.dispatchEvent(new window.Event('visibilitychange'));
        assert.deepEqual(said, ['ok:upload.done']);
        assert.deepEqual(held, [10000]);
        document.dispatchEvent(new window.Event('visibilitychange'));
        assert.equal(said.length, 1);
    });
});

test('the log is there: no toast', async (t) => {
    const { said } = toasts(t);
    const r = rig(t);
    await r.run([asFile('a'), asFile('b')]);
    assert.equal(r.assigned.length, 1);
    const g = launch(t, gone410());
    await g.promise;
    assert.match(g.status(), /upload\.unavailable/);
    assert.equal(said.length, 0);
});

test('a failed file is named in the verdict', async (t) => {
    // failed: the warning at the end of a run, the files under its words.
    const failed = (r) => r.host.querySelector('pre.warn-summary[task-summary="failed"]')?.textContent.split('\n');
    const r = launch(t, bad400());
    await r.promise;
    assert.deepEqual(failed(r), ['upload.someFailed', 'a/x.mkv']);
    assert.equal(r.host.querySelector('[task-summary="upload"]'), null, 'the others went on: no stop');
    assert.equal(r.host.querySelector('.closeable-close').hidden, false);
    assert.equal(r.host.querySelector('.alert-close-wrapper').classList.contains('hidden'), false);
    assert.equal(r.assigned.length, 0);

    const w = rig(t);
    await w.run([item(fileEntry('/a', 10)), item(fileEntry('/unreadable', 20, { fail: true })), item(fileEntry('/c', 30))]);
    assert.deepEqual(failed(w), ['upload.someFailed', 'unreadable']);

    // Nothing readable: no upload is made for it.
    const none = rig(t);
    await none.run([item(fileEntry('/unreadable', 20, { fail: true }))]);
    assert.equal(none.calls.token.length, 0);
    assert.deepEqual(failed(none), ['upload.someFailed', 'unreadable']);
    assert.equal(window.__uploadBusy, false);

    // Ten names at most.
    const many = rig(t);
    await many.run(Array.from({ length: 12 }, (_, i) => item(fileEntry(`/f${i}`, 1, { fail: true }))));
    const names = failed(many);
    assert.equal(names.length, 12);
    assert.deepEqual(names.slice(1, 3), ['f0', 'f1']);
    assert.equal(names[11], '…');
});

test('a part that cannot be read fails its file, not the network', async (t) => {
    const failed = (r) => r.host.querySelector('pre.warn-summary[task-summary="failed"]')?.textContent.split('\n');
    // The browser answers a changed file as it answers a dropped connection:
    // no status. The second file was changed on disk after it was picked.
    let sends = 0;
    const r = rig(t, {
        put: (p) => {
            if (p.path !== 'x/b.tmp') return ok201;
            if (++sends > 3) throw new Error('retried as offline');
            return { status: 0, text: '' };
        },
        readable: async () => false,
    });
    await r.run([item(folder('x', { 'a.mkv': 10, 'b.tmp': 20 }))]);
    assert.equal(sends, 1, 'not sent again');
    assert.deepEqual(r.calls.wait, [], 'no offline pause');
    assert.deepEqual(failed(r), ['upload.someFailed', 'x/b.tmp']);
    assert.ok(r.calls.put.some((p) => p.path === 'x/a.mkv'), 'the others go on');
    assert.equal(window.__uploadBusy, false);

    // A readable part with no answer is the network: paused and sent again.
    let n = 0;
    const net = rig(t, {
        put: () => (++n === 1 ? { status: 0, text: '' } : ok201),
        readable: async () => true,
    });
    await net.run([asFile('a')]);
    assert.equal(net.calls.put.length, 2);
    assert.equal(net.calls.wait.length, 1);
    assert.equal(failed(net), undefined);
});

test('the log says it is temporary from the start', async (t) => {
    const base = 'upload.state.temporary';
    const withHint = 'upload.state.temporary upload.state.saveHint';
    // watch records the note at the first token and at every PUT.
    const watch = (t, sc = {}) => {
        const seen = [];
        let r;
        const note = () => r.host.querySelector('pre.info-summary')?.textContent ?? null;
        const count = () => r.host.querySelectorAll('pre.info-summary').length;
        const token = sc.token || ((b, n) => grant(n));
        const put = sc.put || (() => ok201);
        r = rig(t, {
            token: (body, n) => { if (n === 1) seen.push(note()); return token(body, n); },
            put: (p, onProgress, n) => { seen.push(note()); return put(p, onProgress, n); },
        });
        return { r, seen, note, count };
    };
    await t.test('a drop on the home page', async (t) => {
        const w = watch(t);
        await w.r.run([asFile('a')]);
        assert.deepEqual(w.seen, [base, base]);
        // The verdict takes its place.
        assert.equal(w.count(), 0);
        assert.match(w.r.status(), /upload\.done/);
    });
    await t.test('Vault set up', async (t) => {
        const w = watch(t);
        await w.r.run([asFile('a')], { vault: true });
        assert.deepEqual(w.seen, [withHint, withHint]);
    });
    // The page says it under its header already.
    await t.test("not on the upload's page", async (t) => {
        const w = watch(t);
        await w.r.run([asFile('a')], { id: U0, name: 'Photos', dir: ['Photos'] });
        assert.deepEqual(w.seen, [null, null]);
    });
    await t.test('a stop takes its place', async (t) => {
        const sc = gone410();
        const w = watch(t, sc);
        await w.r.run(sc.items, { vault: true });
        assert.deepEqual(w.seen, [withHint, withHint, withHint]);
        assert.match(w.r.status(), /upload\.unavailable/);
        assert.equal(w.note(), null);
    });
    await t.test('the next run starts over', async (t) => {
        const sc = bad400();
        const w = watch(t, sc);
        await w.r.run(sc.items, { ...sc.opts, vault: true });
        assert.equal(w.count(), 0);
        assert.match(w.r.status(), /upload\.someFailed/);
        const verdicts = [];
        w.r.tr.put = async () => { verdicts.push(w.r.host.querySelectorAll('pre[task-summary]').length, w.count()); return ok201; };
        await w.r.run([asFile('a')], { vault: true });
        assert.deepEqual(verdicts, [0, 1]);
        assert.equal(w.r.host.querySelectorAll('.job-stage').length, 1);
    });
});
