import { init as initI18n, t, tf } from '../../lib/auth/i18n';

const EMAIL_KEY = 'auth.loginEmail';

(function restoreEmail() {
    try {
        const email = sessionStorage.getItem(EMAIL_KEY);
        if (!email) return;
        sessionStorage.removeItem(EMAIL_KEY);
        const input = document.querySelector('form input[name=email]');
        if (input && !input.value) input.value = email;
    } catch {}
})();

window.submitLoginForm = function(target, e) {
    (async (data) => {
        await initI18n();
        const initProgressLog = (await import('../../lib/progressLog')).initProgressLog;
        const pl = initProgressLog(document.querySelector('.progress-alert'));
        pl.clear();
        const e = pl.inProgress('login', tf('auth.progress.sendingMagicLink', data.email));
        const supertokens = (await import('../../lib/supertokens'));
        try {
            await supertokens.sendMagicLink(data, window._CSRF);
            e.done(tf('auth.progress.magicLinkSent', data.email));
        } catch (err) {
            console.error(err);
            const {describeAuthError} = await import('../../lib/auth/errors');
            const {message, reloading} = await describeAuthError(err, {t, tf});
            if (reloading) {
                // The reloaded page starts empty; the viewer only presses Send again.
                try {
                    sessionStorage.setItem(EMAIL_KEY, data.email);
                } catch {}
            }
            e.error(message);
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
        await initI18n();
        const initProgressLog = (await import('../../lib/progressLog')).initProgressLog;
        const pl = initProgressLog(document.querySelector('.progress-alert'));
        pl.clear();
        const progressEntry = pl.inProgress('login', tf('auth.progress.redirectingTo', provider));
        const supertokens = (await import('../../lib/supertokens'));
        try {
            await supertokens.signInWith(window._CSRF, provider);
        } catch (err) {
            console.error(err);
            if (err.statusText) {
                progressEntry.error(err.statusText.toLowerCase());
            } else if (err.message) {
                progressEntry.error(err.message.toLowerCase());
            } else {
                progressEntry.error(tf('auth.progress.redirectFailed', provider));
            }
            progressEntry.close();
        }
    })();
    e.preventDefault();
    return false;
}
