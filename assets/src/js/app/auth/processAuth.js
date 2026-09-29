import { init as initI18n, t, tf } from '../../lib/auth/i18n';

export async function processAuth(el, name, descriptionKey, action) {
    await initI18n();
    const initProgressLog = (await import('../../lib/progressLog')).initProgressLog;
    const pl = initProgressLog(el.querySelector('.progress-alert'));
    pl.clear();
    const e = pl.inProgress(name, t(descriptionKey));
    const supertokens = (await import('../../lib/supertokens'));
    try {
        const res = await supertokens[action](window._CSRF);
        if (!res || res.status === 'OK') {
            e.done(t(`auth.progress.${name}Successful`));
            window.dispatchEvent(new CustomEvent('auth'));
            const r = el.querySelector('a#return-url');
            if (r) {
                r.click();
            }
        } else if (res.status === 'RESTART_FLOW_ERROR') {
            e.error(t('auth.progress.magicLinkExpired'));
        } else {
            e.error(t(`auth.progress.${name}Failed`));
        }
    } catch (err) {
        const {describeAuthError} = await import('../../lib/auth/errors');
        e.error((await describeAuthError(err, {t, tf})).message);
    }
    e.close();
}
