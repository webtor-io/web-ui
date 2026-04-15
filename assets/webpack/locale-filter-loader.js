// Webpack loader that filters locale JSON to keys matching a prefix.
// Usage: import('../../locales/en.json?prefix=discover')
// Result: only keys starting with "discover." are included in the bundle.
module.exports = function(source) {
    const params = new URLSearchParams(this.resourceQuery);
    const prefix = params.get('prefix');
    if (!prefix) return source;

    const all = JSON.parse(source);
    const filtered = {};
    const dot = prefix + '.';
    for (const [key, value] of Object.entries(all)) {
        if (key.startsWith(dot)) {
            filtered[key] = value;
        }
    }
    return `export default ${JSON.stringify(filtered)};`;
};
