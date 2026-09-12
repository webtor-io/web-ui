import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolveSubtitleLevel, selectEventData } from './subtitle-telemetry.js';

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
