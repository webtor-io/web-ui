import { makeI18n, getLang } from '../i18n';

const loaders = {
    en: () => import(/* webpackChunkName: "locale-profile-en" */ '../../../../../locales/en.json?prefix=profile'),
    ru: () => import(/* webpackChunkName: "locale-profile-ru" */ '../../../../../locales/ru.json?prefix=profile'),
    es: () => import(/* webpackChunkName: "locale-profile-es" */ '../../../../../locales/es.json?prefix=profile'),
    de: () => import(/* webpackChunkName: "locale-profile-de" */ '../../../../../locales/de.json?prefix=profile'),
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
