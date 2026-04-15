import { useRef, useEffect, useState, useCallback } from 'preact/hooks';
import { t } from '../i18n';

export function RatingDialog({ currentRating, onRate, onUnrate, onClose }) {
    const dialogRef = useRef(null);
    const [selected, setSelected] = useState(currentRating || 0);
    const [hovered, setHovered] = useState(0);

    useEffect(() => {
        setSelected(currentRating || 0);
    }, [currentRating]);

    useEffect(() => {
        const dialog = dialogRef.current;
        if (dialog && !dialog.open) {
            dialog.showModal();
        }
        return () => {
            if (dialog && dialog.open) dialog.close();
        };
    }, []);

    const handleBackdropClick = useCallback((e) => {
        if (e.target === dialogRef.current) onClose();
    }, [onClose]);

    const handleSubmit = useCallback((e) => {
        e.preventDefault();
        if (selected > 0) onRate(selected);
    }, [selected, onRate]);

    const displayVal = hovered || selected;

    return (
        <dialog ref={dialogRef} class="modal" onClick={handleBackdropClick}>
            <div class="modal-box max-w-sm bg-w-card border border-w-line/50 rounded-2xl" onClick={e => e.stopPropagation()}>
                <div class="flex flex-col items-center pt-2 pb-3 gap-1">
                    <div class="flex gap-0.5">
                        {Array.from({ length: 10 }, (_, i) => i + 1).map(val => {
                            const isActive = val <= (hovered || selected);
                            const isBright = hovered ? val === hovered : val === selected;
                            return (
                                <button
                                    key={val}
                                    type="button"
                                    class="mask mask-star-2 bg-yellow-400 w-6 h-6 cursor-pointer transition-opacity"
                                    style={{ opacity: isActive ? (isBright ? 1 : 0.7) : 0.25 }}
                                    onClick={() => { setSelected(val); setHovered(0); }}
                                    onMouseEnter={() => setHovered(val)}
                                    onMouseLeave={() => setHovered(0)}
                                />
                            );
                        })}
                    </div>
                    <span class="text-sm font-semibold text-yellow-400 h-5">
                        {displayVal > 0 ? `${displayVal}/10` : ''}
                    </span>
                </div>
                <div class="modal-action mt-0 justify-center">
                    <button type="button" class="btn btn-soft" onClick={handleSubmit} disabled={selected === 0}>
                        {t('discover.rate')}
                    </button>
                    {currentRating > 0 && (
                        <button type="button" class="btn btn-ghost text-w-sub" onClick={onUnrate}>
                            {t('discover.dropRating')}
                        </button>
                    )}
                    <button type="button" class="btn btn-ghost border border-w-line text-w-sub hover:border-w-pink hover:text-base-content" onClick={onClose}>
                        {t('discover.cancel')}
                    </button>
                </div>
            </div>
        </dialog>
    );
}
