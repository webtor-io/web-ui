import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    MAX_VISIBLE_LANGS,
    groupByLang,
    activeLang,
    expandedLangFor,
    langRowOps,
    readChips,
    applyLangFilter,
    expandedLang,
    syncLangRow,
    syncNow,
    setChipActive,
    toggleLangOverflow,
    refresh,
} from './track-picker.js';

// ---- the pure half --------------------------------------------------
//
// Same fixture as handlers/action.TestSubtitleLangGroupsOrdersActiveThen-
// PreferredThenCount: the row is rendered by Go and recomputed here after
// an upload, and the two orders must be the same one.

const C = (id, lang, extra = {}) => ({ id, lang, isDefault: false, ...extra });

test('groupByLang: active first, then preferred, then count, then server order', () => {
    const chips = [
        C('none', ''),
        C('a', 'en'), C('b', 'en'), C('c', 'en'),
        C('d', 'ru', { isDefault: true }), C('e', 'ru'),
        C('f', 'de'),
        C('g', ''),
    ];
    assert.deepEqual(groupByLang(chips, 'de'), [
        { lang: 'ru', count: 2, active: true },
        { lang: 'de', count: 1, active: false },
        { lang: 'en', count: 3, active: false },
        { lang: 'und', count: 1, active: false },
    ]);
});

test('groupByLang: preferred first when nothing is active', () => {
    const chips = [C('a', 'en'), C('b', 'en'), C('c', 'de')];
    assert.deepEqual(groupByLang(chips, 'de').map((g) => g.lang), ['de', 'en']);
});

test('groupByLang: an unset preference does not collide with the Unknown group', () => {
    const chips = [C('a', ''), C('b', 'en'), C('c', 'en')];
    assert.deepEqual(groupByLang(chips, '').map((g) => g.lang), ['en', 'und']);
});

// The last tie-break is the order the server rendered, not the language
// name: Go's comparator ends with `return false`, and an alphabetical sort
// here would reshuffle the row on the first refresh after an upload.
test('groupByLang: equal counts keep the server order', () => {
    const chips = ['ru', 'en', 'de'].map((l, i) => C('x' + i, l));
    assert.deepEqual(groupByLang(chips, '').map((g) => g.lang), ['ru', 'en', 'de']);
});

test('groupByLang: regional variants share one chip, untagged tracks are "und"', () => {
    const chips = [C('a', 'pt-BR'), C('b', 'pt_PT'), C('c', 'PT'), C('d', '')];
    assert.deepEqual(groupByLang(chips, ''), [
        { lang: 'pt', count: 3, active: false },
        { lang: 'und', count: 1, active: false },
    ]);
});

test('activeLang is empty when subtitles are off', () => {
    assert.equal(activeLang([C('a', 'en'), C('b', 'ru')]), '');
    assert.equal(activeLang([C('a', 'en'), C('b', 'ru', { isDefault: true })]), 'ru');
    // "Off" is a choice, not a language.
    assert.equal(activeLang([C('none', '', { isDefault: true }), C('a', 'en')]), '');
});

test('expandedLangFor keeps the language the viewer opened when nothing is active', () => {
    const chips = [C('a', 'en'), C('b', 'ru')];
    assert.equal(expandedLangFor(chips, { preferred: 'de', current: 'en' }), 'en');
    // The current language lost its last track (deleted upload) — fall back.
    assert.equal(expandedLangFor(chips, { preferred: 'ru', current: 'pl' }), 'ru');
    assert.equal(expandedLangFor(chips, { preferred: 'pl', current: 'pl' }), 'en');
    // An active track always wins over both.
    assert.equal(expandedLangFor([C('a', 'en'), C('b', 'ru', { isDefault: true })], { preferred: 'en', current: 'en' }), 'ru');
    assert.equal(expandedLangFor([], { preferred: 'en', current: 'en' }), '');
});

test('langRowOps: counts, dot, and the chip a new upload needs', () => {
    const chips = [C('a', 'en'), C('b', 'ru', { isDefault: true }), C('c', 'pl', { langName: 'Polish', langFlag: '🇵🇱' })];
    const row = [{ lang: 'en' }, { lang: 'ru' }];
    const ops = langRowOps(chips, row, 'ru');
    assert.deepEqual(ops.updates, [
        { lang: 'en', count: 1, active: false, selected: false, hidden: false },
        { lang: 'ru', count: 1, active: true, selected: true, hidden: false },
    ]);
    assert.deepEqual(ops.missing, [
        { lang: 'pl', count: 1, active: false, selected: false, hidden: false, name: 'Polish', flag: '🇵🇱' },
    ]);
    assert.equal(ops.overflow, 0);
});

// Deleting the last upload of a language is the one event that can empty a
// chip out from under the viewer: the row still has the chip the server
// rendered, and the tracks container no longer has anything in it.
test('post-delete: the emptied language chip drops to 0 and hides', () => {
    const before = [C('a', 'en'), C('us-1', 'pl', { langName: 'Polish', langFlag: '🇵🇱' })];
    const row = [{ lang: 'en' }, { lang: 'pl' }];
    assert.deepEqual(langRowOps(before, row, 'pl').updates, [
        { lang: 'en', count: 1, active: false, selected: false, hidden: false },
        { lang: 'pl', count: 1, active: false, selected: true, hidden: false },
    ]);
    const after = [C('a', 'en')];
    const ops = langRowOps(after, row, 'pl');
    assert.deepEqual(ops.updates, [
        { lang: 'en', count: 1, active: false, selected: false, hidden: false },
        { lang: 'pl', count: 0, active: false, selected: true, hidden: true },
    ]);
    // An emptied chip is gone, not collapsed: "+1" would promise a chip
    // nothing can bring back.
    assert.equal(ops.overflow, 0);
    // …and the row must not stay expanded on a language with no tracks.
    assert.equal(expandedLangFor(after, { preferred: 'de', current: 'pl' }), 'en');
});

test('langRowOps hides everything past the sixth chip but never the active one', () => {
    assert.equal(MAX_VISIBLE_LANGS, 6, 'must mirror maxVisibleLangChips in handlers/action/picker.go');
    const chips = ['en', 'de', 'fr', 'es', 'it', 'pl'].map((l, i) => C('x' + i, l));
    chips.push(C('y', 'cs', { isDefault: true }));
    const row = ['en', 'de', 'fr', 'es', 'it', 'pl', 'cs'].map((lang) => ({ lang }));
    const ops = langRowOps(chips, row, 'cs');
    assert.deepEqual(ops.updates.filter((u) => u.hidden).map((u) => u.lang), ['pl']);
    assert.equal(ops.overflow, 1);
});

test('langRowOps keeps both the active and the preferred language visible at nine groups', () => {
    const chips = ['en', 'de', 'fr', 'es', 'it', 'pl', 'nl'].map((l, i) => C('x' + i, l));
    chips.push(C('y', 'cs', { isDefault: true }));
    chips.push(C('z', 'fi'));
    const row = ['en', 'de', 'fr', 'es', 'it', 'pl', 'nl', 'cs', 'fi'].map((lang) => ({ lang }));
    const ops = langRowOps(chips, row, 'cs', 'fi');
    assert.deepEqual(ops.updates.filter((u) => u.hidden).map((u) => u.lang), ['it', 'pl', 'nl']);
    assert.equal(ops.overflow, 3);
    const visible = ops.updates.filter((u) => !u.hidden).map((u) => u.lang);
    assert.ok(visible.includes('cs') && visible.includes('fi'), `active+preferred must stay visible: ${visible}`);
});

// ---- a DOM stand-in -------------------------------------------------
//
// Small on purpose: attributes, one class list, hidden, textContent and a
// single-compound-selector querySelector. Everything the module touches and
// nothing else — a fake cleverer than the DOM would start testing itself.

function parseSelector(sel) {
    const out = { id: '', classes: [], attrs: [] };
    const re = /#([\w-]+)|\.([\w-]+)|\[([\w-]+)(?:=["']?([^\]"']*)["']?)?\]/g;
    let m;
    while ((m = re.exec(sel))) {
        if (m[1]) out.id = m[1];
        else if (m[2]) out.classes.push(m[2]);
        else out.attrs.push({ name: m[3], value: m[4] === undefined ? null : m[4] });
    }
    return out;
}

function matches(node, s) {
    if (s.id && node.getAttribute('id') !== s.id) return false;
    for (const c of s.classes) if (!node.classList.contains(c)) return false;
    for (const a of s.attrs) {
        const v = node.getAttribute(a.name);
        if (v === null) return false;
        if (a.value !== null && v !== a.value) return false;
    }
    return true;
}

function el(tag, attrs = {}, children = []) {
    const a = { ...attrs };
    const text = a.text || '';
    delete a.text;
    const cls = () => String(a.class || '').split(/\s+/).filter(Boolean);
    const setCls = (list) => { a.class = list.join(' '); };
    const node = {
        tag,
        children: children.slice(),
        hidden: false,
        textContent: text,
        classList: {
            contains: (c) => cls().includes(c),
            add(c) { if (!cls().includes(c)) setCls(cls().concat(c)); },
            remove(c) { setCls(cls().filter((x) => x !== c)); },
            toggle(c, on) {
                const want = on === undefined ? !cls().includes(c) : !!on;
                if (want) this.add(c); else this.remove(c);
                return want;
            },
        },
        getAttribute: (n) => (n in a ? String(a[n]) : null),
        setAttribute: (n, v) => { a[n] = String(v); },
        removeAttribute: (n) => { delete a[n]; },
        append(child) { node.children.push(child); return child; },
        insertBefore(child, ref) {
            const i = ref ? node.children.indexOf(ref) : -1;
            if (i < 0) node.children.push(child); else node.children.splice(i, 0, child);
            return child;
        },
        querySelectorAll(sel) {
            const s = parseSelector(sel);
            const out = [];
            const walk = (n) => {
                for (const c of n.children) {
                    if (matches(c, s)) out.push(c);
                    walk(c);
                }
            };
            walk(node);
            return out;
        },
        querySelector(sel) { return node.querySelectorAll(sel)[0] || null; },
        cloneNode() {
            return el(tag, { ...a, text: node.textContent }, node.children.map((c) => c.cloneNode(true)));
        },
    };
    return node;
}

const span = (cls, text = '') => el('span', { class: cls, text });

// A subtitle/audio chip as templates/partials/action/stream_video.html
// renders it: the check icon, the origin badge, the label.
function trackChip(kind, o) {
    const a = {
        class: `${kind} track-chip${o.def ? ' track-chip-active' : ''}${o.locked ? ' chip-locked' : ''}`,
        'data-id': o.id,
        'data-lang': o.lang === undefined ? '' : o.lang,
        'data-lang-name': o.name || '',
        'data-lang-flag': o.flag || '',
        'data-label': o.label || '',
        'aria-checked': o.def ? 'true' : 'false',
    };
    if (o.locked) a['data-locked'] = 'true';
    if (o.def) a['data-default'] = 'true';
    const kids = [el('svg', { class: 'chip-check' })];
    if (o.origin) kids.push(span('chip-origin badge badge-xs font-mono', o.origin));
    kids.push(span('chip-label', o.label || ''));
    const node = el('button', a, kids);
    node.querySelector('.chip-check').hidden = !o.def;
    node.hidden = !!o.hidden;
    return node;
}

function langChip(o) {
    const node = el('button', {
        class: `lang lang-chip${o.selected ? ' lang-chip-active' : ''}`,
        'data-lang': o.lang,
        'aria-pressed': o.selected ? 'true' : 'false',
    }, [
        span('chip-flag', o.flag || ''),
        span('lang-name', o.name || o.lang.toUpperCase()),
        span('lang-count', String(o.count === undefined ? 0 : o.count)),
        span('lang-dot'),
    ]);
    node.querySelector('.lang-dot').hidden = !o.active;
    node.hidden = !!o.hidden;
    return node;
}

// buildPicker renders the dialog the way the server would, from an explicit
// spec: the row is given, not derived, so a test can hand the module a stale
// row (exactly what an upload or a delete leaves behind) and check what it
// makes of it.
function buildPicker({ tracks = [], row = [], audio = [], preferred = '', moreExpanded = false } = {}) {
    const trackEls = tracks.map((t) => trackChip('subtitle', t));
    const rowEls = row.map(langChip);
    const more = el('button', {
        id: 'subtitle-lang-more',
        class: 'lang-chip',
        'aria-expanded': moreExpanded ? 'true' : 'false',
        'aria-label': 'More languages',
    }, [span('more-count', '+0')]);
    const template = el('template', { id: 'lang-chip-template' });
    template.content = { firstElementChild: langChip({ lang: '', count: 0 }) };

    const langs = el('div', { id: 'subtitle-langs', role: 'group' }, rowEls.concat([more, template]));
    const uploads = el('div', { id: 'my-subtitles', class: 'contents' });
    const tracksBox = el('div', { id: 'subtitle-tracks', role: 'radiogroup' }, trackEls.concat([uploads]));
    const audioBox = el('div', { id: 'audio-tracks', role: 'radiogroup' }, audio.map((t) => trackChip('audio', t)));
    const container = el('div', { class: 'modal', 'data-preferred-lang': preferred }, [
        el('span', { id: 'audio-now' }, [span('now-value')]),
        audioBox,
        el('span', { id: 'subtitle-now' }, [span('now-origin'), span('now-value')]),
        langs,
        tracksBox,
    ]);
    container.querySelector('#subtitle-now').querySelector('.now-origin').hidden = true;
    return { container, uploads, more, langs, tracksBox };
}

const offChip = (def = false) => ({ id: 'none', lang: '', label: 'Off', def });
const langsOf = (c) => Array.from(c.querySelector('#subtitle-langs').querySelectorAll('.lang[data-lang]'));
const visibleTracks = (c) => readChips(c).filter((x) => !x.el.hidden).map((x) => x.id);
const countOf = (chip) => chip.querySelector('.lang-count').textContent;

// ---- the DOM half ---------------------------------------------------

test('the Off chip is never hidden by the language filter', () => {
    const { container } = buildPicker({
        tracks: [offChip(), { id: 'a', lang: 'en', label: 'English' }, { id: 'b', lang: 'ru', label: 'Russian' }],
        row: [{ lang: 'en', count: 1, selected: true }, { lang: 'ru', count: 1 }],
    });
    applyLangFilter(container, 'ru');
    assert.deepEqual(visibleTracks(container), ['none', 'b']);
    applyLangFilter(container, 'en');
    assert.deepEqual(visibleTracks(container), ['none', 'a']);
    // …including a language nothing is tagged with.
    applyLangFilter(container, 'de');
    assert.deepEqual(visibleTracks(container), ['none']);
});

test('refresh: opens on the active track language and marks the row', () => {
    const { container } = buildPicker({
        preferred: 'de',
        tracks: [
            offChip(),
            { id: 'a', lang: 'en', name: 'English', flag: '🇬🇧', label: 'English', origin: 'OS' },
            { id: 'b', lang: 'ru', name: 'Russian', flag: '🇷🇺', label: 'Russian', origin: 'EM', def: true },
        ],
        row: [{ lang: 'en', count: 1, selected: true }, { lang: 'ru', count: 1 }],
        audio: [{ id: 'au', lang: 'en', name: 'English', flag: '🇬🇧', label: 'English', def: true }],
    });
    assert.equal(refresh(container), 'ru');
    assert.equal(expandedLang(container), 'ru');
    assert.deepEqual(visibleTracks(container), ['none', 'b']);
    const [en, ru] = langsOf(container);
    assert.equal(ru.querySelector('.lang-dot').hidden, false, 'the active language carries the dot');
    assert.equal(en.querySelector('.lang-dot').hidden, true);
    assert.equal(ru.classList.contains('lang-chip-active'), true);
    assert.equal(en.classList.contains('lang-chip-active'), false);
    // "Now:" comes off the active chips, strings included.
    assert.equal(container.querySelector('#subtitle-now').querySelector('.now-value').textContent, '🇷🇺 Russian');
    assert.equal(container.querySelector('#subtitle-now').querySelector('.now-origin').textContent, 'EM');
    assert.equal(container.querySelector('#audio-now').querySelector('.now-value').textContent, '🇬🇧 English');
});

test('syncNow falls back to the chip label when there is no language name', () => {
    const { container } = buildPicker({
        tracks: [offChip(true), { id: 'a', lang: 'en', name: 'English', label: 'English' }],
        row: [{ lang: 'en', count: 1, selected: true }],
    });
    syncNow(container);
    const now = container.querySelector('#subtitle-now');
    assert.equal(now.querySelector('.now-value').textContent, 'Off');
    assert.equal(now.querySelector('.now-origin').textContent, '');
    assert.equal(now.querySelector('.now-origin').hidden, true);
});

test('post-upload: a new language gets a chip cloned from the template, counts follow', () => {
    const { container, uploads } = buildPicker({
        preferred: 'de',
        tracks: [offChip(), { id: 'a', lang: 'en', name: 'English', label: 'English' }],
        row: [{ lang: 'en', count: 1, selected: true }],
    });
    refresh(container);
    assert.equal(expandedLang(container), 'en');

    // The uploads partial is swapped in with one MY chip in a language the
    // server never rendered a chip for.
    uploads.append(trackChip('subtitle', { id: 'us-1', lang: 'pl', name: 'Polish', flag: '🇵🇱', label: 'pl.srt', origin: 'MY' }));
    uploads.append(trackChip('subtitle', { id: 'us-2', lang: 'en', name: 'English', label: 'en.srt', origin: 'MY' }));
    refresh(container, { current: expandedLang(container) });

    const chips = langsOf(container);
    assert.deepEqual(chips.map((c) => c.getAttribute('data-lang')), ['en', 'pl']);
    assert.deepEqual(chips.map(countOf), ['2', '1']);
    assert.equal(chips[1].querySelector('.lang-name').textContent, 'Polish');
    assert.equal(chips[1].querySelector('.chip-flag').textContent, '🇵🇱');
    assert.equal(chips[1].hidden, false);
    // The viewer was looking at English and stays there.
    assert.equal(expandedLang(container), 'en');
    assert.deepEqual(visibleTracks(container), ['none', 'a', 'us-2']);
    // The clone is inserted before the "+N" button, not after it.
    const kids = container.querySelector('#subtitle-langs').children;
    assert.ok(kids.indexOf(chips[1]) < kids.findIndex((k) => k.getAttribute('id') === 'subtitle-lang-more'));
});

test('post-delete: the emptied chip drops to 0, hides, and the row falls back', () => {
    const { container, uploads } = buildPicker({
        preferred: 'de',
        tracks: [offChip(), { id: 'a', lang: 'en', name: 'English', label: 'English' }],
        row: [{ lang: 'en', count: 1 }, { lang: 'pl', count: 1, name: 'Polish', selected: true }],
    });
    uploads.append(trackChip('subtitle', { id: 'us-1', lang: 'pl', name: 'Polish', label: 'pl.srt', origin: 'MY' }));
    refresh(container, { current: 'pl' });
    assert.deepEqual(langsOf(container).map(countOf), ['1', '1']);

    // The delete comes back as an innerHTML swap of #my-subtitles: the chip
    // is gone, the language row still carries its count.
    uploads.children.length = 0;
    refresh(container, { current: expandedLang(container) });

    const [en, pl] = langsOf(container);
    assert.equal(countOf(en), '1');
    assert.equal(countOf(pl), '0');
    assert.equal(en.hidden, false);
    assert.equal(pl.hidden, true, 'a language with no tracks left has no chip');
    assert.equal(expandedLang(container), 'en');
    assert.deepEqual(visibleTracks(container), ['none', 'a']);
});

test('syncLangRow alone rewrites counts without moving the viewer', () => {
    const { container, uploads } = buildPicker({
        tracks: [offChip(), { id: 'a', lang: 'en', name: 'English', label: 'English' }],
        row: [{ lang: 'en', count: 1, selected: true }, { lang: 'ru', count: 3, name: 'Russian' }],
    });
    uploads.append(trackChip('subtitle', { id: 'us-1', lang: 'en', label: 'en.srt', origin: 'MY' }));
    syncLangRow(container);
    assert.deepEqual(langsOf(container).map(countOf), ['2', '0']);
    assert.equal(expandedLang(container), 'en');
});

test('setChipActive moves the active mark, leaving exactly one per group', () => {
    const { container } = buildPicker({
        tracks: [
            offChip(),
            { id: 'a', lang: 'en', label: 'English', def: true },
            { id: 'b', lang: 'en', label: 'English 2' },
            { id: 'ai', lang: 'en', label: 'AI', locked: true },
        ],
        row: [{ lang: 'en', count: 3, selected: true }],
    });
    const chips = readChips(container);
    const active = () => chips.filter((c) => c.el.classList.contains('track-chip-active')).map((c) => c.id);
    assert.deepEqual(active(), ['a']);

    for (const c of chips) setChipActive(c.el, c.id === 'b');
    assert.deepEqual(active(), ['b']);
    assert.equal(chips[2].el.getAttribute('aria-checked'), 'true');
    assert.equal(chips[2].el.querySelector('.chip-check').hidden, false);
    assert.equal(chips[1].el.getAttribute('aria-checked'), 'false');
    assert.equal(chips[1].el.querySelector('.chip-check').hidden, true);

    // A locked chip can never take the mark, whatever the caller asks for.
    setChipActive(chips[3].el, true);
    assert.deepEqual(active(), ['b']);
    assert.equal(chips[3].el.getAttribute('aria-checked'), 'false');
});

test('"+N" collapses the tail, toggles the whole row open, and comes back', () => {
    const langs = ['en', 'de', 'fr', 'es', 'it', 'pl', 'nl', 'cs'];
    const { container, more } = buildPicker({
        tracks: [offChip()].concat(langs.map((l, i) => ({ id: 'x' + i, lang: l, name: l.toUpperCase(), label: l }))),
        row: langs.map((l, i) => ({ lang: l, count: 1, selected: i === 0 })),
    });
    refresh(container);
    const hidden = () => langsOf(container).filter((c) => c.hidden).map((c) => c.getAttribute('data-lang'));
    assert.deepEqual(hidden(), ['nl', 'cs']);
    assert.equal(more.hidden, false);
    assert.equal(more.querySelector('.more-count').textContent, '+2');

    assert.equal(toggleLangOverflow(container), true);
    assert.deepEqual(hidden(), []);
    assert.equal(more.hidden, false, 'the disclosure stays as the way back');
    assert.equal(more.querySelector('.more-count').textContent, '×');
    assert.equal(more.getAttribute('aria-label'), 'More languages', 'the localized label is never rewritten');

    assert.equal(toggleLangOverflow(container), false);
    assert.deepEqual(hidden(), ['nl', 'cs']);
    assert.equal(more.querySelector('.more-count').textContent, '+2');
});

test('"+N" hides itself when nothing is collapsed', () => {
    const { container, more } = buildPicker({
        tracks: [offChip(), { id: 'a', lang: 'en', label: 'English' }],
        row: [{ lang: 'en', count: 1, selected: true }],
    });
    refresh(container);
    assert.equal(more.hidden, true);
});
