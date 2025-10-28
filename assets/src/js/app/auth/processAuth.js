export async function processAuth(el, name, description, action) {
    const initProgressLog = (await import('../../lib/progressLog')).initProgressLog;
    const pl = initProgressLog(el.querySelector('.progress-alert'));
    pl.clear();
    const e = pl.inProgress(name, description);
    const supertokens = (await import('../../lib/supertokens'));
    try {
        const res = await supertokens[action](window._CSRF);
        if (!res || res.status === 'OK') {
            e.done(`${name} successful`);
            window.dispatchEvent(new CustomEvent('auth'));
            const r = el.querySelector('a#return-url');
            if (r) {
                r.click();
            }
        } else if (res.status === 'RESTART_FLOW_ERROR') {
            e.error('magic link expired, try to login again');
        } else {
            e.error(`${name} failed, try to login again`);
        }
    } catch (err) {
        if (err.statusText) {
            e.error(err.statusText.toLowerCase());
        } else if (err.message) {
            e.error(err.message.toLowerCase());
        } else {
            e.error('unknown error');
        }
    }
    e.close();
}
