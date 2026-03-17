import {init} from '../lib/supertokens';
try {
    await init(window._CSRF);
} catch (err) {
    console.error(err);
}

export {}
