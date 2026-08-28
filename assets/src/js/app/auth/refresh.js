import av from '../../lib/av';
import {nextLocation} from '../../lib/auth/nextLocation.js';
av(async function() {
    const {refresh} = (await import('../../lib/supertokens'));
    let refreshed = false;
    try {
        refreshed = await refresh(window._CSRF);
    } catch (err) {
        console.error(err);
    }
    window.location.replace(nextLocation(refreshed, window.location));
});

export {}
