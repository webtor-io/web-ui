// The Chrome extension hands /ext/download the .torrent it intercepted in a
// window message. Versions up to 0.1.12 put the bytes in `torrent` itself;
// 0.1.13 and later wrap them as `torrent.data`. Read the bytes by their
// shape, not by comparing versions, so a future release that changes the
// version scheme (or a dev build without `ver`) cannot turn into an empty file.
export function torrentBytes(torrent) {
    if (torrent == null) return null;
    if (Array.isArray(torrent) || torrent instanceof ArrayBuffer || ArrayBuffer.isView(torrent)) {
        return new Uint8Array(torrent);
    }
    if (torrent.data != null) return torrentBytes(torrent.data);
    return null;
}
