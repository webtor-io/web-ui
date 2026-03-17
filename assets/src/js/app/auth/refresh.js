import av from '../../lib/av';
av(async function() {
    const {refresh} = (await import('../../lib/supertokens'));
    try {
        await refresh(window._CSRF);
        window.location.replace(window.location.href);
    } catch (err) {
        console.error(err);
        window.location = '/login';
    }
});

export {}
