window.submitLoginForm = function(target, e) {
    (async (data) => {
        const initProgressLog = (await import('../../lib/progressLog')).initProgressLog;
        const pl = initProgressLog(document.querySelector('.progress-alert'));
        pl.clear();
        const e = pl.inProgress('login','sending magic link to ' + data.email);
        const supertokens = (await import('../../lib/supertokens'));
        try {
            await supertokens.sendMagicLink(data, window._CSRF);
            e.done('magic link sent to ' + data.email);
        } catch (err) {
            console.log(err);
            if (err.statusText) {
                e.error(err.statusText.toLowerCase());
            } else if (err.message) {
                e.error(err.message.toLowerCase());
            } else {
                e.error('unknown error');
            }
        }
        e.close();
    })({
        email: target.querySelector('input[name=email]').value,
    });
    e.preventDefault();
    return false;
}

window.signInWith = function(e, provider) {
    (async () => {
        const initProgressLog = (await import('../../lib/progressLog')).initProgressLog;
        const pl = initProgressLog(document.querySelector('.progress-alert'));
        pl.clear();
        const progressEntry = pl.inProgress('login',`redirecting to ${provider}`);
        const supertokens = (await import('../../lib/supertokens'));
        try {
            await supertokens.signInWith(window._CSRF, provider);
        } catch (err) {
            console.log(err);
            if (err.statusText) {
                progressEntry.error(err.statusText.toLowerCase());
            } else if (err.message) {
                progressEntry.error(err.message.toLowerCase());
            } else {
                progressEntry.error(`failed to redirect to ${provider}`);
            }
            progressEntry.close();
        }
    })();
    e.preventDefault();
    return false;
}
