import test from 'node:test';
import assert from 'node:assert/strict';
import { torrentBytes } from './extTorrentBytes.js';

const bytes = [100, 56, 58, 97, 110, 110, 111, 117, 110, 99, 101];

test('0.1.12 and earlier: the bytes are the torrent itself', () => {
    assert.deepEqual(Array.from(torrentBytes(bytes)), bytes);
    assert.deepEqual(Array.from(torrentBytes(new Uint8Array(bytes).buffer)), bytes);
    assert.deepEqual(Array.from(torrentBytes(new Uint8Array(bytes))), bytes);
});

test('0.1.13 and later: the bytes are wrapped as torrent.data', () => {
    assert.deepEqual(Array.from(torrentBytes({ data: bytes })), bytes);
    assert.deepEqual(Array.from(torrentBytes({ data: new Uint8Array(bytes).buffer })), bytes);
});

test('anything else yields no bytes instead of an empty file', () => {
    assert.equal(torrentBytes(undefined), null);
    assert.equal(torrentBytes({}), null);
    assert.equal(torrentBytes({ data: undefined }), null);
});
