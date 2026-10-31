import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';

// Node never runs the locale-filter-loader (jsx-hooks.mjs answers locale
// imports with {}), so this runs it by hand on the query Discover's
// i18n.js actually writes.
const require = createRequire(import.meta.url);
const loader = require('../../../../webpack/locale-filter-loader.js');
const en = readFileSync(new URL('../../../../../locales/en.json', import.meta.url), 'utf8');

function filter(query) {
    const out = loader.call({ resourceQuery: query }, en);
    return JSON.parse(out.replace(/^export default /, '').replace(/;$/, ''));
}

test('Discover locale chunk carries discover.* and genre.* keys, nothing else', () => {
    const src = readFileSync(new URL('./i18n.js', import.meta.url), 'utf8');
    const query = src.match(/\.json(\?prefix=[^`]+)`/)[1];
    const keys = Object.keys(filter(query));
    assert.ok(keys.includes('discover.type.movie'));
    assert.ok(keys.includes('genre.scifi'));
    assert.ok(keys.includes('genre.all'));
    assert.deepEqual(keys.filter((k) => !k.startsWith('discover.') && !k.startsWith('genre.')), []);
});

test('locale-filter-loader: a single prefix still keeps only its own keys', () => {
    const keys = Object.keys(filter('?prefix=genre'));
    assert.ok(keys.length > 31);
    assert.ok(keys.every((k) => k.startsWith('genre.')));
});
