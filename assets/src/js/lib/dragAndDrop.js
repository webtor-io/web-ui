/**
 * Drag and Drop Library for reorderable lists
 * Supports both desktop and mobile touch events
 */

export default class DragAndDrop {
    constructor(options) {
        this.listSelector = options.listSelector;
        this.itemSelector = options.itemSelector;
        this.orderInputSelector = options.orderInputSelector;
        this.dataAttribute = options.dataAttribute;
        this.onUpdateOrder = options.onUpdateOrder || null;
        this.onHandleMove = options.onHandleMove || null;
        
        this.list = document.querySelector(this.listSelector);
        this.orderInput = document.querySelector(this.orderInputSelector);
        this.dragEl = null;
        this.touchStartY = 0;
        this.touchCurrentY = 0;
        this.isDragging = false;
        
        if (this.list) {
            this.init();
        }
    }
    
    init() {
        this.bindEvents();
    }
    
    updateOrder() {
        const items = this.list.querySelectorAll(this.itemSelector);
        const vals = [];
        items.forEach((item) => {
            const value = item.getAttribute(this.dataAttribute);
            if (value) {
                vals.push(value);
            }
        });
        if (this.orderInput) {
            this.orderInput.value = vals.join(',');
        }
        
        // Call custom update order callback if provided
        if (this.onUpdateOrder) {
            this.onUpdateOrder(vals);
        }
    }
    
    startDrag(target) {
        this.dragEl = target;
        this.isDragging = true;
        target.style.opacity = '0.5';
        target.style.transform = 'scale(1.05)';
    }
    
    endDrag() {
        if (!this.dragEl) return;
        this.dragEl.style.opacity = '';
        this.dragEl.style.transform = '';
        this.dragEl = null;
        this.isDragging = false;
        this.updateOrder();
    }
    
    handleMove(clientY) {
        if (!this.dragEl || !this.isDragging) return;
        
        const target = document.elementFromPoint(
            this.list.getBoundingClientRect().left + 10, 
            clientY
        )?.closest(this.itemSelector);
        
        if (!target || target === this.dragEl) return;
        
        // Don't allow reordering if target is not draggable
        if (!target.draggable) return;
        
        const rect = target.getBoundingClientRect();
        const next = (clientY - rect.top) / (rect.height) > 0.5;
        
        let insertBefore = next ? target.nextSibling : target;

        this.list.insertBefore(this.dragEl, insertBefore);
        
        // Call custom handle move callback if provided
        if (this.onHandleMove) {
            this.onHandleMove(clientY, target, this.dragEl);
        }
    }
    
    bindEvents() {
        // Desktop drag and drop events
        this.list.addEventListener('dragstart', (e) => {
            const target = e.target.closest(this.itemSelector);
            if (!target || !target.draggable) return;
            this.startDrag(target);
            e.dataTransfer.effectAllowed = 'move';
        });
        
        this.list.addEventListener('dragover', (e) => {
            if (!this.dragEl) return;
            e.preventDefault();
            this.handleMove(e.clientY);
        });
        
        this.list.addEventListener('dragend', () => {
            this.endDrag();
        });
        
        // Mobile touch events
        this.list.addEventListener('touchstart', (e) => {
            const target = e.target.closest(this.itemSelector);
            if (!target || !target.draggable) return;
            this.touchStartY = e.touches[0].clientY;
            this.touchCurrentY = this.touchStartY;
            
            // Start drag after a small delay to distinguish from scrolling
            setTimeout(() => {
                if (Math.abs(this.touchCurrentY - this.touchStartY) < 10) {
                    this.startDrag(target);
                    e.preventDefault();
                }
            }, 150);
        }, { passive: false });
        
        this.list.addEventListener('touchmove', (e) => {
            this.touchCurrentY = e.touches[0].clientY;
            if (this.isDragging) {
                e.preventDefault();
                this.handleMove(e.touches[0].clientY);
            }
        }, { passive: false });
        
        this.list.addEventListener('touchend', (e) => {
            if (this.isDragging) {
                e.preventDefault();
                this.endDrag();
            }
            this.touchStartY = 0;
            this.touchCurrentY = 0;
        }, { passive: false });
    }
}