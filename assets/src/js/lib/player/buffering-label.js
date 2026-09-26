// When the player's buffering label (BufferingLabel.jsx) is the lock -- the
// button "Buffering | lock 5 Mbps >" that opens the stream plan card over
// the video -- rather than the plain "Buffering" (owner, 2026-09-26; docs/
// player.md "Buffering label").
//
// Whether the stall is the plan's cap is the transfer status's word, not the
// player's: the status on the same page has the server's verdict (thp's
// limiter has held the viewer at the cap long enough for its plan box, and
// something faster is on sale) and the page's own (a real stall of at least
// 1.5 s), and publishes the label only where its own block sells the stream
// box at a stall -- not inside the free grace window, not while the grace
// popup is up or on its way (lib/transferStatus.js playerLabel, carried here
// by lib/playerLabel.js). No status on the page (an embed), no label.
//
// capLock adds what only the player knows at this very moment, because the
// label it holds can be a second old (the status draws once a second):
//   - the wait on screen is the playing film stalling -- not a source
//     starting, a session seek, the translation hold or the next file
//     loading, each of which shows the same label with its own reason;
//   - by its own clock the film is past its free grace window (a session
//     seek back into the window reads a label the status has not taken back
//     yet).
// The grace popup needs no clause of its own: it holds the film paused
// (grace-hold.js), and a paused film shows no label.
//
// Returns the label to draw the lock with, or null for the plain pill.
export function capLock({
    label, playing = false, loading = false, seeking = false, preHolding = false, nextLoading = false,
    awaitingStart = false, graceSec = 0, movieTime = 0,
} = {}) {
    // Only a whole label: the lock says the rate, the card needs its link.
    if (!label || !label.rate || !label.cta || !label.cta.url) return null;
    if (!playing || !loading) return null;
    if (seeking || preHolding || nextLoading || awaitingStart) return null;
    if (graceSec > 0 && movieTime < graceSec) return null;
    return label;
}
