// The Chrome extension hands /ext/download the .torrent it intercepted in a
// window message. Versions up to 0.1.12 put the bytes in `torrent` itself;
// 0.1.13 and later wrap them as `torrent.data`. Read the bytes by their
// shape, not by comparing versions, so a future release that changes the
// version scheme (or a dev build without `ver`) cannot turn into an empty file.
//
// The shape checks are realm-safe: `instanceof ArrayBuffer` is false for a
// buffer made in another realm (a frame, an extension's world), and that
// would have read a real torrent as "no bytes". Array.isArray, ArrayBuffer.isView
// and the [[Class]] tag look at the object, not at whose constructor made it.
export function torrentBytes(torrent) {
    if (torrent == null) return null;
    if (Array.isArray(torrent) || isArrayBuffer(torrent)) {
        return new Uint8Array(torrent);
    }
    if (ArrayBuffer.isView(torrent)) {
        // The view's own bytes, whatever its element type: copying a
        // Uint16Array element-wise would truncate every value to a byte.
        return new Uint8Array(torrent.buffer, torrent.byteOffset, torrent.byteLength);
    }
    if (torrent.data != null) return torrentBytes(torrent.data);
    return null;
}

function isArrayBuffer(x) {
    return Object.prototype.toString.call(x) === '[object ArrayBuffer]';
}
