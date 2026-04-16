import { makeI18n, getLang } from '../i18n';

// Keep this map in sync with services/i18n/i18n.go SupportedLangs. Missing
// entries silently fall back to English in init().
const loaders = {
    en: () => import(/* webpackChunkName: "locale-auth-en" */ '../../../../../locales/en.json?prefix=auth'),
    ru: () => import(/* webpackChunkName: "locale-auth-ru" */ '../../../../../locales/ru.json?prefix=auth'),
    es: () => import(/* webpackChunkName: "locale-auth-es" */ '../../../../../locales/es.json?prefix=auth'),
    de: () => import(/* webpackChunkName: "locale-auth-de" */ '../../../../../locales/de.json?prefix=auth'),
    fr: () => import(/* webpackChunkName: "locale-auth-fr" */ '../../../../../locales/fr.json?prefix=auth'),
    pt: () => import(/* webpackChunkName: "locale-auth-pt" */ '../../../../../locales/pt.json?prefix=auth'),
    it: () => import(/* webpackChunkName: "locale-auth-it" */ '../../../../../locales/it.json?prefix=auth'),
    pl: () => import(/* webpackChunkName: "locale-auth-pl" */ '../../../../../locales/pl.json?prefix=auth'),
    tr: () => import(/* webpackChunkName: "locale-auth-tr" */ '../../../../../locales/tr.json?prefix=auth'),
    nl: () => import(/* webpackChunkName: "locale-auth-nl" */ '../../../../../locales/nl.json?prefix=auth'),
    cs: () => import(/* webpackChunkName: "locale-auth-cs" */ '../../../../../locales/cs.json?prefix=auth'),
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

// Synchronous access after init(). Returns the key if called before init().
export function t(key) {
    if (!instance) return key;
    return instance.t(key);
}

export function tf(key, ...args) {
    if (!instance) return key;
    return instance.tf(key, ...args);
}
