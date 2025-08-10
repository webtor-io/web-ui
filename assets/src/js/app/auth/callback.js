import av from '../../lib/av';
av(async function() {
    const initProgressLog = (await import('../../lib/progressLog')).initProgressLog;
    const pl = initProgressLog(this.querySelector('.progress-alert'));
    pl.clear();
    const e = pl.inProgress('callback', 'checking auth code');
    const supertokens = (await import('../../lib/supertokens'));
    try {
        const res = await supertokens.handleCallback(window._CSRF);
        if (res.status === 'OK') {
            e.done('login successful');
            window.dispatchEvent(new CustomEvent('auth'));
        } else if (res.status === 'SIGN_IN_UP_NOT_ALLOWED') {
            e.error(res.reason);
        } else {
            e.error('login failed, try to login again');
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
});

export {}
