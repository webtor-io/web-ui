import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readAllTracks, readTracks, resolveSubtitleLevel, selectEventData } from './subtitle-telemetry.js';

function makeAttrEl(attrs) {
    return { getAttribute: (n) => (n in attrs ? attrs[n] : null) };
}

test('none when no tracks', () => {
    assert.deepEqual(resolveSubtitleLevel([], 'en'), {
        level: 'none', hasUiLang: false, count: 0, badge: '', defaultLang: '', needed: true, translated: false,
    });
});

test('best level wins and ui language is detected', () => {
    const tracks = [
        { provider: 'OpenSubtitles', srclang: 'en', source: 'imdb' },
        { provider: 'ExportTag', srclang: 'ru', source: '' },
        { provider: 'OpenSubtitles', srclang: 'pt-BR', source: 'imdb' },
    ];
    assert.deepEqual(resolveSubtitleLevel(tracks, 'pt'), {
        level: '2', hasUiLang: true, count: 3, badge: '', defaultLang: '', needed: true, translated: false,
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
        level: '5', hasUiLang: true, count: 1, badge: 'ai', defaultLang: 'pt', needed: true, translated: true,
    });
});

// defaultLang: which language actually plays -- what `level` (the best on
// the list) cannot say. The English track beside a translation offer is
// read off it (docs/subtitle_translate.md, Telemetry).
//
// Negative controls: without defaultLang in the result the deepEqual tests
// above fail; without the langGuessed branch the untagged stream reads 'en'.
test('defaultLang is the language of the track that plays', () => {
    const en = { provider: 'OpenSubtitles', srclang: 'en', source: 'hash', badge: 'os', isDefault: true };
    const ai = { provider: 'Translated', srclang: 'pt', source: '', badge: 'ai' };
    assert.equal(resolveSubtitleLevel([ai, en], 'pt', { audioLang: 'en' }).defaultLang, 'en', 'English beside the offer');
    assert.equal(resolveSubtitleLevel([ai, { ...en, srclang: 'pt-BR' }], 'pt').defaultLang, 'pt', 'a base language');
    assert.equal(resolveSubtitleLevel([ai, { ...en, isDefault: false }], 'pt').defaultLang, '', 'subtitles off');
    const untagged = { provider: 'MediaProbe', srclang: 'en', badge: 'emb', langGuessed: true, isDefault: true };
    assert.equal(resolveSubtitleLevel([untagged], 'pt').defaultLang, 'und', 'labelled English, never declared');
    assert.equal(resolveSubtitleLevel([{ provider: 'UserSubtitle', srclang: '', badge: 'user', isDefault: true }], 'pt').defaultLang, 'und');
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

test('readTracks queries .subtitle[data-provider], the trait every chip in the row shares', () => {
    const userEl = makeAttrEl({ 'data-id': 'u1', 'data-provider': 'UserSubtitle', 'data-srclang': 'ru', 'data-source': '', 'data-badge': 'user', 'data-rank': '0' });
    const trEl = makeAttrEl({
        'data-id': 'tr-ru', 'data-provider': 'Translated', 'data-srclang': 'ru', 'data-source': '',
        'data-badge': 'ai', 'data-rank': '5', 'data-locked': 'true', 'data-default': 'true',
        'data-saved': 'true', 'data-source-badge': 'os',
    });
    const guessedEl = makeAttrEl({ 'data-id': 'mp-0', 'data-provider': 'MediaProbe', 'data-srclang': 'en', 'data-source': '', 'data-rank': '1', 'data-lang-guessed': 'true' });
    const noneEl = makeAttrEl({ 'data-id': 'none', 'data-provider': 'MediaProbe', 'data-srclang': '', 'data-source': '' });
    const modal = {
        querySelectorAll: (selector) => (selector === '.subtitle[data-provider]' ? [userEl, trEl, guessedEl, noneEl] : []),
    };
    assert.deepEqual(readTracks(modal), [
        { id: 'u1', provider: 'UserSubtitle', srclang: 'ru', source: '', badge: 'user', rank: 0, forced: false, langGuessed: false, locked: false, isDefault: false, saved: false, sourceBadge: '' },
        { id: 'tr-ru', provider: 'Translated', srclang: 'ru', source: '', badge: 'ai', rank: 5, forced: false, langGuessed: false, locked: true, isDefault: true, saved: true, sourceBadge: 'os' },
        { id: 'mp-0', provider: 'MediaProbe', srclang: 'en', source: '', badge: '', rank: 1, forced: false, langGuessed: true, locked: false, isDefault: false, saved: false, sourceBadge: '' },
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

test('readAllTracks keeps the None entry so a saved "no subtitles" choice is visible', () => {
    // readTracks drops "None" because every consumer that ranks or reports
    // tracks treats "no subtitle" as the absence of one. hasSavedDefault is
    // the exception: choosing None IS a choice, and dropping it meant an
    // audio switch turned subtitles back on over an explicit off.
    const noneEl = makeAttrEl({
        'data-id': 'none', 'data-provider': '', 'data-srclang': '', 'data-source': '',
        'data-default': 'true', 'data-saved': 'true',
    });
    const osEl = makeAttrEl({ 'data-id': 'os-1', 'data-provider': 'OpenSubtitles', 'data-srclang': 'en', 'data-source': 'hash', 'data-rank': '3' });
    const modal = {
        querySelectorAll: (selector) => (selector === '.subtitle[data-provider]' ? [noneEl, osEl] : []),
    };
    const all = readAllTracks(modal);
    assert.deepEqual(all.map((t) => t.id), ['none', 'os-1']);
    assert.equal(all[0].saved, true);
    assert.equal(all[0].isDefault, true);
    // readTracks still drops it, for everything that ranks or reports.
    assert.deepEqual(readTracks(modal).map((t) => t.id), ['os-1']);
});

test('readAllTracks tolerates a missing modal', () => {
    assert.deepEqual(readAllTracks(null), []);
});
