import { safeHref } from '../safeHref';

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
// One answer says it before the status can: the viewer's to the grace popup
// ("continue at 5 Mbps", its close, or Play -- data-grace-cta-answered on
// this player's element). From then on the rest of the film is at the cap,
// but the status's word comes 8-15 s later (thp's verdict, then the box), and
// the popup kept the lock down while it was up: a seek past the window put
// the popup up, and after "slow" a plain "Buffering" hung (owner,
// 2026-09-27). So once answered the player draws the lock at once with the
// card the stream job rendered on the element (answerLabel) -- the status's
// stream box in its own words -- and the status's label replaces it the
// moment it is there: one source of truth where there is one.
//
// The answer's lock covers only the gap before the status has a word, never
// a word that says no. A null label is both "not yet" and "not the cap", so
// the status says the second out loud: the cause it names instead
// (lib/transferStatus.js playerCause -- the swarm, no seeders, pieces nobody
// has, nothing flowing), where statusview refuses to sell on purpose and a
// trial would not end the wait. And once the status's own label has been up
// on this stretch of the film past its window (statusSpoke), taking it back
// is its word too -- it drops its box only after 10 s of the viewer under
// the cap -- so from then on the lock is the status's alone. A seek back
// inside the window starts a new stretch: the status takes its label back
// there for the window, not for the cap, and past it again the gap before
// its verdict is the answer's again.
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
//     label the status has not taken back yet -- and the answer's lock does
//     not hold there either, the window plays at the grace rate again;
//   - its grace popup is not up, and not on its way this very render
//     (graceUp): a session seek past the window puts the popup up as the seek
//     starts, and the seek's wait is on screen behind it.
// The answer's lock has more: the stream job's word that the file fits
// under the cap with room to spare (data-status-fits-cap) -- a wait there is
// not the cap's, as far as anyone knows before the server says so -- the
// caller's `answered`, which is this file's answer and not the next file's
// start (a new element, and a new grace window), and the status's word that
// is not a label: the other `cause` it names, and `spoke` (statusSpoke) --
// its label has been up on this stretch, and whatever it says now is its.
//
// Returns the label to draw the lock with, or null for the plain pill.
export function capLock({ label, answer = null, answered = false, fitsCap = false, cause = '', spoke = false, graceSec = 0, movieTime = 0, graceUp = false } = {}) {
    if (graceUp) return null;
    if (graceSec > 0 && movieTime < graceSec) return null;
    if (whole(label)) return label;
    if (answered && !fitsCap && !cause && !spoke && whole(answer)) return answer;
    return null;
}

// statusSpoke is whether the status's own label has been up since the answer
// on this stretch of the film past its grace window, given the last render's
// `spoke`: from then on the status's word alone decides the lock, and its
// label taken back means the plain pill, not the answer's lock again. Reset
// inside the window by movie time (a seek back): the status takes its label
// back there for the window, and past it again its verdict takes 8-15 s
// once more -- the answer's gap again.
export function statusSpoke(spoke, { label, answered = false, graceSec = 0, movieTime = 0 } = {}) {
    if (graceSec > 0 && movieTime < graceSec) return false;
    return !!spoke || (answered && whole(label));
}

// Only a whole label: the lock says the rate, the card needs its link.
const whole = (label) => !!(label && label.rate && label.cta && label.cta.url);

// answerLabel is the label the stream job rendered on the player element for
// the moment the viewer answers the grace popup (stream_video.html
// data-cap-card-*, jobs/scripts CapCard): the status's stream box in the same
// words -- the same keys and number, the promo plan through the player's own
// /trial surface -- and the line with what the file needs is the element's
// data-status-stall-sub, the very line the status's label takes (its `sub`),
// the cap alone without it. The props are the status label's, with `source`
// saying which of the two raised the lock. null where the job rendered none
// (no grace window, an embed, nothing faster on sale) or not a whole one.
export function answerLabel(el) {
    const d = el && el.dataset;
    if (!d) return null;
    // Only our own paths and https links become the card's href.
    const url = safeHref(d.capCardUrl);
    if (!url || !d.capCardRate || !d.capCardTitle || !d.capCardCta) return null;
    return {
        rate: d.capCardRate,
        title: d.capCardTitle,
        sub: d.statusStallSub || d.capCardSub || '',
        cta: { label: d.capCardCta, note: d.capCardNote || '', url },
        props: {
            ctx: 'stream', location: 'player', auth: d.capCardAuth || '', state: 'stream_stall',
            tier: d.capCardTier || '', target: d.capCardTarget || '', source: 'grace-answer',
        },
    };
}

// lockKey is a lock's impression key: its props but for `source`. The same
// lock raised by the answer and then taken over by the status is one lock
// seen, counted with the source that raised it first.
export function lockKey(label) {
    const { source, ...rest } = (label && label.props) || {};
    return JSON.stringify(Object.keys(rest).sort().map((k) => [k, rest[k]]));
}
