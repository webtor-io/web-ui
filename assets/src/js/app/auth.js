import {init, refresh} from '../lib/supertokens';
import {resumeRefresh} from '../lib/auth/nextLocation';
try {
    await init(window._CSRF);
} catch (err) {
    console.error(err);
}

// views/auth/refresh.html: a page load whose access token had expired. The
// SDK is already here, so the refresh starts now -- not behind layout.js and
// its analytics chunks, and without a second copy of the SDK.
if (document.getElementById('auth-refresh')) {
    let storage;
    try {
        storage = window.sessionStorage;
    } catch {
        // storage blocked: resumeRefresh copes
    }
    await resumeRefresh(() => refresh(window._CSRF), {
        location: window.location,
        storage,
        now: Date.now(),
        failed(err) {
            console.error(err);
            document.getElementById('auth-refresh')?.classList.add('hidden');
            const failed = document.getElementById('auth-refresh-failed');
            failed?.classList.remove('hidden');
            failed?.classList.add('flex');
        },
    });
}

export {}
