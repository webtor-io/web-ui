// Node module-customization hooks that let `node --test` load the player's
// JSX. Registered by ./register.mjs, which `npm test` passes as --import.
//
// Three things stand between Node and assets/src/js/lib/player/Player.jsx,
// and each hook below answers exactly one of them:
//
//   1. Node has no idea what .jsx is. `load` transpiles it with @babel/core
//      through the repo's own babel.config.json — the same presets webpack
//      uses, so the tests run the transform the bundle ships, and a preset
//      change reaches both at once. `caller.supportsStaticESM` is what
//      keeps preset-env from rewriting the modules to CommonJS (babel-loader
//      sets the same flag for the same reason); without it the transpiled
//      source would be CJS handed back as `format: 'module'`.
//   2. The source imports './Controls' without an extension, which webpack
//      resolves (`resolve: { fullySpecified: false }`) and Node does not.
//      `resolve` retries a failed relative specifier with the extensions
//      webpack would have tried.
//   3. Player.jsx imports a stylesheet for its side effect. There is no
//      styling in a test, so `load` answers .css with an empty module rather
//      than letting Node refuse the extension.
//
// Deliberately NOT a general-purpose loader: it covers the specifiers this
// repo actually writes. A miss is a resolution error naming the file, not a
// silent empty module.
import { existsSync } from 'node:fs';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { transformAsync } from '@babel/core';

// The repo root, four levels up from assets/src/js/test.
const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../..');
const BABEL_CONFIG = path.join(ROOT, 'babel.config.json');

// The order webpack's default `resolve.extensions` would try.
const CANDIDATES = ['.js', '.jsx', '/index.js', '/index.jsx'];

export async function resolve(specifier, context, nextResolve) {
    try {
        return await nextResolve(specifier, context);
    } catch (err) {
        // Only extensionless RELATIVE specifiers get a second chance: a bare
        // one is a package, and guessing there would mask a missing
        // dependency as a resolution that quietly picked a file.
        if (!specifier.startsWith('.') || !context.parentURL || path.extname(specifier)) throw err;
        const from = path.dirname(fileURLToPath(context.parentURL));
        for (const ext of CANDIDATES) {
            const candidate = path.resolve(from, specifier + ext);
            if (existsSync(candidate)) {
                return { url: pathToFileURL(candidate).href, format: 'module', shortCircuit: true };
            }
        }
        throw err;
    }
}

// The locale bundles are imported dynamically as
// `locales/en.json?prefix=player` — webpack's locale-filter-loader strips
// the file down to that prefix at build time. Node has neither the loader
// nor a reason to read the file: the tests assert on behaviour, and t()
// answers with the key when no bundle is loaded.
const LOCALE_JSON = /\/locales\/[a-z]{2}\.json(\?|$)/;

export async function load(url, context, nextLoad) {
    if (LOCALE_JSON.test(url)) {
        return { format: 'module', source: 'export default {};', shortCircuit: true };
    }
    if (url.endsWith('.css')) {
        // webpack turns this import into a stylesheet in the bundle; here it
        // has to be a module that exists and does nothing.
        return { format: 'module', source: 'export default {};', shortCircuit: true };
    }
    if (!url.endsWith('.jsx')) return nextLoad(url, context);

    const filename = fileURLToPath(url);
    const source = await readFile(filename, 'utf8');
    const out = await transformAsync(source, {
        filename,
        root: ROOT,
        configFile: BABEL_CONFIG,
        babelrc: false,
        // Inline maps so a stack trace from a failing test points at the
        // line in the .jsx, not at the transpiled output.
        sourceMaps: 'inline',
        caller: {
            name: 'node-test-jsx-hooks',
            supportsStaticESM: true,
            supportsDynamicImport: true,
            supportsTopLevelAwait: true,
            supportsImportMeta: true,
        },
    });
    return { format: 'module', source: out.code, shortCircuit: true };
}
