// Webpack loader that filters locale JSON to keys matching a prefix.
// Usage: import('../../locales/en.json?prefix=discover')
// Result: only keys starting with "discover." are included in the bundle.
//
// Also strips "@"-prefixed keys (ARB-style translator context metadata, e.g.
// "@support.work": "…") so they never reach client bundles. The Go server
// does the same in services/i18n/i18n.go via unmarshalStrippingAtKeys.
module.exports = function(source) {
    const params = new URLSearchParams(this.resourceQuery);
    const prefix = params.get('prefix');
    if (!prefix) return source;

    const all = JSON.parse(source);
    const filtered = {};
    const dot = prefix + '.';
    for (const [key, value] of Object.entries(all)) {
        if (key.startsWith('@')) continue;
        if (key.startsWith(dot)) {
            filtered[key] = value;
        }
    }
    return `export default ${JSON.stringify(filtered)};`;
};
