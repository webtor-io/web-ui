import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
    ADDON_GENRE_NAMES, genreSlugs, genreLabels, optionLabel,
    catalogFilter, genreFor, filterOptions,
} from './genres.js';

// The jsx hooks answer locale imports with {}, so the real file is read.
const en = JSON.parse(readFileSync(new URL('../../../../../locales/en.json', import.meta.url), 'utf8'));
const tEn = (key) => en[key] || key;
const tKey = (key) => key;

test('every slug the map produces has an English label', () => {
    const slugs = genreSlugs(ADDON_GENRE_NAMES);
    assert.equal(slugs.length, 31, 'the whole vocabulary is reachable from addon names');
    assert.deepEqual(slugs.filter((s) => !(`genre.${s}` in en)), []);
});

test('genreSlugs: combos split, deduped, first appearance, unknown and junk left out', () => {
    assert.deepEqual(genreSlugs(['Sci-Fi & Fantasy', 'Action & Adventure', 'Science Fiction', 'Drama', 'Anime']),
        ['scifi', 'fantasy', 'action', 'adventure', 'drama']);
    assert.deepEqual(genreSlugs(['War & Politics', 'War']), ['war', 'politics']);
    assert.deepEqual(genreSlugs(['Reality-TV', 'Reality', 'Talk-Show', 'Film-Noir', 'Game-Show', 'TV Movie']),
        ['reality', 'talk', 'film_noir', 'game_show', 'tv_movie']);
    assert.deepEqual(genreSlugs(['action', 'DRAMA']), [], 'case-sensitive');
    assert.deepEqual(genreSlugs(['constructor', 'toString', '__proto__']), [], 'no prototype keys');
    assert.deepEqual(genreSlugs('Drama'), []);
    assert.deepEqual(genreSlugs(undefined), []);
});

test('genreLabels: slug labels, unknown names as themselves, deduped by label', () => {
    assert.deepEqual(genreLabels(['Sci-Fi', 'Science Fiction', 'Anime', 'Drama', 'Anime'], tEn), ['Sci-Fi', 'Anime', 'Drama']);
    assert.deepEqual(genreLabels(['Action & Adventure', 'Action'], tKey), ['genre.action', 'genre.adventure']);
    assert.deepEqual(genreLabels(['Talk-Show', 'Reality-TV'], tEn), ['Talk show', 'Reality TV']);
    assert.deepEqual(genreLabels(['constructor'], tKey), ['constructor'], 'shown as itself, never as a key');
    assert.deepEqual(genreLabels([null, 7, '', { a: 1 }, 'Drama'], tKey), ['genre.drama']);
    assert.deepEqual(genreLabels('Drama', tKey), []);
});

test('optionLabel: a known value by its label, a combo joined, a year as is', () => {
    assert.equal(optionLabel('Sci-Fi', tEn), 'Sci-Fi');
    assert.equal(optionLabel('Reality-TV', tEn), 'Reality TV');
    assert.equal(optionLabel('Action & Adventure', tKey), 'genre.action & genre.adventure');
    assert.equal(optionLabel('2026', tKey), '2026');
});

// Cinemeta 3.0.14's catalogs, trimmed (understand/cinemeta/manifest.json).
const top = { type: 'movie', id: 'top', extra: [{ name: 'genre', options: ['Action', 'Drama'] }, { name: 'search' }, { name: 'skip' }], extraSupported: ['search', 'genre', 'skip'], genres: ['Action', 'Drama'] };
const year = { type: 'movie', id: 'year', extra: [{ name: 'genre', options: ['2026', '2025', '1920'], isRequired: true }, { name: 'skip' }], extraSupported: ['genre', 'skip'], extraRequired: ['genre'], genres: ['2026', '2025', '1920'] };
const lastVideos = { type: 'series', id: 'last-videos', extra: [{ name: 'lastVideosIds', isRequired: true, optionsLimit: 100 }], extraSupported: ['lastVideosIds'], extraRequired: ['lastVideosIds'] };

test('catalogFilter: Cinemeta\'s optional genre, its required year, and catalogs with none', () => {
    assert.deepEqual(catalogFilter(top), { mode: 'genre', options: ['Action', 'Drama'] });
    assert.deepEqual(catalogFilter(year), { mode: 'year', options: ['2026', '2025', '1920'] });
    assert.equal(catalogFilter(lastVideos), null);
    assert.equal(catalogFilter({ type: 'movie', id: 'plain' }), null);
});

test('catalogFilter: the legacy form, required either way, junk options', () => {
    assert.deepEqual(catalogFilter({ extraSupported: ['genre'], genres: ['Drama', 'Drama', 7, ''] }), { mode: 'genre', options: ['Drama'] });
    assert.deepEqual(catalogFilter({ extraSupported: ['genre'], extraRequired: ['genre'], genres: ['1999'] }), { mode: 'year', options: ['1999'] });
    assert.deepEqual(catalogFilter({ extra: [{ name: 'genre', options: ['2001'] }], extraRequired: ['genre'] }), { mode: 'year', options: ['2001'] });
    assert.equal(catalogFilter({ genres: ['Drama'] }), null, 'genres without the genre extra declared');
    assert.equal(catalogFilter({ extra: [{ name: 'genre' }] }), null, 'no options to offer');
    assert.equal(catalogFilter({ extra: 'genre', extraSupported: 'genre', genres: ['Drama'] }), null);
});

test('catalogFilter: a required genre that is not a year list is left alone', () => {
    assert.equal(catalogFilter({ extra: [{ name: 'genre', isRequired: true, options: ['Shonen', 'Seinen'] }] }), null);
    assert.equal(catalogFilter({ extra: [{ name: 'genre', isRequired: true, options: ['2026', '90s'] }] }), null);
});

test('genreFor: an offered value, else the default; null without a filter', () => {
    const g = { filter: catalogFilter(top) };
    const y = { filter: catalogFilter(year) };
    assert.equal(genreFor(g, 'Drama'), 'Drama');
    assert.equal(genreFor(g, 'Bogus'), null);
    assert.equal(genreFor(g, null), null);
    assert.equal(genreFor(y, '1920'), '1920');
    assert.equal(genreFor(y, 'Drama'), '2026');
    assert.equal(genreFor(y, undefined), '2026');
    assert.equal(genreFor({ filter: null }, 'Drama'), null);
    assert.equal(genreFor(null, 'Drama'), null);
});

test('filterOptions: genres by label in the page language, years in manifest order', () => {
    const labels = { 'genre.action': 'Боевик', 'genre.drama': 'Драма', 'genre.animation': 'Мультфильм' };
    const tRu = (k) => labels[k] || k;
    const opts = filterOptions({ mode: 'genre', options: ['Drama', 'Animation', 'Action'] }, tRu, 'ru');
    assert.deepEqual(opts.map((o) => o.value), ['Action', 'Drama', 'Animation']);
    assert.deepEqual(opts.map((o) => o.label), ['Боевик', 'Драма', 'Мультфильм']);
    assert.deepEqual(filterOptions(catalogFilter(year), tKey, 'en').map((o) => o.value), ['2026', '2025', '1920']);
});
