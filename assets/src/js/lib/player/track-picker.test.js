import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    MAX_VISIBLE_LANGS,
    applyOffState,
    offStateAfterActivate,
    restoreSavedTranslation,
    toggleDecision,
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

test('groupByLang: preferred first, then active, then count, then server order', () => {
    const chips = [
        C('none', ''),
        C('a', 'en'), C('b', 'en'), C('c', 'en'),
        C('d', 'ru', { isDefault: true }), C('e', 'ru'),
        C('f', 'de'),
        C('g', ''),
    ];
    assert.deepEqual(groupByLang(chips, 'de'), [
        { lang: 'de', count: 1, active: false },
        { lang: 'ru', count: 2, active: true },
        { lang: 'en', count: 3, active: false },
        { lang: 'und', count: 1, active: false },
    ]);
});

// The new tie-break on its own (owner, 2026-09-15): the preferred language
// has one track and the playing one has three, so count, active and the
// server's order all point the other way. Mirrors
// TestSubtitleLangGroupsExpandsPreferredOverTheActiveLanguage.
test('groupByLang: preferred wins over the language that is playing', () => {
    const chips = [C('a', 'en'), C('b', 'en', { isDefault: true }), C('c', 'en'), C('d', 'pt')];
    assert.deepEqual(groupByLang(chips, 'pt').map((g) => g.lang), ['pt', 'en']);
    // ...and the playing language keeps its dot where it sorts.
    assert.equal(groupByLang(chips, 'pt')[1].active, true);
    // A preferred language with no tracks of its own changes nothing: the
    // playing language leads again, as it did before this rule.
    assert.deepEqual(groupByLang(chips, 'de').map((g) => g.lang), ['en', 'pt']);
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

test('expandedLangFor keeps the language the viewer opened, then the preferred one', () => {
    const chips = [C('a', 'en'), C('b', 'ru')];
    assert.equal(expandedLangFor(chips, { preferred: 'de', current: 'en' }), 'en');
    // The current language lost its last track (deleted upload) — fall back.
    assert.equal(expandedLangFor(chips, { preferred: 'ru', current: 'pl' }), 'ru');
    assert.equal(expandedLangFor(chips, { preferred: 'pl', current: 'pl' }), 'en');
    // The viewer's browsing choice still wins over the preferred language
    // on a refresh — but with nothing browsed, preferred beats the track
    // that is playing (owner, 2026-09-15).
    const playing = [C('a', 'en', { isDefault: true }), C('b', 'ru')];
    assert.equal(expandedLangFor(playing, { preferred: 'ru', current: 'en' }), 'en');
    assert.equal(expandedLangFor(playing, { preferred: 'ru' }), 'ru');
    // ...and with no preference it is the playing language, as before.
    assert.equal(expandedLangFor(playing, {}), 'en');
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
    const ops = langRowOps(chips, row, 'fi', 'fi');
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
    if (o.suggested) a['data-suggested'] = 'true';
    // Every chip the server renders carries data-provider (empty for the
    // "None" carrier), and readTracks selects on it — a fixture that leaves
    // it off hides its own tracks from the switch's decision.
    a['data-provider'] = o.ai ? 'Translated' : (o.provider || '');
    if (o.offered) a['data-offered'] = 'true';
    const kids = [el('svg', { class: 'chip-check' })];
    if (o.origin) kids.push(span('chip-origin badge badge-xs font-mono', o.origin));
    // The AI chip carries both of its states in the markup (see
    // stream_video.html): the verb while idle, the track name while playing.
    if (o.offered) kids.push(span('ai-action', 'Translate to ' + (o.name || o.lang)));
    kids.push(span((o.ai ? 'ai-label ' : '') + 'chip-label', o.label || ''));
    const node = el('button', a, kids);
    node.querySelector('.chip-check').hidden = !o.def;
    if (o.offered) {
        node.querySelector('.ai-action').hidden = false;
        node.querySelector('.ai-label').hidden = true;
    }
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
function buildPicker({ tracks = [], row = [], audio = [], preferred = '', moreExpanded = false, off = false, lastId = '' } = {}) {
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

    const toggle = el('input', { type: 'checkbox', id: 'subtitles-toggle', class: 'toggle toggle-soft' });
    toggle.checked = !off;
    // Same shape as the template: the switch leads #subtitle-langs and the
    // chips live in .lang-row beside it, which is what the muted state dims.
    const langRow = el('div', { class: 'lang-row' + (off ? ' picker-off' : '') }, rowEls.concat([more, template]));
    const langs = el('div', { id: 'subtitle-langs', role: 'group' }, [
        el('label', { class: 'flex items-center' }, [toggle]),
        langRow,
    ]);
    const uploads = el('div', { id: 'my-subtitles', class: 'contents' });
    const tracksBox = el('div', { id: 'subtitle-tracks', role: 'radiogroup' }, trackEls.concat([uploads]));
    const hint = el('p', { id: 'subtitle-hint' });
    hint.hidden = true;
    const audioBox = el('div', { id: 'audio-tracks', role: 'radiogroup' }, audio.map((t) => trackChip('audio', t)));
    const attrs = { class: 'modal', 'data-preferred-lang': preferred, 'data-subtitles-off': off ? 'true' : 'false' };
    if (lastId) attrs['data-last-subtitle'] = lastId;
    const container = el('div', attrs, [
        el('span', { id: 'audio-now' }, [span('now-value')]),
        audioBox,
        el('span', { id: 'subtitle-now' }, [span('now-origin'), span('now-value')]),
        langs,
        tracksBox,
        hint,
    ]);
    container.querySelector('#subtitle-now').querySelector('.now-origin').hidden = true;
    return { container, uploads, more, langs, tracksBox };
}

// The "None" item: since the toggle replaced the Off chip it is a hidden
// carrier at the head of the track row, not something the viewer can click.
const offChip = (def = false) => ({ id: 'none', lang: '', label: '', def, hidden: true });
const langsOf = (c) => Array.from(c.querySelector('#subtitle-langs').querySelectorAll('.lang[data-lang]'));
const visibleTracks = (c) => readChips(c).filter((x) => !x.el.hidden).map((x) => x.id);
const countOf = (chip) => chip.querySelector('.lang-count').textContent;

// ---- the DOM half ---------------------------------------------------

// The "None" item is a state carrier, not a control: the toggle on the
// heading is what turns subtitles off, and the filter must never reveal
// the carrier as if it were a chip the viewer can press.
test('the none carrier stays hidden through every language filter', () => {
    const { container } = buildPicker({
        tracks: [offChip(), { id: 'a', lang: 'en', label: 'English' }, { id: 'b', lang: 'ru', label: 'Russian' }],
        row: [{ lang: 'en', count: 1, selected: true }, { lang: 'ru', count: 1 }],
    });
    applyLangFilter(container, 'ru');
    assert.deepEqual(visibleTracks(container), ['b']);
    applyLangFilter(container, 'en');
    assert.deepEqual(visibleTracks(container), ['a']);
    // …including a language nothing is tagged with.
    applyLangFilter(container, 'de');
    assert.deepEqual(visibleTracks(container), []);
});

// A refresh leaves the viewer in the language whose chip is pressed — on a
// first open that is the one the server expanded (the preferred language),
// and the track playing keeps its dot one chip over, hidden tracks and all
// (owner, 2026-09-15).
test('refresh: stays in the pressed language and marks the row', () => {
    const { container } = buildPicker({
        preferred: 'en',
        tracks: [
            offChip(),
            { id: 'a', lang: 'en', name: 'English', flag: '🇬🇧', label: 'English', origin: 'OS' },
            { id: 'b', lang: 'ru', name: 'Russian', flag: '🇷🇺', label: 'Russian', origin: 'EM', def: true },
        ],
        row: [{ lang: 'en', count: 1, selected: true }, { lang: 'ru', count: 1 }],
        audio: [{ id: 'au', lang: 'en', name: 'English', flag: '🇬🇧', label: 'English', def: true }],
    });
    assert.equal(refresh(container), 'en');
    assert.equal(expandedLang(container), 'en');
    assert.deepEqual(visibleTracks(container), ['a']);
    const [en, ru] = langsOf(container);
    assert.equal(ru.querySelector('.lang-dot').hidden, false, 'the playing language keeps the dot');
    assert.equal(en.querySelector('.lang-dot').hidden, true);
    assert.equal(en.classList.contains('lang-chip-active'), true);
    assert.equal(ru.classList.contains('lang-chip-active'), false);
    // …and with nothing pressed yet, the preferred language opens.
    const fresh = buildPicker({
        preferred: 'ru',
        tracks: [offChip(), { id: 'a', lang: 'en', label: 'English', def: true }, { id: 'b', lang: 'ru', label: 'Russian' }],
        row: [{ lang: 'en', count: 1 }, { lang: 'ru', count: 1 }],
    });
    assert.equal(refresh(fresh.container), 'ru');
    // "Now:" comes off the active chips, strings included.
    assert.equal(container.querySelector('#subtitle-now').querySelector('.now-value').textContent, '🇷🇺 Russian');
    assert.equal(container.querySelector('#subtitle-now').querySelector('.now-origin').textContent, 'EM');
    assert.equal(container.querySelector('#audio-now').querySelector('.now-value').textContent, '🇬🇧 English');
});

// syncNow no-ops in the shipped markup (the "Now:" lines were dropped),
// but the fallback it is built on still has to hold: an audio track the
// language name does not describe is named by its label. The subtitle side
// of the same pass is the "None" carrier, which has no label at all — with
// subtitles off there is nothing to summarize.
test('syncNow falls back to the chip label when there is no language name', () => {
    const { container } = buildPicker({
        tracks: [offChip(true), { id: 'a', lang: 'en', name: 'English', label: 'English' }],
        row: [{ lang: 'en', count: 1, selected: true }],
        audio: [{ id: 'au', lang: '', name: '', label: 'Track 4', def: true }],
    });
    syncNow(container);
    assert.equal(container.querySelector('#audio-now').querySelector('.now-value').textContent, 'Track 4');
    const now = container.querySelector('#subtitle-now');
    assert.equal(now.querySelector('.now-value').textContent, '');
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
    assert.deepEqual(visibleTracks(container), ['a', 'us-2']);
    // The clone is inserted before the "+N" button, not after it — and into
    // the chips' own box (.lang-row), not next to the switch.
    const kids = container.querySelector('.lang-row').children;
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
    assert.deepEqual(visibleTracks(container), ['a']);
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

// The chip a viewer reached through "+N" and pressed is theirs: collapsing
// the row again must not hide it. Without this the filter stayed pressed on
// an invisible chip — tracks of a language with no chip on screen, and no
// way back to it short of re-expanding the row.
test('collapsing the row keeps the expanded language chip visible and pressed', () => {
    const langs = ['en', 'de', 'fr', 'es', 'it', 'pl', 'nl', 'cs'];
    const { container, more } = buildPicker({
        tracks: [offChip()].concat(langs.map((l, i) => ({ id: 'x' + i, lang: l, name: l.toUpperCase(), label: l }))),
        row: langs.map((l, i) => ({ lang: l, count: 1, selected: i === 0 })),
    });
    refresh(container);
    const chipOf = (l) => langsOf(container).find((c) => c.getAttribute('data-lang') === l);
    assert.equal(chipOf('cs').hidden, true, 'the 8th language starts collapsed');

    // Open the row and press the last chip — the path the viewer takes.
    assert.equal(toggleLangOverflow(container), true);
    applyLangFilter(container, 'cs');
    assert.equal(chipOf('cs').getAttribute('aria-pressed'), 'true');

    assert.equal(toggleLangOverflow(container), false);
    assert.equal(chipOf('cs').hidden, false, 'the pressed chip survives the collapse');
    assert.equal(chipOf('cs').getAttribute('aria-pressed'), 'true');
    assert.deepEqual(visibleTracks(container), ['x7'], 'its tracks are the ones on screen');
    // Only the languages actually put away are counted: "+2" next to a
    // visible chip would be a lie of exactly the kind "+N" must not tell.
    assert.equal(more.querySelector('.more-count').textContent, '+1');
    assert.deepEqual(
        langsOf(container).filter((c) => c.hidden).map((c) => c.getAttribute('data-lang')),
        ['nl'],
    );
});

// ---- the on/off decision --------------------------------------------
//
// The toggle's whole rule, as a pure function: what to activate and
// whether the choice is the viewer's (persist) or the player's.

const T = (id, extra = {}) => ({ id, srclang: 'de', rank: 3, ...extra });

test('toggleDecision: off activates the none item and persists it', () => {
    assert.deepEqual(
        toggleDecision({ on: false, lastId: 'a', suggestedId: 'b', tracks: [T('a'), T('b')] }),
        { activateId: 'none', persist: true },
    );
});

test('toggleDecision: on restores what was playing before it was switched off', () => {
    assert.deepEqual(
        toggleDecision({ on: true, lastId: 'a', suggestedId: 'b', tracks: [T('a'), T('b')], preferredLang: 'de' }),
        { activateId: 'a', persist: true },
    );
});

test('toggleDecision: on takes the server suggestion when nothing was chosen this session', () => {
    assert.deepEqual(
        toggleDecision({ on: true, lastId: '', suggestedId: 'b', tracks: [T('a'), T('b')], preferredLang: 'de' }),
        { activateId: 'b', persist: true },
    );
});

// Negative control for the `usable` guard: without it a stale
// data-last-subtitle (the upload it named was deleted) or a locked
// suggestion would be activated and leave subtitles "on" with nothing on
// screen.
test('toggleDecision: a vanished last choice and a locked suggestion both fall through to the ladder', () => {
    assert.deepEqual(
        toggleDecision({ on: true, lastId: 'gone', suggestedId: 'b', tracks: [T('a'), T('b', { locked: true })], preferredLang: 'de' }),
        { activateId: 'a', persist: true },
    );
});

test('toggleDecision: the ladder decides when there is neither a last choice nor a suggestion', () => {
    assert.deepEqual(
        toggleDecision({
            on: true,
            tracks: [T('a', { rank: 4 }), T('b', { rank: 1 })],
            audioLang: 'en',
            preferredLang: 'de',
        }),
        { activateId: 'b', persist: true },
    );
});

// A file with nothing activatable in the viewer's language: the toggle has
// nothing to turn on, so it must not PUT anything and must not claim a
// track is playing.
test('toggleDecision: nothing to turn on means nothing is activated', () => {
    assert.deepEqual(
        toggleDecision({ on: true, tracks: [T('a', { locked: true })], preferredLang: 'de' }),
        { activateId: '', persist: false },
    );
});

// ---- the muted state ------------------------------------------------

test('applyOffState mutes both rows, marks the chips disabled and leaves them in place', () => {
    const { container } = buildPicker({
        tracks: [offChip(), { id: 'a', lang: 'de', label: 'German' }, { id: 'b', lang: 'de', label: 'AI', locked: true }],
        row: [{ lang: 'de', count: 2, selected: true }],
    });
    applyOffState(container, true);
    assert.equal(container.getAttribute('data-subtitles-off'), 'true');
    assert.equal(container.querySelector('#subtitles-toggle').checked, false);
    for (const sel of ['.lang-row', '#subtitle-tracks']) {
        assert.ok(container.querySelector(sel).classList.contains('picker-off'), `${sel} is not muted`);
    }
    // ...but never the switch's own container: it is the way back, and a
    // control at half opacity is the one thing that must stay legible.
    assert.equal(container.querySelector('#subtitle-langs').classList.contains('picker-off'), false);
    // Muted, not disabled: the chips stay clickable (clicking one switches
    // subtitles back on), only the ARIA state says they are inert for now.
    assert.deepEqual(readChips(container).map((c) => c.el.getAttribute('aria-disabled')), ['true', 'true', 'true']);
    assert.deepEqual(readChips(container).map((c) => c.el.hidden), [true, false, false]);
});

// Negative control for the locked carve-out: switching subtitles back on
// must not hand a free viewer an activatable AI chip.
test('applyOffState(false) clears the muted state but never unlocks a locked chip', () => {
    const { container } = buildPicker({
        tracks: [offChip(true), { id: 'a', lang: 'de', label: 'German' }, { id: 'b', lang: 'de', label: 'AI', locked: true }],
        row: [{ lang: 'de', count: 2, selected: true }],
        off: true,
    });
    applyOffState(container, true);
    applyOffState(container, false);
    assert.equal(container.getAttribute('data-subtitles-off'), 'false');
    assert.equal(container.querySelector('#subtitles-toggle').checked, true);
    for (const sel of ['.lang-row', '#subtitle-tracks']) {
        assert.equal(container.querySelector(sel).classList.contains('picker-off'), false, `${sel} is still muted`);
    }
    assert.deepEqual(readChips(container).map((c) => c.el.getAttribute('aria-disabled')), [null, null, 'true']);
});

// With subtitles off the "None" item carries data-default, but the row
// must keep pointing at the muted choice: the dot is what tells the viewer
// which language comes back, and the filter must not jump to another one.
test('the row follows the muted choice while subtitles are off', () => {
    const { container } = buildPicker({
        preferred: 'en',
        off: true,
        lastId: 'b',
        tracks: [
            offChip(true),
            { id: 'a', lang: 'en', name: 'English', label: 'English' },
            { id: 'b', lang: 'ru', name: 'Russian', label: 'Russian' },
        ],
        row: [{ lang: 'en', count: 1, selected: true }, { lang: 'ru', count: 1 }],
    });
    // The pressed language stays pressed; the muted choice only keeps the
    // dot, one chip over.
    assert.equal(refresh(container), 'en');
    const [en, ru] = langsOf(container);
    assert.equal(en.querySelector('.lang-dot').hidden, true);
    assert.equal(ru.querySelector('.lang-dot').hidden, false);
});

// Switching off activates the "None" item, and markTrack clears every
// other chip's mark on its way through. The block is muted, not emptied:
// the choice that comes back has to stay visibly the choice.
test('the muted choice keeps its active mark while subtitles are off', () => {
    const { container } = buildPicker({
        tracks: [offChip(true), { id: 'a', lang: 'de', label: 'German' }, { id: 'b', lang: 'de', label: 'Other' }],
        row: [{ lang: 'de', count: 2, selected: true }],
        off: true,
        lastId: 'a',
    });
    // As markTrack leaves it: nothing marked but the carrier.
    for (const c of readChips(container)) setChipActive(c.el, c.id === 'none');
    applyOffState(container, true);
    assert.deepEqual(
        readChips(container).map((c) => c.el.classList.contains('track-chip-active')),
        [false, true, false],
    );
    assert.equal(container.querySelector('[data-id="a"]').querySelector('.chip-check').hidden, false);
});

// ---- the switch follows what is playing ------------------------------
//
// The invariant is "off == the none item is the active one", and it is
// written in exactly one place (markTrack in Player.jsx) so that the
// activations the player performs for the viewer — a deleted upload
// landing on none, the audio-switch re-pick returning none, an upload
// selected while the switch was off — move the switch with them.

test('offStateAfterActivate: activating none switches off and remembers the outgoing track', () => {
    assert.deepEqual(offStateAfterActivate('a', 'none', ''), { off: true, lastId: 'a' });
    // ...and replaces an older memory: what comes back is what was just
    // taken away, not what the viewer chose two switches ago.
    assert.deepEqual(offStateAfterActivate('b', 'none', 'a'), { off: true, lastId: 'b' });
});

test('offStateAfterActivate: activating a track switches on and keeps the memory', () => {
    assert.deepEqual(offStateAfterActivate('none', 'a', 'b'), { off: false, lastId: 'b' });
    assert.deepEqual(offStateAfterActivate('a', 'b', ''), { off: false, lastId: '' });
});

// Negative control for the "outgoing default only" guard: nothing was
// playing (the chip was deleted with its file, or the page opened off), so
// there is nothing to remember and an older memory must go too — a
// data-last-subtitle naming a chip that is gone would resurrect it.
test('offStateAfterActivate: nothing was playing, so nothing is remembered', () => {
    assert.deepEqual(offStateAfterActivate('', 'none', 'a'), { off: true, lastId: '' });
    assert.deepEqual(offStateAfterActivate('none', 'none', 'a'), { off: true, lastId: '' });
});

// The mirror of SubtitleLangGroups' suggested clause: with subtitles off
// and no group in the preferred language, the row opens where the track
// the switch would turn on lives, not on the biggest group. On the client
// that falls out of readChips reading the muted choice as the active one —
// one mechanism, not a second ordering rule — and it follows
// data-last-subtitle when the viewer switched off in this session, which
// the server cannot know.
test('the row opens on the suggested language when the preferred one has no tracks', () => {
    const { container } = buildPicker({
        preferred: 'pt',
        off: true,
        tracks: [
            offChip(true),
            { id: 'a', lang: 'en', name: 'English', label: 'a' },
            { id: 'b', lang: 'en', name: 'English', label: 'b' },
            { id: 'c', lang: 'en', name: 'English', label: 'c' },
            { id: 'd', lang: 'ru', name: 'Russian', label: 'mine.ru.srt', suggested: true },
        ],
        // Nothing pressed yet: a first open, before the viewer filtered.
        row: [{ lang: 'en', count: 3 }, { lang: 'ru', count: 1 }],
    });
    assert.equal(refresh(container), 'ru');
    // The client never reorders the row — that is the server's job
    // (SubtitleLangGroups), and syncLangRow only updates the chips in
    // place. What it does move is the pressed state and the filter.
    const [en, ru] = langsOf(container);
    assert.equal(ru.getAttribute('aria-pressed'), 'true');
    assert.equal(en.getAttribute('aria-pressed'), 'false');
    assert.deepEqual(visibleTracks(container), ['d']);

    // ...and a within-session memory wins over the server's suggestion:
    // what comes back is what the viewer had, not what the ladder guessed.
    const { container: c2 } = buildPicker({
        preferred: 'pt',
        off: true,
        lastId: 'a',
        tracks: [
            offChip(true),
            { id: 'a', lang: 'en', name: 'English', label: 'a' },
            { id: 'd', lang: 'ru', name: 'Russian', label: 'mine.ru.srt', suggested: true },
        ],
        row: [{ lang: 'en', count: 1 }, { lang: 'ru', count: 1 }],
    });
    assert.equal(refresh(c2), 'en');
});

// The "None" carrier is hidden, has no label and is aria-hidden: marking it
// would leave an invisible chip claiming to be the chosen one, and an
// aria-checked radio in a group where nothing looks checked. data-default
// still moves to it — that is the state — but the look never does.
test('setChipActive never marks the none carrier', () => {
    const { container } = buildPicker({
        tracks: [offChip(), { id: 'a', lang: 'de', label: 'German' }],
        row: [{ lang: 'de', count: 1, selected: true }],
    });
    const carrier = container.querySelector('[data-id="none"]');
    setChipActive(carrier, true);
    assert.equal(carrier.classList.contains('track-chip-active'), false);
    assert.equal(carrier.getAttribute('aria-checked'), 'false');
    assert.equal(carrier.querySelector('.chip-check').hidden, true);
    // ...and a real chip still takes it, so the guard is not a blanket "no".
    const chip = container.querySelector('[data-id="a"]');
    setChipActive(chip, true);
    assert.equal(chip.classList.contains('track-chip-active'), true);
});

// ---- a translation is never started for the viewer -------------------
//
// Owner, 2026-09-16: an AI translation costs tokens, so it takes an
// explicit click. The switch may restore one only when the viewer already
// ran it in this session (data-last-subtitle — cached, hence free).

const AI = (id, extra = {}) => ({ id, provider: 'Translated', srclang: 'de', rank: 5, ...extra });

test('toggleDecision: the switch never starts a translation it was only offered', () => {
    // The server's offer is the AI item and nothing else is activatable.
    assert.deepEqual(
        toggleDecision({ on: true, suggestedId: 'tr-de', tracks: [AI('tr-de')], preferredLang: 'de' }),
        { activateId: '', persist: false },
    );
    // ...and it is not reached through the ladder rule either.
    assert.deepEqual(
        toggleDecision({ on: true, tracks: [AI('tr-de')], audioLang: 'ja', preferredLang: 'de' }),
        { activateId: '', persist: false },
    );
});

test('toggleDecision: a human track wins over an offered translation', () => {
    const tracks = [AI('tr-de'), { id: 'a', provider: 'OpenSubtitles', srclang: 'de', rank: 3 }];
    assert.deepEqual(
        toggleDecision({ on: true, suggestedId: 'tr-de', tracks, preferredLang: 'de' }),
        { activateId: 'a', persist: true },
    );
});

test('toggleDecision: the viewer’s own earlier translation does come back', () => {
    // Already translated this session, so restoring it costs nothing.
    assert.deepEqual(
        toggleDecision({ on: true, lastId: 'tr-de', tracks: [AI('tr-de')], preferredLang: 'de' }),
        { activateId: 'tr-de', persist: true },
    );
});

test('the AI chip is a verb until it is playing', () => {
    const { container } = buildPicker({
        tracks: [offChip(), { id: 'tr-de', lang: 'de', label: 'German', ai: true, offered: true }],
        row: [{ lang: 'de', count: 1, selected: true }],
    });
    const chip = container.querySelector('[data-id="tr-de"]');
    const shown = (sel) => Array.from(chip.querySelectorAll(sel)).map((n) => !n.hidden);

    setChipActive(chip, false);
    assert.deepEqual(shown('.ai-action'), [true], 'idle: the verb shows');
    assert.deepEqual(shown('.ai-label'), [false], 'idle: the track name is hidden');

    setChipActive(chip, true);
    assert.deepEqual(shown('.ai-action'), [false], 'playing: the verb is hidden');
    assert.deepEqual(shown('.ai-label'), [true], 'playing: the track name shows');
});

test('the hint shows exactly when the only offer is a translation', () => {
    const { container } = buildPicker({
        preferred: 'de',
        off: true,
        tracks: [offChip(true), { id: 'tr-de', lang: 'de', label: 'German', ai: true, offered: true }],
        row: [{ lang: 'de', count: 1, selected: true }],
    });
    applyOffState(container, true);
    assert.equal(container.querySelector('#subtitle-hint').hidden, false);
    // Switched back on (by a click on the chip, say): the hint goes.
    applyOffState(container, false);
    assert.equal(container.querySelector('#subtitle-hint').hidden, true);

    // Negative control for the "only a translation" half: a real track is
    // offered instead, so the switch has something to give and the hint
    // would be a lie.
    const { container: c2 } = buildPicker({
        preferred: 'de',
        off: true,
        tracks: [offChip(true), { id: 'a', lang: 'de', label: 'German', suggested: true }],
        row: [{ lang: 'de', count: 1, selected: true }],
    });
    applyOffState(c2, true);
    assert.equal(c2.querySelector('#subtitle-hint').hidden, true);
});

// An offered translation is not "what comes back": the switch refuses it,
// so the row must not follow it either — no dot, no expansion, nothing
// that says "this one is yours" about a track nothing will activate.
test('the row does not follow an offered translation', () => {
    const { container } = buildPicker({
        preferred: 'de',
        off: true,
        tracks: [
            offChip(true),
            { id: 'a', lang: 'en', name: 'English', label: 'English' },
            { id: 'tr-de', lang: 'de', name: 'German', label: 'German', ai: true, offered: true },
        ],
        row: [{ lang: 'en', count: 1 }, { lang: 'de', count: 1 }],
    });
    refresh(container);
    const [en, de] = langsOf(container);
    assert.equal(de.querySelector('.lang-dot').hidden, true, 'an offer is not a selection');
    assert.equal(en.querySelector('.lang-dot').hidden, true);
});

// ---- a saved translation comes back ----------------------------------

test('restoreSavedTranslation: only a saved, playing, unlocked translation', () => {
    const tr = (extra = {}) => ({ id: 'tr-de', provider: 'Translated', isDefault: true, saved: true, locked: false, ...extra });
    assert.equal(restoreSavedTranslation([tr()]), 'tr-de');
    // Negative control, one clause at a time: the ladder's own pick (not
    // saved) must not start a run, a saved choice that is not what is
    // playing is not this page's state, a locked item cannot run at all,
    // and a human track needs no restoring here — the <track> element and
    // its default attribute already do that.
    assert.equal(restoreSavedTranslation([tr({ saved: false })]), null);
    assert.equal(restoreSavedTranslation([tr({ isDefault: false })]), null);
    assert.equal(restoreSavedTranslation([tr({ locked: true })]), null);
    assert.equal(restoreSavedTranslation([{ id: 'os-1', provider: 'OpenSubtitles', isDefault: true, saved: true }]), null);
    assert.equal(restoreSavedTranslation([]), null);
});

test('the hint follows the switch’s own answer, not the mere presence of an offer', () => {
    // (a) An offer, but a human track is playing: subtitles are on, so
    // there is nothing to explain.
    const { container: on } = buildPicker({
        preferred: 'de',
        tracks: [offChip(), { id: 'a', lang: 'de', label: 'German', def: true }, { id: 'tr-de', lang: 'de', label: 'German', ai: true, offered: true }],
        row: [{ lang: 'de', count: 2, selected: true }],
    });
    applyOffState(on, false);
    assert.equal(on.querySelector('#subtitle-hint').hidden, true);

    // ...and with subtitles off but a real track to restore, the switch
    // would not refuse either.
    const { container: restorable } = buildPicker({
        preferred: 'de',
        off: true,
        tracks: [
            offChip(true),
            { id: 'a', lang: 'de', label: 'German', suggested: true },
            { id: 'tr-de', lang: 'de', label: 'German', ai: true, offered: true },
        ],
        row: [{ lang: 'de', count: 2, selected: true }],
    });
    applyOffState(restorable, true);
    assert.equal(restorable.querySelector('#subtitle-hint').hidden, true);
});

// (b) The viewer ran the translation, then switched subtitles off. The
// offer is spent — the run is cached and comes back through
// data-last-subtitle — so the hint must not resurface.
test('the hint does not come back after the translation has been run', () => {
    const { container } = buildPicker({
        preferred: 'de',
        tracks: [offChip(), { id: 'tr-de', lang: 'de', label: 'German', ai: true, offered: true }],
        row: [{ lang: 'de', count: 1, selected: true }],
    });
    const chip = container.querySelector('[data-id="tr-de"]');
    // As a click leaves it: activated, so no longer on offer.
    setChipActive(chip, true);
    assert.equal(chip.getAttribute('data-offered'), null);
    assert.equal(chip.classList.contains('chip-offered'), false);

    container.setAttribute('data-last-subtitle', 'tr-de');
    applyOffState(container, true);
    assert.equal(container.querySelector('#subtitle-hint').hidden, true);
});
