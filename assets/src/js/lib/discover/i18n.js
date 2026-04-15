import { makeI18n, getLang, langPath } from '../i18n';
export { langPath };

const loaders = {
    en: () => import(/* webpackChunkName: "locale-discover-en" */ '../../../../../locales/en.json?prefix=discover'),
    ru: () => import(/* webpackChunkName: "locale-discover-ru" */ '../../../../../locales/ru.json?prefix=discover'),
    es: () => import(/* webpackChunkName: "locale-discover-es" */ '../../../../../locales/es.json?prefix=discover'),
    de: () => import(/* webpackChunkName: "locale-discover-de" */ '../../../../../locales/de.json?prefix=discover'),
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
