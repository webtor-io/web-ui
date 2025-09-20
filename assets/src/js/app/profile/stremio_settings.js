import av from '../../lib/av';

av(async function(){
    const list = this.querySelector('#resolution-list');
    if (!list) return;
    const orderInput = this.querySelector('#resolution_order');
    let dragEl = null;
    let touchStartY = 0;
    let touchCurrentY = 0;
    let isDragging = false;
    
    function updateOrder(){
        const items = list.querySelectorAll('.resolution-item');
        const vals = [];
        items.forEach(function(it){ vals.push(it.getAttribute('data-resolution')); });
        orderInput.value = vals.join(',');
    }
    
    function startDrag(target) {
        dragEl = target;
        isDragging = true;
        target.style.opacity = '0.5';
        target.style.transform = 'scale(1.05)';
    }
    
    function endDrag() {
        if (!dragEl) return;
        dragEl.style.opacity = '';
        dragEl.style.transform = '';
        dragEl = null;
        isDragging = false;
        updateOrder();
    }
    
    function handleMove(clientY) {
        if (!dragEl || !isDragging) return;
        const target = document.elementFromPoint(list.getBoundingClientRect().left + 10, clientY)?.closest('.resolution-item');
        if (!target || target === dragEl) return;
        const rect = target.getBoundingClientRect();
        const next = (clientY - rect.top)/(rect.height) > 0.5;
        list.insertBefore(dragEl, next ? target.nextSibling : target);
    }
    
    // Desktop drag and drop events
    list.addEventListener('dragstart', function(e){
        const target = e.target.closest('.resolution-item');
        if (!target) return;
        startDrag(target);
        e.dataTransfer.effectAllowed = 'move';
    });
    
    list.addEventListener('dragover', function(e){
        if (!dragEl) return;
        e.preventDefault();
        handleMove(e.clientY);
    });
    
    list.addEventListener('dragend', function(){
        endDrag();
    });
    
    // Mobile touch events
    list.addEventListener('touchstart', function(e){
        const target = e.target.closest('.resolution-item');
        if (!target) return;
        touchStartY = e.touches[0].clientY;
        touchCurrentY = touchStartY;
        
        // Start drag after a small delay to distinguish from scrolling
        setTimeout(() => {
            if (Math.abs(touchCurrentY - touchStartY) < 10) {
                startDrag(target);
                e.preventDefault();
            }
        }, 150);
    }, { passive: false });
    
    list.addEventListener('touchmove', function(e){
        touchCurrentY = e.touches[0].clientY;
        if (isDragging) {
            e.preventDefault();
            handleMove(e.touches[0].clientY);
        }
    }, { passive: false });
    
    list.addEventListener('touchend', function(e){
        if (isDragging) {
            e.preventDefault();
            endDrag();
        }
        touchStartY = 0;
        touchCurrentY = 0;
    }, { passive: false });
});

export {}