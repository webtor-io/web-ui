// AI suggestion chips shown above the free-form query input.
// Clicking a chip sends its pre-expanded `query` string to the recommender.

export function AIChipsRow({ chips, onSelect, disabled }) {
    if (!chips || chips.length === 0) return null;
    return (
        <div class="flex flex-wrap gap-2 mt-3">
            {chips.map(chip => (
                <button
                    key={chip.id}
                    type="button"
                    disabled={disabled}
                    onClick={() => onSelect(chip)}
                    class="inline-flex items-center gap-1.5 rounded-full bg-w-cyan/10 text-w-cyan border border-w-cyan/30 hover:bg-w-cyan/20 hover:border-w-cyan/60 transition-colors px-3.5 py-1.5 text-sm font-medium cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                    title={chip.query}
                >
                    {chip.icon && <span class="text-base leading-none">{chip.icon}</span>}
                    <span>{chip.label}</span>
                </button>
            ))}
        </div>
    );
}
