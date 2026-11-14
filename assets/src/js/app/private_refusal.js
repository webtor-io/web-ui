import av from '../lib/av';
import { trackImpressions } from '../lib/impression';

// private-refused-shown {code, reason} (S29), declared on the refusal card
// (partials/private/refusal). Through av, so it runs once layout.js has set
// window.umami: on a full page load as well as after an async swap.
av(function() {
    trackImpressions(this);
});

export {}
