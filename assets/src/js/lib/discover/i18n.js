import { makeI18n, getLang, langPath } from '../i18n';
export { langPath };

// Keep this map in sync with services/i18n/i18n.go SupportedLangs and the
// other JS i18n loaders (auth, profile, player). Missing entries silently
// fall back to English in init() — that's how "pitchMe" stayed in English
// for /pl/discover users despite the Go server delivering Polish chips.
const loaders = {
    en: () => import(/* webpackChunkName: "locale-discover-en" */ '../../../../../locales/en.json?prefix=discover'),
    ru: () => import(/* webpackChunkName: "locale-discover-ru" */ '../../../../../locales/ru.json?prefix=discover'),
    es: () => import(/* webpackChunkName: "locale-discover-es" */ '../../../../../locales/es.json?prefix=discover'),
    de: () => import(/* webpackChunkName: "locale-discover-de" */ '../../../../../locales/de.json?prefix=discover'),
    fr: () => import(/* webpackChunkName: "locale-discover-fr" */ '../../../../../locales/fr.json?prefix=discover'),
    pt: () => import(/* webpackChunkName: "locale-discover-pt" */ '../../../../../locales/pt.json?prefix=discover'),
    it: () => import(/* webpackChunkName: "locale-discover-it" */ '../../../../../locales/it.json?prefix=discover'),
    pl: () => import(/* webpackChunkName: "locale-discover-pl" */ '../../../../../locales/pl.json?prefix=discover'),
    tr: () => import(/* webpackChunkName: "locale-discover-tr" */ '../../../../../locales/tr.json?prefix=discover'),
    nl: () => import(/* webpackChunkName: "locale-discover-nl" */ '../../../../../locales/nl.json?prefix=discover'),
    cs: () => import(/* webpackChunkName: "locale-discover-cs" */ '../../../../../locales/cs.json?prefix=discover'),
};

let instance;
let instanceLang;

export async function init() {
    const lang = getLang();
    if (instance && instanceLang === lang) return instance;
    const loader = loaders[lang] || loaders.en;
    const mod = await loader();
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
