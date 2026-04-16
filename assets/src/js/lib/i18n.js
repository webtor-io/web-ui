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
//       // ...one entry per code in SUPPORTED below.
//   };
//   const mod = await (loaders[getLang()] || loaders.en)();
//   const { t, tf } = makeI18n(mod.default || mod);

// THE single source of truth for client-side supported locales.
//
// Mirrors services/i18n/i18n.go SupportedLangs (Go server). Anything not
// listed here gets clamped to 'en' by getLang() — every other JS module
// (per-area i18n loaders, aiClient.js) imports SUPPORTED from here so a
// new locale needs to be added in exactly one place client-side.
//
// Cross-language sync: when adding a locale, edit BOTH this constant AND
// services/i18n/i18n.go SupportedLangs. There's no automated check —
// drift would mean either the Go server renders an unsupported lang in
// <html lang> (silently clamped to 'en' by getLang) or a JS-known lang
// the server doesn't know about (404 on /xx/ paths).
export const SUPPORTED = ['en', 'ru', 'es', 'de', 'fr', 'pt', 'it', 'pl', 'tr', 'nl', 'cs'];

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
