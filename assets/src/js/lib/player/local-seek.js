// A seek inside the run that is already playing.
//
// A transcoder session plays a RUN: FFmpeg started at `seekOffset` (film time)
// and writes segments as fast as the torrent feeds it -- no `-re`, no list
// size -- so the playlist holds everything from the start of the run to
// wherever FFmpeg has got to (content-transcoder/services/hls.go). Every point
// in between is one `video.currentTime =` away.
//
// Until 2026-09-20 every seek in a session was a POST to the transcoder: a new
// FFmpeg, a frozen frame, a second or more of nothing -- including the ten
// seconds of a double tap and the fifteen of an arrow key, which land inside
// what is already there almost every time. A restart is for the two cases that
// need one: back before the run began, or forward past what has been produced.

// How close to the end of what is produced a local seek may land. Two
// segments (4 s each): nearer than that and the player would be waiting on
// FFmpeg at the live edge -- which a restart from the quantized point does
// not make faster, but the edge of a playlist is no place to aim for.
export const EDGE_S = 8;

// producedEnd: how far the run has been written, in the run's own time. hls.js
// knows it from the playlist it keeps reloading; without it (native HLS) the
// element's seekable range says the same thing.
export function producedEnd(video, hls) {
    const level = hls && hls.levels && hls.levels[hls.currentLevel >= 0 ? hls.currentLevel : 0];
    const total = level && level.details && level.details.totalduration;
    if (typeof total === 'number' && total > 0) return total;
    try {
        const s = video && video.seekable;
        if (s && s.length > 0) return s.end(s.length - 1);
    } catch (e) { /* a detached element */ }
    return 0;
}

// localSeekTarget: the run-time position for a film-time target, or null when
// the target is outside the run and the transcoder has to be asked.
export function localSeekTarget(filmTime, seekOffset, produced, { edge = EDGE_S } = {}) {
    if (!(produced > 0) || !Number.isFinite(filmTime) || !Number.isFinite(seekOffset)) return null;
    const local = filmTime - seekOffset;
    if (local < 0) return null;                  // before the run began
    if (local > produced - edge) return null;    // past what FFmpeg has written
    return local;
}
