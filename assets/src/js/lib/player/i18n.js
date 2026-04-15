import { makeI18n, getLang } from '../i18n';

const loaders = {
    en: () => import(/* webpackChunkName: "locale-player-en" */ '../../../../../locales/en.json?prefix=player'),
    ru: () => import(/* webpackChunkName: "locale-player-ru" */ '../../../../../locales/ru.json?prefix=player'),
    es: () => import(/* webpackChunkName: "locale-player-es" */ '../../../../../locales/es.json?prefix=player'),
    de: () => import(/* webpackChunkName: "locale-player-de" */ '../../../../../locales/de.json?prefix=player'),
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
