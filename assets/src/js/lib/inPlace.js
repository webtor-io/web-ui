// Writing into server-rendered nodes, only what changed: the status badge
// (lib/statusBadge.js) and the transfer status block (lib/transferStatus.js)
// never re-create markup, so an update is a text or an attribute of a node
// that stays. Pure functions: safe to have twice in two bundles (CLAUDE.md,
// shared JS state).

// A slot's text is changed in its own text node when it has one, so an
// update is a characterData change and not a new child.
export function text(el, s) {
    if (!el || el.textContent === s) return;
    const only = el.childNodes.length === 1 ? el.firstChild : null;
    if (only && only.nodeType === 3) only.data = s;
    else el.textContent = s;
}

// attr sets an attribute to v (true: present and empty), removes it for
// null / undefined / false, and touches nothing when it already is so.
export function attr(el, name, v) {
    if (!el) return;
    if (v === null || v === undefined || v === false) {
        if (el.hasAttribute(name)) el.removeAttribute(name);
        return;
    }
    const s = v === true ? '' : String(v);
    if (el.getAttribute(name) !== s) el.setAttribute(name, s);
}

export function hide(el, hidden) {
    if (el && el.hidden !== hidden) el.hidden = hidden;
}
