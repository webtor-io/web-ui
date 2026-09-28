// makeDebug is the `debug` package's logger when `localStorage.debug` asks
// for it, else a no-op. Reached at the top level of modules the layout and
// the embed import (asyncView.js, message.js): a browser whose localStorage
// throws on access (site data blocked, some private modes) must get the
// no-op, not an exception -- an exception here fails the import of the
// whole entry, and with it Turnstile and the async navigation.
export async function makeDebug(name) {
    let on = false;
    try {
        on = !!localStorage.debug;
    } catch (e) {
        on = false;
    }
    if (on) {
        const makeDebug = (await import('debug')).default;
        return makeDebug(name);
    } else {
        return function() {};
    }
}
