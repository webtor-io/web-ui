// Shared text-clamping math for the inline "…" expanders — the Preact
// ExpandableText (Discover modal, AI recs) and the vanilla plot clamp in
// app/resource/get.js use the same cut search so the two can't drift.

// Line-height factor the height budget is computed with. Must match the
// leading-* utility on the clamped text: every call site uses
// leading-relaxed (1.625). The resource-page template hardcodes the
// product (max-h-[4.875em] = 3 × 1.625) in
// templates/views/resource/get.html — keep them in sync.
export const CLAMP_LINE_HEIGHT = 1.625;

// findTextCut returns the longest prefix of `text` that fits the
// element's height budget with room reserved for the inline "…" link
// (three ellipsis chars approximate its rendered width), or null when
// the full text already fits. `meas` must have max-height + overflow
// hidden applied; its textContent is mutated during the binary search —
// callers re-render or discard the element afterwards.
export function findTextCut(meas, text) {
    meas.textContent = text;
    const limit = meas.clientHeight;
    if (meas.scrollHeight <= limit + 1) return null;
    let lo = 0, hi = text.length;
    while (lo < hi) {
        const mid = Math.ceil((lo + hi) / 2);
        meas.textContent = text.slice(0, mid) + '………';
        if (meas.scrollHeight <= limit + 1) lo = mid;
        else hi = mid - 1;
    }
    return lo;
}

// trimTextCut slices at the cut point and drops trailing whitespace /
// punctuation so the inline "…" doesn't trail a comma or period.
export function trimTextCut(text, cut) {
    return text.slice(0, cut).replace(/[\s.,;:!?]*$/u, '');
}
