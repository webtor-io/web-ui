// One registration per script: lib/asyncView.js keys the init on the
// script URL and ignores a second av() from the same file (see the note in
// app/resource/get.js). Compose one callback instead of calling this twice.
export default function (init, destroy = null) {
    const target = document.currentScript.parentElement;
    window.av = window.av || [];
    window.av.push([target, init, destroy]);
}