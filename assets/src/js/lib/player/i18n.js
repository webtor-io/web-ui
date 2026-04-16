import { makeI18n, getLang } from '../i18n';

// See lib/discover/i18n.js for the dynamic-import rationale.
function load(lang) {
    return import(
        /* webpackChunkName: "locale-player-[request]" */
        `../../../../../locales/${lang}.json?prefix=player`
    );
}

let instance;
let instanceLang;

export async function init() {
    const lang = getLang();
    if (instance && instanceLang === lang) return instance;
    const mod = await load(lang);
    instance = makeI18n(mod.default || mod);
    instanceLang = lang;
    return instance;
}

// Synchronous access after init(). Returns the key if called before init().
export function t(key) {
    if (!instance) return key;
    return instance.t(key);
}

export function tf(key, ...args) {
    if (!instance) return key;
    return instance.tf(key, ...args);
}
