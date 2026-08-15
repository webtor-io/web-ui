import test from 'node:test';
import assert from 'node:assert/strict';

import { discoverReducer, initialState } from './discoverReducer.js';

// The subscription slice of the reducer. Its whole job is the optimistic
// flip: the bell fills the moment it is clicked, and goes back if the server
// refuses. Everything the modal renders reads from this Set.

const KEY = 'season:tt1190634:3';

test('the slice starts empty and unlimited-until-told-otherwise', () => {
    assert.equal(initialState.subscriptionKeys.size, 0);
    assert.equal(initialState.subscriptionCount, 0);
    assert.equal(initialState.subscriptionLimit, -1);
});

test('SUBSCRIPTIONS_LOADED replaces the whole set', () => {
    const loaded = discoverReducer(initialState, {
        type: 'SUBSCRIPTIONS_LOADED',
        keys: [KEY, 'movie:tt0111161'],
        count: 2,
        limit: 3,
    });
    assert.deepEqual([...loaded.subscriptionKeys], [KEY, 'movie:tt0111161']);
    assert.equal(loaded.subscriptionCount, 2);
    assert.equal(loaded.subscriptionLimit, 3);

    // A later load with fewer keys must not merge into the old set —
    // unsubscribing elsewhere has to be visible here.
    const reloaded = discoverReducer(loaded, { type: 'SUBSCRIPTIONS_LOADED', keys: [KEY], count: 1, limit: 3 });
    assert.deepEqual([...reloaded.subscriptionKeys], [KEY]);
});

test('SUBSCRIPTION_ADD and _REMOVE are a round trip', () => {
    const added = discoverReducer(initialState, { type: 'SUBSCRIPTION_ADD', key: KEY });
    assert.equal(added.subscriptionKeys.has(KEY), true);
    assert.equal(added.subscriptionCount, 1);

    const removed = discoverReducer(added, { type: 'SUBSCRIPTION_REMOVE', key: KEY });
    assert.equal(removed.subscriptionKeys.has(KEY), false);
    assert.equal(removed.subscriptionCount, 0);
});

// The rollback path: DiscoverApp adds optimistically, then removes the same
// key when the server answers 402 or 409. Getting back to exactly the
// previous state is what makes that safe.
test('a refused subscribe leaves the set as it was', () => {
    const before = discoverReducer(initialState, {
        type: 'SUBSCRIPTIONS_LOADED', keys: ['movie:tt0111161'], count: 1, limit: 3,
    });
    const optimistic = discoverReducer(before, { type: 'SUBSCRIPTION_ADD', key: KEY });
    const rolledBack = discoverReducer(optimistic, { type: 'SUBSCRIPTION_REMOVE', key: KEY });
    assert.deepEqual([...rolledBack.subscriptionKeys], [...before.subscriptionKeys]);
    assert.equal(rolledBack.subscriptionCount, before.subscriptionCount);
});

test('adding twice is idempotent', () => {
    let state = discoverReducer(initialState, { type: 'SUBSCRIPTION_ADD', key: KEY });
    state = discoverReducer(state, { type: 'SUBSCRIPTION_ADD', key: KEY });
    assert.equal(state.subscriptionKeys.size, 1);
    assert.equal(state.subscriptionCount, 1);
});

// Preact re-renders on identity change; mutating the Set in place would
// leave the bell drawn in its old state.
test('every action produces a new Set', () => {
    const added = discoverReducer(initialState, { type: 'SUBSCRIPTION_ADD', key: KEY });
    assert.notEqual(added.subscriptionKeys, initialState.subscriptionKeys);
    const removed = discoverReducer(added, { type: 'SUBSCRIPTION_REMOVE', key: KEY });
    assert.notEqual(removed.subscriptionKeys, added.subscriptionKeys);
});

test('the slice does not disturb the rest of the state', () => {
    const seeded = { ...initialState, items: [{ id: 'tt1' }], selectedType: 'series' };
    const after = discoverReducer(seeded, { type: 'SUBSCRIPTION_ADD', key: KEY });
    assert.deepEqual(after.items, seeded.items);
    assert.equal(after.selectedType, 'series');
    assert.equal(after.watchlistIds, seeded.watchlistIds);
});
