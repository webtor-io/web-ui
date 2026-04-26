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

// THE single source of truth for client-side supported locales — derived
// at webpack build time from the actual locale files on disk.
//
// `__SUPPORTED_LOCALES__` is injected by webpack.config.js DefinePlugin
// (see discoverSupportedLocales there). It mirrors the Go server's
// services/i18n.New(), which scans the same `locales/*.json` files at
// startup. Drop a `xx.json` file and both sides auto-pick it up — no
// hardcoded list to maintain anywhere.
//
// Display order: DefaultLang ("en") first, others alphabetical.
//
// eslint-disable-next-line no-undef
export const SUPPORTED = __SUPPORTED_LOCALES__;

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

// Strip a leading /{lang} prefix from a pathname so it can be compared
// against canonical (lang-less) paths like '/discover' or '/lib/'. Useful
// for popstate filters that need to match regardless of which locale the
// user is viewing. English paths have no prefix so they pass through.
export function stripLangPrefix(pathname) {
    const m = pathname.match(/^\/([a-z]{2})(\/|$)/);
    if (m && SUPPORTED.includes(m[1]) && m[1] !== 'en') {
        // Drop "/<lang>"; keep the trailing slash so '/ru' → '/' (not '').
        return pathname.slice(3) || '/';
    }
    return pathname;
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
