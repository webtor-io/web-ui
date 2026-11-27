// Mirrors helpers.Bytes (the template's bitsForHumans): base 1024, one
// decimal below 10, so a size counted in the browser renders exactly like
// one the server rendered next to it.
export function humanBytes(n) {
    if (n < 10) return `${n} B`;
    // Base 1024 with KB/MB/GB labels — the same arithmetic as helpers.Bytes on
    // the server (and as Chrome's own download UI). 1000 here used to make
    // the two differ by 5–7% on the same bytes.
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB'];
    const e = Math.floor(Math.log(n) / Math.log(1024));
    const val = Math.floor((n / Math.pow(1024, e)) * 10 + 0.5) / 10;
    return `${val < 10 ? val.toFixed(1) : Math.round(val)} ${units[e]}`; // no-break space, as helpers.Bytes
}
