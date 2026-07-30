import av from '../lib/av';
import {initOnboardingChecklist} from '../lib/onboardingChecklist';
// initOnboardingChecklist returns its own teardown; av() calls it when the card
// is replaced, which happens on every async navigation away from the home page.
let destroy = null;
av(function() {
    destroy = initOnboardingChecklist(this);
}, function() {
    if (destroy) {
        destroy();
        destroy = null;
    }
});
