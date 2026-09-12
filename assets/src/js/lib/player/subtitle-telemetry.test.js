import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readTracks, resolveSubtitleLevel, selectEventData } from './subtitle-telemetry.js';

function makeAttrEl(attrs) {
    return { getAttribute: (n) => (n in attrs ? attrs[n] : null) };
}

test('none when no tracks', () => {
    assert.deepEqual(resolveSubtitleLevel([], 'en'), { level: 'none', hasUiLang: false, count: 0 });
});

test('best level wins and ui language is detected', () => {
    const tracks = [
        { provider: 'OpenSubtitles', srclang: 'en', source: 'imdb' },
        { provider: 'ExportTag', srclang: 'ru', source: '' },
        { provider: 'OpenSubtitles', srclang: 'pt-BR', source: 'imdb' },
    ];
    assert.deepEqual(resolveSubtitleLevel(tracks, 'pt'), { level: '2', hasUiLang: true, count: 3 });
});

test('hash-matched OpenSubtitles is level 3, imdb is 4', () => {
    assert.equal(resolveSubtitleLevel([{ provider: 'OpenSubtitles', srclang: 'en', source: 'hash' }], 'de').level, '3');
    assert.equal(resolveSubtitleLevel([{ provider: 'OpenSubtitles', srclang: 'en', source: 'imdb' }], 'de').level, '4');
});

test('user subtitles are level 0', () => {
    assert.equal(resolveSubtitleLevel([{ provider: 'UserSubtitle', srclang: '', source: '' }], 'en').level, '0');
});

test('selectEventData reads data attributes', () => {
    const el = {
        getAttribute: (n) => ({ 'data-provider': 'OpenSubtitles', 'data-srclang': 'en', 'data-source': 'hash' })[n] || null,
    };
    assert.deepEqual(selectEventData(el), { provider: 'OpenSubtitles', srclang: 'en', source: 'hash' });
});

test('readTracks queries .subtitle[data-provider], not li.subtitle (user uploads render the marker on a div)', () => {
    const userEl = makeAttrEl({ 'data-id': 'u1', 'data-provider': 'UserSubtitle', 'data-srclang': 'ru', 'data-source': '' });
    const hashEl = makeAttrEl({ 'data-id': 'os1', 'data-provider': 'OpenSubtitles', 'data-srclang': 'en', 'data-source': 'hash' });
    const noneEl = makeAttrEl({ 'data-id': 'none', 'data-provider': 'MediaProbe', 'data-srclang': '', 'data-source': '' });
    const modal = {
        querySelectorAll: (selector) => (selector === '.subtitle[data-provider]' ? [userEl, hashEl, noneEl] : []),
    };
    assert.deepEqual(readTracks(modal), [
        { provider: 'UserSubtitle', srclang: 'ru', source: '' },
        { provider: 'OpenSubtitles', srclang: 'en', source: 'hash' },
    ]);
});

test('readTracks tolerates a missing modal', () => {
    assert.deepEqual(readTracks(null), []);
});
