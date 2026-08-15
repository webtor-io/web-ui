import test from 'node:test';
import assert from 'node:assert/strict';

globalThis.__SUPPORTED_LOCALES__ = ['en', 'ru'];
globalThis.document = { documentElement: { lang: 'en' } };

const { getStreamPrefs, hasPrefs, resolutionOf, matchesPrefs, noneMatchPrefs } =
    await import('./streamPrefs.js');

function withPrefs(prefs, fn) {
    globalThis.window = { _streamPrefs: prefs };
    try {
        return fn(getStreamPrefs());
    } finally {
        globalThis.window = {};
    }
}

test('preferences come from the page bootstrap, with the language resolved', () => {
    withPrefs({ resolutions: ['1080p', '720p'], language: 'ru' }, (p) => {
        assert.deepEqual(p.resolutions, ['1080p', '720p']);
        assert.equal(p.language, 'ru');
        // The setting is a code; stream titles yield display names.
        assert.equal(p.languageName, 'Russian');
        assert.equal(hasPrefs(p), true);
    });
});

test('an absent bootstrap is no preference at all', () => {
    globalThis.window = {};
    const p = getStreamPrefs();
    assert.deepEqual(p.resolutions, []);
    assert.equal(hasPrefs(p), false);
});

test('resolution labels map onto the vocabulary the profile uses', () => {
    assert.equal(resolutionOf({ labels: ['4K', 'HDR'] }), '4k');
    assert.equal(resolutionOf({ labels: ['2160p'] }), '4k');
    assert.equal(resolutionOf({ labels: ['1080p', 'WEB-DL'] }), '1080p');
    assert.equal(resolutionOf({ labels: ['720p'] }), '720p');
    // Anything unplaceable is "other", which the profile offers as a choice.
    assert.equal(resolutionOf({ labels: ['BluRay'] }), 'other');
    assert.equal(resolutionOf({}), 'other');
});

test('a stream has to satisfy both halves', () => {
    withPrefs({ resolutions: ['1080p'], language: 'ru' }, (p) => {
        assert.equal(matchesPrefs({ labels: ['1080p'] }, ['Russian'], p), true);
        assert.equal(matchesPrefs({ labels: ['4K'] }, ['Russian'], p), false);
        assert.equal(matchesPrefs({ labels: ['1080p'] }, ['English'], p), false);
    });
});

test('no preferences means everything matches', () => {
    withPrefs({}, (p) => {
        assert.equal(matchesPrefs({ labels: ['4K'] }, ['German'], p), true);
        assert.equal(noneMatchPrefs([{ labels: ['4K'] }], [['German']], p), false);
    });
});

// A language the client's table cannot resolve must not silence the whole
// list — the note would then claim nothing matches when nothing was checked.
test('an unresolvable language code is not a filter', () => {
    withPrefs({ language: 'zz' }, (p) => {
        assert.equal(p.languageName, '');
        assert.equal(matchesPrefs({ labels: ['1080p'] }, ['English'], p), true);
    });
});

test('noneMatchPrefs fires only when nothing in the list qualifies', () => {
    withPrefs({ resolutions: ['1080p'], language: 'ru' }, (p) => {
        const parsed = [{ labels: ['4K'] }, { labels: ['720p'] }];
        const langs = [['Russian'], ['Russian']];
        assert.equal(noneMatchPrefs(parsed, langs, p), true);

        // One qualifying stream is enough to keep the note away.
        parsed.push({ labels: ['1080p'] });
        langs.push(['Russian']);
        assert.equal(noneMatchPrefs(parsed, langs, p), false);
    });
});

test('an empty list is not a preference miss — that is the other empty state', () => {
    withPrefs({ resolutions: ['1080p'] }, (p) => {
        assert.equal(noneMatchPrefs([], [], p), false);
    });
});
