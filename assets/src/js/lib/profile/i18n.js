import { makeI18n, getLang } from '../i18n';

// Keep this map in sync with services/i18n/i18n.go SupportedLangs. Missing
// entries silently fall back to English in init().
const loaders = {
    en: () => import(/* webpackChunkName: "locale-profile-en" */ '../../../../../locales/en.json?prefix=profile'),
    ru: () => import(/* webpackChunkName: "locale-profile-ru" */ '../../../../../locales/ru.json?prefix=profile'),
    es: () => import(/* webpackChunkName: "locale-profile-es" */ '../../../../../locales/es.json?prefix=profile'),
    de: () => import(/* webpackChunkName: "locale-profile-de" */ '../../../../../locales/de.json?prefix=profile'),
    fr: () => import(/* webpackChunkName: "locale-profile-fr" */ '../../../../../locales/fr.json?prefix=profile'),
    pt: () => import(/* webpackChunkName: "locale-profile-pt" */ '../../../../../locales/pt.json?prefix=profile'),
    it: () => import(/* webpackChunkName: "locale-profile-it" */ '../../../../../locales/it.json?prefix=profile'),
    pl: () => import(/* webpackChunkName: "locale-profile-pl" */ '../../../../../locales/pl.json?prefix=profile'),
    tr: () => import(/* webpackChunkName: "locale-profile-tr" */ '../../../../../locales/tr.json?prefix=profile'),
    nl: () => import(/* webpackChunkName: "locale-profile-nl" */ '../../../../../locales/nl.json?prefix=profile'),
    cs: () => import(/* webpackChunkName: "locale-profile-cs" */ '../../../../../locales/cs.json?prefix=profile'),
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

export function t(key) {
    if (!instance) return key;
    return instance.t(key);
}

export function tf(key, ...args) {
    if (!instance) return key;
    return instance.tf(key, ...args);
}
