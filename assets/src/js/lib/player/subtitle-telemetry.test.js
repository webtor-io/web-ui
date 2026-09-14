import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readTracks, resolveSubtitleLevel, selectEventData } from './subtitle-telemetry.js';

function makeAttrEl(attrs) {
    return { getAttribute: (n) => (n in attrs ? attrs[n] : null) };
}

test('none when no tracks', () => {
    assert.deepEqual(resolveSubtitleLevel([], 'en'), {
        level: 'none', hasUiLang: false, count: 0, badge: '', needed: true, translated: false,
    });
});

test('best level wins and ui language is detected', () => {
    const tracks = [
        { provider: 'OpenSubtitles', srclang: 'en', source: 'imdb' },
        { provider: 'ExportTag', srclang: 'ru', source: '' },
        { provider: 'OpenSubtitles', srclang: 'pt-BR', source: 'imdb' },
    ];
    assert.deepEqual(resolveSubtitleLevel(tracks, 'pt'), {
        level: '2', hasUiLang: true, count: 3, badge: '', needed: true, translated: false,
    });
});

test('hash-matched OpenSubtitles is level 3, imdb is 4', () => {
    assert.equal(resolveSubtitleLevel([{ provider: 'OpenSubtitles', srclang: 'en', source: 'hash' }], 'de').level, '3');
    assert.equal(resolveSubtitleLevel([{ provider: 'OpenSubtitles', srclang: 'en', source: 'imdb' }], 'de').level, '4');
});

test('user subtitles are level 0', () => {
    assert.equal(resolveSubtitleLevel([{ provider: 'UserSubtitle', srclang: '', source: '' }], 'en').level, '0');
});

test('translated is level 5 and badge/needed are reported', () => {
    const tracks = [{ provider: 'Translated', srclang: 'pt', source: '', badge: 'ai', isDefault: true }];
    assert.deepEqual(resolveSubtitleLevel(tracks, 'pt', { audioLang: 'en' }), {
        level: '5', hasUiLang: true, count: 1, badge: 'ai', needed: true, translated: true,
    });
});

test('audio already in the UI language: subtitles are not needed', () => {
    assert.equal(resolveSubtitleLevel([], 'pt', { audioLang: 'pt' }).needed, false);
    assert.equal(resolveSubtitleLevel([], 'pt', { audioLang: 'pt-BR' }).needed, false);
});

test('needed is measured against the preferred content language, not the UI', () => {
    // A German UI reading Portuguese subtitles: German audio still needs
    // them, Portuguese audio does not. Asking the UI language instead
    // would invert both answers.
    assert.equal(resolveSubtitleLevel([], 'de', { audioLang: 'pt', preferredLang: 'pt' }).needed, false);
    assert.equal(resolveSubtitleLevel([], 'de', { audioLang: 'de', preferredLang: 'pt' }).needed, true);
    // No preference configured ⇒ the UI language stands in.
    assert.equal(resolveSubtitleLevel([], 'de', { audioLang: 'de', preferredLang: '' }).needed, false);
});

test('selectEventData reads data attributes', () => {
    const el = makeAttrEl({
        'data-provider': 'OpenSubtitles', 'data-srclang': 'en', 'data-source': 'hash', 'data-badge': 'os',
    });
    assert.deepEqual(selectEventData(el), { provider: 'OpenSubtitles', srclang: 'en', source: 'hash', badge: 'os' });
});

test('readTracks queries .subtitle[data-provider], not li.subtitle (user uploads render the marker on a div)', () => {
    const userEl = makeAttrEl({ 'data-id': 'u1', 'data-provider': 'UserSubtitle', 'data-srclang': 'ru', 'data-source': '', 'data-badge': 'user', 'data-rank': '0' });
    const trEl = makeAttrEl({
        'data-id': 'tr-ru', 'data-provider': 'Translated', 'data-srclang': 'ru', 'data-source': '',
        'data-badge': 'ai', 'data-rank': '5', 'data-locked': 'true', 'data-default': 'true',
        'data-saved': 'true', 'data-source-badge': 'os',
    });
    const noneEl = makeAttrEl({ 'data-id': 'none', 'data-provider': 'MediaProbe', 'data-srclang': '', 'data-source': '' });
    const modal = {
        querySelectorAll: (selector) => (selector === '.subtitle[data-provider]' ? [userEl, trEl, noneEl] : []),
    };
    assert.deepEqual(readTracks(modal), [
        { id: 'u1', provider: 'UserSubtitle', srclang: 'ru', source: '', badge: 'user', rank: 0, forced: false, locked: false, isDefault: false, saved: false, sourceBadge: '' },
        { id: 'tr-ru', provider: 'Translated', srclang: 'ru', source: '', badge: 'ai', rank: 5, forced: false, locked: true, isDefault: true, saved: true, sourceBadge: 'os' },
    ]);
});

test('readTracks defaults a missing rank to last', () => {
    const el = makeAttrEl({ 'data-id': 'x', 'data-provider': 'External', 'data-srclang': 'en' });
    const modal = { querySelectorAll: () => [el] };
    assert.equal(readTracks(modal)[0].rank, 9);
});

test('readTracks tolerates a missing modal', () => {
    assert.deepEqual(readTracks(null), []);
});
