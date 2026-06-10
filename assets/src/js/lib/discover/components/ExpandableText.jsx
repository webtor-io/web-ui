import { useRef, useState, useLayoutEffect } from 'preact/hooks';
import { CLAMP_LINE_HEIGHT, findTextCut, trimTextCut } from '../../textClamp';

// ExpandableText — text block clamped to `lines` line-heights with a
// clickable "…" rendered INLINE at the cut point, like a link. Clicking
// expands the full text; an inline "↑" at the end collapses it back.
//
// Inline placement needs a real text cut, not CSS clamping: the cut
// index comes from findTextCut (lib/textClamp.js), measured on an
// invisible clone of the paragraph so Preact-managed DOM is never
// mutated directly. The em math assumes leading-relaxed — pass a
// matching textClass.
export function ExpandableText({ text, lines = 3, textClass = '', class: cls = '' }) {
    const ref = useRef(null);
    const [expanded, setExpanded] = useState(false);
    const [cut, setCut] = useState(null);

    useLayoutEffect(() => {
        setExpanded(false);
        const el = ref.current;
        if (!el || !el.parentNode || !text) { setCut(null); return; }
        let raf = 0;
        let tries = 0;
        const measure = () => {
            if (!el.parentNode) return;
            // Zero width means we're inside a not-yet-open <dialog>
            // (display:none until the parent's showModal() effect runs) —
            // every measurement would be meaningless. Retry on subsequent
            // frames; give up after ~1s and stay clamped without a toggle.
            if (el.clientWidth === 0) {
                if (++tries <= 60) raf = requestAnimationFrame(measure);
                return;
            }
            const meas = el.cloneNode(false);
            meas.style.position = 'absolute';
            meas.style.visibility = 'hidden';
            meas.style.width = el.clientWidth + 'px';
            meas.style.maxHeight = `calc(${lines} * ${CLAMP_LINE_HEIGHT}em)`;
            meas.style.overflow = 'hidden';
            el.parentNode.insertBefore(meas, el);
            setCut(findTextCut(meas, text));
            meas.remove();
        };
        measure();
        return () => cancelAnimationFrame(raf);
    }, [text, lines]);

    if (!text) return null;

    const truncated = !expanded && cut != null;
    const visible = truncated ? trimTextCut(text, cut) : text;
    const toggle = (e) => { e.stopPropagation(); setExpanded(v => !v); };

    return (
        <p
            ref={ref}
            class={`${textClass} ${cls} ${expanded ? '' : 'overflow-hidden'}`}
            style={expanded ? undefined : { maxHeight: `calc(${lines} * ${CLAMP_LINE_HEIGHT}em)` }}
        >
            {visible}
            {cut != null && (
                <>
                    {' '}
                    <button
                        type="button"
                        class="text-w-cyan hover:underline cursor-pointer bg-transparent p-0 font-bold"
                        onClick={toggle}
                    >{expanded ? '↑' : '…'}</button>
                </>
            )}
        </p>
    );
}
