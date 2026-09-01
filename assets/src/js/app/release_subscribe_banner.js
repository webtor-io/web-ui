import av from '../lib/av';
import { trackImpressions } from '../lib/impression';

// The banner is an async island (re-rendered on subscribe/unsubscribe), so
// this binding runs per render; trackImpressions dedupes per page load.
av(function() {
    trackImpressions(this);
});

export {}
