import { useState, useRef, useEffect, useCallback } from 'preact/hooks';

export function SearchBar({ onSearch, onExit, isSearchMode, initialQuery }) {
    const [value, setValue] = useState('');
    const timerRef = useRef(null);
    const inputRef = useRef(null);

    // Sync input value with initialQuery (restored from URL or popstate)
    useEffect(() => {
        if (initialQuery != null) {
            setValue(initialQuery);
        }
    }, [initialQuery]);

    // Reset input when exiting search mode
    useEffect(() => {
        if (!isSearchMode) {
            setValue('');
        }
    }, [isSearchMode]);

    const handleInput = useCallback((e) => {
        const v = e.target.value;
        setValue(v);
        clearTimeout(timerRef.current);
        timerRef.current = setTimeout(() => onSearch(v), 400);
    }, [onSearch]);

    const handleKeyDown = useCallback((e) => {
        if (e.key === 'Escape') {
            setValue('');
            onExit();
            inputRef.current?.blur();
        }
    }, [onExit]);

    const handleClear = useCallback(() => {
        setValue('');
        onExit();
    }, [onExit]);

    return (
        <div class="relative mb-6">
            <div class="flex items-center bg-w-surface border border-w-line rounded-xl focus-within:border-w-cyan/50 transition-colors">
                <svg class="w-5 h-5 text-w-muted ml-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <circle cx="11" cy="11" r="8"></circle>
                    <path d="m21 21-4.3-4.3"></path>
                </svg>
                <input
                    ref={inputRef}
                    type="text"
                    placeholder="Search movies and series..."
                    class="w-full bg-transparent border-none outline-none px-3 py-3 text-w-text placeholder:text-w-muted text-sm"
                    autocomplete="off"
                    value={value}
                    onInput={handleInput}
                    onKeyDown={handleKeyDown}
                />
                {value.length > 0 && (
                    <button class="mr-3 p-1 text-w-muted hover:text-w-text transition-colors flex-shrink-0" type="button" onClick={handleClear}>
                        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M18 6 6 18M6 6l12 12"></path>
                        </svg>
                    </button>
                )}
            </div>
        </div>
    );
}
