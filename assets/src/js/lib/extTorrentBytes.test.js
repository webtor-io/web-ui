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

// A message's data may be built in another realm: the extension's world or a
// frame. `instanceof ArrayBuffer` is false there, and the bytes read as none.
test('bytes made in another realm are still bytes', async () => {
    const vm = await import('node:vm');
    const other = vm.createContext({});
    const foreignBuffer = vm.runInContext(`new Uint8Array(${JSON.stringify(bytes)}).buffer`, other);
    assert.equal(foreignBuffer instanceof ArrayBuffer, false, 'the fixture must come from another realm');
    assert.deepEqual(Array.from(torrentBytes(foreignBuffer)), bytes);
    assert.deepEqual(Array.from(torrentBytes({ data: foreignBuffer })), bytes);
    const foreignView = vm.runInContext(`new Uint8Array(${JSON.stringify(bytes)})`, other);
    assert.deepEqual(Array.from(torrentBytes(foreignView)), bytes);
    const foreignArray = vm.runInContext(JSON.stringify(bytes), other);
    assert.deepEqual(Array.from(torrentBytes({ data: foreignArray })), bytes);
});

test('a view gives its own bytes, whatever its element type', () => {
    const buf = new Uint8Array([0, ...bytes, 0]).buffer;
    assert.deepEqual(Array.from(torrentBytes(new Uint8Array(buf, 1, bytes.length))), bytes);
    assert.deepEqual(Array.from(torrentBytes(new DataView(buf, 1, bytes.length))), bytes);
    const wide = new Uint16Array([0x0164, 0x3a38]); // little-endian: 64 01 38 3a
    assert.deepEqual(Array.from(torrentBytes(wide)), Array.from(new Uint8Array(wide.buffer)));
});
