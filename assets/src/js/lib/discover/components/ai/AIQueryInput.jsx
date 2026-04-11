import { useState, useCallback } from 'preact/hooks';
import { currentLocale } from '../../aiClient';

// Free-form query input used for both the initial recommend call and the
// refine bar. The input and the submit button share a single rounded
// container for a unified "search bar" feel — think Google search, not
// DaisyUI "input + button" pair.
//
// Button copy is intentionally playful:
//   initial → "Pitch me" / "Предложи"   (as if asking an agent for a pitch)
//   refine  → "Remix"    / "Пересобрать" (retake the same list with new criteria)

const PLACEHOLDERS = {
    initial: {
        en: 'Describe what you want to watch…',
        ru: 'Опиши, что хочешь посмотреть…',
    },
    refine: {
        en: 'More thrillers, fewer sequels…',
        ru: 'Добавь триллеров, убери продолжения…',
    },
};

const SUBMIT_LABEL = {
    initial: { en: 'Pitch me', ru: 'Предложи' },
    refine:  { en: 'Remix',    ru: 'Пересобрать' },
};

export function AIQueryInput({ mode = 'initial', initialValue = '', disabled, onSubmit, maxLength = 500 }) {
    const [value, setValue] = useState(initialValue);
    const locale = currentLocale();
    const trimmed = value.trim();

    const handleSubmit = useCallback((e) => {
        e?.preventDefault?.();
        if (!trimmed || disabled) return;
        onSubmit(trimmed);
    }, [trimmed, disabled, onSubmit]);

    return (
        <form
            onSubmit={handleSubmit}
            class="flex items-stretch w-full rounded-xl overflow-hidden border border-w-line/60 bg-w-bg/60 focus-within:border-w-cyan transition-colors shadow-sm"
        >
            <input
                type="text"
                value={value}
                onInput={(e) => setValue(e.target.value)}
                placeholder={PLACEHOLDERS[mode][locale]}
                maxLength={maxLength}
                disabled={disabled}
                class="flex-1 min-w-0 bg-transparent px-4 py-2.5 text-w-text placeholder:text-w-muted/70 focus:outline-none text-sm sm:text-base disabled:opacity-60"
            />
            {value.length > maxLength * 0.8 && (
                <span class="flex items-center px-2 text-[10px] text-w-muted tabular-nums border-l border-w-line/40">
                    {value.length}/{maxLength}
                </span>
            )}
            <button
                type="submit"
                disabled={disabled || !trimmed}
                class="px-4 sm:px-5 bg-w-cyan/15 hover:bg-w-cyan/25 text-w-cyan font-semibold text-sm transition-colors border-l border-w-line/60 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed whitespace-nowrap"
            >
                {SUBMIT_LABEL[mode][locale]}
            </button>
        </form>
    );
}
