import { makeI18n, getLang, langPath } from '../i18n';
export { langPath };

// Webpack 5 turns the template-literal dynamic import into a per-locale
// chunk: it scans `../../../../../locales/*.json`, applies the prefix= query
// to the locale-filter-loader, and emits `locale-discover-{lang}.js` for
// each match. New locales added to lib/i18n.js SUPPORTED need zero changes
// here — the `[request]` magic resolves at build time from the matched
// file basename.
function load(lang) {
    return import(
        /* webpackChunkName: "locale-discover-[request]" */
        `../../../../../locales/${lang}.json?prefix=discover`
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

// Synchronous access after init(). Throws if called before init().
export function t(key) {
    return instance.t(key);
}

export function tf(key, ...args) {
    return instance.tf(key, ...args);
}
