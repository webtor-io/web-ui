// Reading a stylesheet in a test, and the WCAG contrast of what it paints:
// for guards that hold a colour, an opacity or a size to the rule it must
// keep (lib/statusStyle.test.js). No browser: the values are the file's own.
import { readFileSync } from 'node:fs';

// Every rule of the file, nested at-rules followed: { sel, at, decls } --
// `at` the enclosing at-rules' preludes, outermost first ([] at the top
// level), `decls` the declarations ({ property: value }; `@apply` lines are
// kept whole under '@apply').
export function cssRules(file) {
    const css = readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
    const out = [];
    const heads = [];
    let buf = '';
    for (const ch of css) {
        if (ch === '{') {
            // After a statement (`@config '…';`), not with it.
            heads.push(buf.slice(buf.lastIndexOf(';') + 1).trim().replace(/\s+/g, ' '));
            buf = '';
        } else if (ch === '}') {
            const sel = heads.pop();
            if (sel !== undefined && !sel.startsWith('@') && buf.trim()) {
                const decls = {};
                for (const d of buf.split(';')) {
                    const s = d.trim();
                    if (s.startsWith('@apply ')) decls['@apply'] = s.slice(7).trim();
                    const i = s.indexOf(':');
                    if (i > 0 && !s.startsWith('@')) decls[s.slice(0, i).trim()] = s.slice(i + 1).trim();
                }
                out.push({ sel, at: heads.filter((h) => h.startsWith('@')), decls });
            }
            buf = '';
        } else {
            buf += ch;
        }
    }
    return out;
}

// The declarations of the rule whose selector list has exactly `sel`, at the
// top level -- or inside at-rules whose preludes contain each of `at`, in
// order. undefined when there is none.
export function findRule(rules, sel, at = []) {
    const r = rules.find((x) => x.sel.split(',').map((s) => s.trim()).includes(sel)
        && x.at.length === at.length && at.every((a, i) => x.at[i].includes(a)));
    return r && r.decls;
}

export const hex = (h) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));

// "#rrggbb" or "rgb(a)(r, g, b[, a])": { rgb, a }.
export function color(s) {
    if (s.startsWith('#')) return { rgb: hex(s), a: 1 };
    const [r, g, b, a = 1] = s.slice(s.indexOf('(') + 1, -1).split(',').map((x) => parseFloat(x));
    return { rgb: [r, g, b], a };
}

// `fg` painted at alpha `a` over the opaque `under`.
export const over = (under, fg, a = 1) => fg.map((c, i) => a * c + (1 - a) * under[i]);

const lum = (rgb) => {
    const lin = (c) => { const v = c / 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
    const [r, g, b] = rgb.map(lin);
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};

// The WCAG 2 contrast ratio of two opaque colours.
export const contrast = (a, b) => {
    const [x, y] = [lum(a), lum(b)];
    return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
};
