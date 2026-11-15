import av from '../lib/av';
import { trackImpressions } from '../lib/impression';

// private-refused-shown {code, reason} (S29), declared on the refusal card
// (partials/private/refusal), and the other impressions of a private torrent
// (the Vault modal's refusal, the seed-after-watching door on the resource
// page). Through av, so it runs once layout.js has set window.umami: on a full
// page load as well as after an async swap.
av(function() {
    trackImpressions(this);
});

export {}
