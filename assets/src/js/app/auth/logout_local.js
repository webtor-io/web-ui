import av from '../../lib/av';

// The sign-out view for instances with no external identity provider.
//
// logout.js drives the same panel through processAuth, which exists to call
// SuperTokens and report what it answered. Here there is nothing to call:
// the handler deleted the session before this page was rendered, so signing
// out is already done by the time the script runs. What is left is the part
// the user actually sees -- the progress log's finished state -- and that is
// worth rendering the same way rather than hand-writing its markup into the
// template: the toolkit owns that structure, and a second copy of it in a
// template would drift the first time progressLog.js changes.
//
// Same i18n keys as the SuperTokens path, so the two read identically.
av(async function () {
    const { init: initI18n, t } = await import('../../lib/auth/i18n');
    await initI18n();
    const initProgressLog = (await import('../../lib/progressLog')).initProgressLog;
    const pl = initProgressLog(this.querySelector('.progress-alert'));
    pl.clear();
    const e = pl.inProgress('logout', t('auth.progress.loggingOut'));
    e.done(t('auth.progress.logoutSuccessful'));
    e.close();
});

export {}
