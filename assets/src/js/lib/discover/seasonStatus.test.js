import test from 'node:test';
import assert from 'node:assert/strict';

import { isSeasonUnfinished, unfinishedSeasons, seasonsOf, isAiringStatus, subscriptionTargetFor } from './seasonStatus.js';

// A fixed "now" so a test written today still means the same thing in a year.
const NOW = Date.parse('2026-08-15T12:00:00Z');

function ep(season, episode, released) {
    return { season, episode, id: `tt1:${season}:${episode}`, released };
}

const aired = (n) => ep(3, n, '2026-07-01T00:00:00Z');
const upcoming = (n) => ep(3, n, '2026-09-01T00:00:00Z');

test('a season with an episode still to air is unfinished', () => {
    const videos = [aired(1), aired(2), upcoming(3)];
    assert.equal(isSeasonUnfinished(videos, 3, { now: NOW }), true);
});

test('a fully aired season of a finished show is over', () => {
    const videos = [aired(1), aired(2)];
    assert.equal(isSeasonUnfinished(videos, 3, { now: NOW }), false);
});

// The gap between a finale and the next schedule: every date is in the past,
// but the show is still being made. Refusing a subscription here is refusing
// it at exactly the moment someone following the show wants one — the same
// reason the server's seasonIsOver needs both halves.
test('the latest season of a show still in production stays open', () => {
    const videos = [ep(2, 1, '2026-01-01T00:00:00Z'), aired(1), aired(2)];
    assert.equal(isSeasonUnfinished(videos, 3, { now: NOW, status: 'Continuing' }), true);
    // ...but only the latest one: season 2 is done regardless of the status.
    assert.equal(isSeasonUnfinished(videos, 2, { now: NOW, status: 'Continuing' }), false);
});

test('an episode with no date reads as unknown, not as aired', () => {
    const videos = [aired(1), ep(3, 2, undefined)];
    assert.equal(isSeasonUnfinished(videos, 3, { now: NOW }), true);
});

test('an unparseable date is treated the same way', () => {
    const videos = [aired(1), ep(3, 2, 'sometime next year')];
    assert.equal(isSeasonUnfinished(videos, 3, { now: NOW }), true);
});

test('specials and unknown seasons are never subscribable', () => {
    const videos = [ep(0, 1, undefined), { episode: 1, released: undefined }];
    assert.equal(isSeasonUnfinished(videos, 0, { now: NOW }), false);
    assert.equal(isSeasonUnfinished(videos, undefined, { now: NOW }), false);
});

test('a season the metadata does not mention is not unfinished', () => {
    assert.equal(isSeasonUnfinished([aired(1)], 9, { now: NOW }), false);
    assert.equal(isSeasonUnfinished([], 1, { now: NOW }), false);
    assert.equal(isSeasonUnfinished(undefined, 1, { now: NOW }), false);
});

test('seasonsOf groups by season and drops what cannot be subscribed to', () => {
    const seasons = seasonsOf([ep(1, 1), ep(1, 2), ep(2, 1), ep(0, 1), { episode: 5 }]);
    assert.deepEqual([...seasons.keys()], [1, 2]);
    assert.equal(seasons.get(1).length, 2);
});

test('unfinishedSeasons lists them in order', () => {
    const videos = [
        ep(1, 1, '2025-01-01T00:00:00Z'),
        ep(2, 1, '2026-01-01T00:00:00Z'),
        ep(3, 1, '2026-07-01T00:00:00Z'),
        ep(3, 2, '2026-09-01T00:00:00Z'),
    ];
    assert.deepEqual(unfinishedSeasons(videos, { now: NOW }), [3]);
});

test('airing statuses cover what the addons actually send', () => {
    for (const s of ['Continuing', 'returning series', 'In Production', 'airing']) {
        assert.equal(isAiringStatus(s), true, s);
    }
    for (const s of ['Ended', 'Canceled', '', undefined, null]) {
        assert.equal(isAiringStatus(s), false, String(s));
    }
});

// --- subscriptionTargetFor ---

const airingMeta = {
    status: 'Continuing',
    videos: [aired(1), upcoming(2)],
};
const finishedMeta = {
    status: 'Ended',
    videos: [aired(1), aired(2)],
};

test('an episode id targets its season', () => {
    assert.deepEqual(
        subscriptionTargetFor('series', 'tt1190634:3:5', 'tt1190634', airingMeta),
        { kind: 'season', videoId: 'tt1190634', season: 3 },
    );
});

test('a movie id targets the film', () => {
    assert.deepEqual(
        subscriptionTargetFor('movie', 'tt0111161', 'tt0111161', null),
        { kind: 'movie', videoId: 'tt0111161' },
    );
});

test('a finished season is not offered when the meta says so', () => {
    assert.equal(subscriptionTargetFor('series', 'tt1190634:3:5', 'tt1190634', finishedMeta), null);
});

// The deep-link path opens streams before the meta arrives. Offering
// optimistically is deliberate — the write path re-checks and says no with a
// message, which beats silently hiding the only affordance on the screen.
test('without meta the season is offered anyway', () => {
    assert.deepEqual(
        subscriptionTargetFor('series', 'tt1190634:3:5', 'tt1190634', null),
        { kind: 'season', videoId: 'tt1190634', season: 3 },
    );
});

test('a series opened without an episode has nothing to target', () => {
    assert.equal(subscriptionTargetFor('series', 'tt1190634', 'tt1190634', airingMeta), null);
});

test('specials cannot be subscribed to', () => {
    assert.equal(subscriptionTargetFor('series', 'tt1190634:0:1', 'tt1190634', airingMeta), null);
});

// Library items carry internal ids no addon or indexer could resolve, so a
// subscription on one would poll forever and find nothing.
test('library ids are refused', () => {
    assert.equal(subscriptionTargetFor('movie', 'wt-8c1f0d24', 'wt-8c1f0d24', null), null);
    assert.equal(subscriptionTargetFor('series', 'wt-8c1f:1:2', 'wt-8c1f', null), null);
});
