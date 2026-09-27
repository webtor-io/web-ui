// When the player's buffering label (BufferingLabel.jsx) is the lock -- the
// button "Buffering | lock 5 Mbps >" that opens the stream plan card -- rather
// than the plain "Buffering" (owner, 2026-09-26; docs/player.md "Buffering
// label").
//
// Whether the viewer is held at the plan's cap is the transfer status's word,
// not the player's: the status on the same page has the server's verdict
// (thp's limiter has held the viewer at the cap long enough for its plan box,
// and something faster is on sale) and publishes the label whenever that
// verdict holds and the card's data came with it -- not inside the free grace
// window, not while the grace popup is up or on its way (lib/transferStatus.js
// playerLabel, carried here by lib/playerLabel.js). No status on the page (an
// embed), no label.
//
// Every wait the pill is shown for is a wait at that cap: the playing film
// stalling, a seek (a session seek's new run, or one inside the run), the
// next file starting, the hold after a seek -- the bytes of each come through
// the same limiter (owner, 2026-09-26: "after a seek at the cap the pill says
// just Buffering, but the seek wait is limited by the plan too"). So the
// player does not ask which wait it is. It adds only what it knows at this
// very moment and the label, drawn once a second, may not know yet:
//   - by its own clock (movie time, a session seek's target included) the film
//     is past its free grace window: a seek back into the window reads a
//     label the status has not taken back yet;
//   - its grace popup is not up, and not on its way this very render
//     (graceUp): a session seek past the window puts the popup up as the seek
//     starts, and the seek's wait is on screen behind it.
//
// Returns the label to draw the lock with, or null for the plain pill.
export function capLock({ label, graceSec = 0, movieTime = 0, graceUp = false } = {}) {
    // Only a whole label: the lock says the rate, the card needs its link.
    if (!label || !label.rate || !label.cta || !label.cta.url) return null;
    if (graceUp) return null;
    if (graceSec > 0 && movieTime < graceSec) return null;
    return label;
}
