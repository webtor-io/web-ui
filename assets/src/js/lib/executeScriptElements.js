const invokedScripts = {};
let loaded = false;
function getScriptName(script) {
    const src = script.getAttribute('src');
    if (src) {
        return src;
    }
    return;
}
// https://stackoverflow.com/a/69190644
// `skip`: a subtree whose scripts are NOT to be run -- content that is already
// live and was only moved (loadAsyncView.js activateViews).
function executeScriptElements(containerElement, skip = null) {
    const scriptElements = containerElement.querySelectorAll('script');

    Array.from(scriptElements).filter((el) => !(skip && skip.contains(el))).forEach((scriptElement) => {
        const name = getScriptName(scriptElement);
        if (name) {
            if (invokedScripts[name]) {
                return;
            } else {
                invokedScripts[name] = true;
            }
        }

        const clonedElement = document.createElement('script');
        clonedElement.async = false;

        Array.from(scriptElement.attributes).forEach((attribute) => {
            clonedElement.setAttribute(attribute.name, attribute.value);
        });

        clonedElement.text = scriptElement.text;

        scriptElement.parentNode.replaceChild(clonedElement, scriptElement);
    });
}

if (!loaded) {
    loaded = true;
    window.addEventListener('load', (event) => {
        const scripts = document.querySelectorAll('script');
        for (const s of scripts) {
            const name = getScriptName(s);
            if (!name) continue;
            invokedScripts[name] = true;
        }
    });
}

export default executeScriptElements;
