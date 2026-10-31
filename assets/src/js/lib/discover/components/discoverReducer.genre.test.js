import test from 'node:test';
import assert from 'node:assert/strict';

import { discoverReducer, initialState, buildCatalogs } from './discoverReducer.js';

// The genre slice: every action that picks a catalog also picks its genre
// value (genres.js genreFor), and SELECT_GENRE validates and dedupes.

const catalogs = buildCatalogs([{
    baseUrl: 'https://meta.test', status: 'ok',
    manifest: { name: 'meta', catalogs: [
        { type: 'movie', id: 'top', extra: [{ name: 'genre', options: ['Action', 'Drama'] }] },
        { type: 'movie', id: 'year', extra: [{ name: 'genre', isRequired: true, options: ['2026', '2025'] }] },
        { type: 'series', id: 'year', extra: [{ name: 'genre', isRequired: true, options: ['2026'] }] },
        { type: 'movie', id: 'plain' },
    ] },
}]);
const [top, year, , plain] = catalogs;
const ready = (selectedCatalog, selectedGenre) => discoverReducer(initialState, {
    type: 'INIT_SUCCESS', catalogs, selectedType: selectedCatalog.type, selectedCatalog, selectedGenre, addons: [],
});

test('buildCatalogs reads each catalog\'s filter', () => {
    assert.deepEqual(catalogs.map((c) => c.filter?.mode ?? null), ['genre', 'year', 'year', null]);
});

test('INIT_SUCCESS takes a value from the link only when the catalog offers it', () => {
    assert.equal(ready(top, 'Drama').selectedGenre, 'Drama');
    assert.equal(ready(top, 'Bogus').selectedGenre, null);
    assert.equal(ready(year, '2025').selectedGenre, '2025');
    assert.equal(ready(year, null).selectedGenre, '2026');
    assert.equal(ready(plain, 'Drama').selectedGenre, null);
});

test('SELECT_GENRE resets the listing; the value already shown is no change', () => {
    const s = { ...ready(top, null), items: [{ id: 'tt1' }], skip: 1, page: 2, hasMore: false };
    const next = discoverReducer(s, { type: 'SELECT_GENRE', genre: 'Action' });
    assert.equal(next.selectedGenre, 'Action');
    assert.deepEqual([next.items, next.skip, next.page, next.hasMore], [[], 0, 0, true]);
    assert.equal(discoverReducer(next, { type: 'SELECT_GENRE', genre: 'Action' }), next);
    assert.equal(discoverReducer(s, { type: 'SELECT_GENRE', genre: 'Bogus' }), s, 'unknown → all genres, already shown');
    const y = ready(year, '2025');
    assert.equal(discoverReducer(y, { type: 'SELECT_GENRE', genre: null }).selectedGenre, '2026', 'a year catalog has no "all"');
});

test('another catalog, type or the way out of search picks that catalog\'s default', () => {
    const s = discoverReducer(ready(top, null), { type: 'SELECT_GENRE', genre: 'Action' });
    assert.equal(discoverReducer(s, { type: 'SELECT_CATALOG', catalog: year }).selectedGenre, '2026');
    assert.equal(discoverReducer(s, { type: 'SELECT_CATALOG', catalog: plain }).selectedGenre, null);
    const y = ready(year, '2025');
    assert.equal(discoverReducer(y, { type: 'SELECT_CATALOG', catalog: top }).selectedGenre, null);
    assert.equal(discoverReducer(s, { type: 'SELECT_TYPE', selectedType: 'series' }).selectedGenre, '2026');
    const searching = { ...s, isSearchMode: true };
    assert.equal(discoverReducer(searching, { type: 'EXIT_SEARCH' }).selectedGenre, null);
    assert.equal(discoverReducer({ ...y, isSearchMode: true }, { type: 'EXIT_SEARCH' }).selectedCatalog, top);
});
