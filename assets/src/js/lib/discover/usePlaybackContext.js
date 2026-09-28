import { useEffect, useMemo, useState } from 'preact/hooks';
import { playbackContext } from './playback';

// usePlaybackContext: what the stream modal's video switches go by
// (playback.js playbackContext) -- the transcoder's answer on the page and
// the browser's own, from the same probe the page declares with. Asked of
// every browser, not only of those taking part: the HEVC and HDR switches
// follow what the browser decodes. The probe runs in the background (its
// module is a lazy chunk) and is started here, on mount; the context is
// gathered again once it answers. Until then, and where it never does, the
// switches read "not answered", never "does not decode". A cached answer of
// this browser counts at once.
//
// `dd` is decode-declaration.js (startProbe, decodedTokens, declaredTokens,
// takesPart), passed in so a test can answer for the browser.
export function usePlaybackContext(win, dd) {
    const [answers, setAnswers] = useState(0);
    // Once per mount: the page and the module do not change under it, and a
    // probe asked again on every render would render again on every answer.
    // An answer after unmount needs no guard: preact renders nothing for an
    // unmounted component (src/component.js renderComponent; unmount clears
    // _parentDom).
    useEffect(() => {
        try {
            Promise.resolve(dd.startProbe(win)).then(() => setAnswers((n) => n + 1), () => {});
        } catch (e) {
            // No probe: the switches stay as for an unanswered one.
        }
    }, []);
    return useMemo(() => playbackContext(win, dd), [win, dd, answers]);
}
