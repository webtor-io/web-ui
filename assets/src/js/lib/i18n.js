// Client-side i18n helper.
// Translations live in locales/*.json (same files used by Go server).
// Webpack locale-filter-loader extracts only keys matching ?prefix=,
// so only the relevant section ships to the client (~2KB vs 40KB).
//
// Usage:
//   import { makeI18n, getLang } from '../../lib/i18n';
//
//   const loaders = {
//       en: () => import('../../../../locales/en.json?prefix=discover'),
//       ru: () => import('../../../../locales/ru.json?prefix=discover'),
//       es: () => import('../../../../locales/es.json?prefix=discover'),
//       de: () => import('../../../../locales/de.json?prefix=discover'),
//   };
//   const mod = await (loaders[getLang()] || loaders.en)();
//   const { t, tf } = makeI18n(mod.default || mod);

const SUPPORTED = ['en', 'ru', 'es', 'de'];

export function getLang() {
    const lang = document.documentElement.lang || 'en';
    return SUPPORTED.includes(lang) ? lang : 'en';
}

// Prefix a path with the current language (matches Go's LangURL).
// English (default) has no prefix; other languages get /{lang}{path}.
export function langPath(path) {
    const lang = getLang();
    if (lang === 'en') return path;
    return '/' + lang + path;
}

export function makeI18n(messages) {
    function t(key) {
        return messages[key] || key;
    }

    function tf(key, ...args) {
        let msg = t(key);
        for (const a of args) {
            msg = msg.replace('%v', a);
        }
        return msg;
    }

    return { t, tf };
}
