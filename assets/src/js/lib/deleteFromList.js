/**
 * DeleteFromList Library for managing item deletion in lists
 * Handles tracking deleted items, updating order, and showing empty states
 */

export default class DeleteFromList {
    constructor(options) {
        this.listSelector = options.listSelector;
        this.itemSelector = options.itemSelector;
        this.deleteButtonSelector = options.deleteButtonSelector;
        this.dataAttribute = options.dataAttribute;
        this.deletedInputId = options.deletedInputId;
        this.orderInputId = options.orderInputId;
        this.emptyStateId = options.emptyStateId;
        this.confirmMessage = options.confirmMessage;
        this.umamiEventName = options.umamiEventName;
        this.itemNameSelector = options.itemNameSelector || null;
        
        this.deletedItems = new Set();
        this.deletedInput = document.getElementById(this.deletedInputId);
        this.list = document.getElementById(this.listSelector.replace('#', ''));
        
        if (this.list) {
            this.init();
        }
    }
    
    init() {
        this.bindEvents();
    }
    
    handleDelete(event) {
        const button = event.target.closest(this.deleteButtonSelector);
        if (!button) return;
        
        const itemId = button.getAttribute(this.dataAttribute);
        const item = button.closest(this.itemSelector);
        
        // Get item name for confirmation message
        let itemName = '';
        if (this.itemNameSelector) {
            const nameElement = item.querySelector(this.itemNameSelector);
            if (nameElement) {
                itemName = nameElement.textContent.trim();
            }
        }
        
        // Generate confirmation message
        const message = typeof this.confirmMessage === 'function' 
            ? this.confirmMessage(itemName) 
            : this.confirmMessage;
        
        if (confirm(message)) {
            // Track delete event with Umami
            if (window.umami && this.umamiEventName) {
                window.umami.track(this.umamiEventName);
            }
            
            // Add to deleted items set
            this.deletedItems.add(itemId);
            
            // Update hidden input
            if (this.deletedInput) {
                this.deletedInput.value = Array.from(this.deletedItems).join(',');
            }
            
            // Remove the element from DOM
            item.remove();
            
            // Update the order input to remove deleted item
            const orderInput = document.getElementById(this.orderInputId);
            if (orderInput) {
                const currentOrder = orderInput.value.split(',').filter(id => !this.deletedItems.has(id));
                orderInput.value = currentOrder.join(',');
            }
            
            // Show empty state if no items left
            const remainingItems = this.list.querySelectorAll(`${this.itemSelector}[${this.dataAttribute}]`);
            if (remainingItems.length === 0) {
                const emptyState = document.getElementById(this.emptyStateId);
                if (emptyState) {
                    emptyState.classList.remove('hidden');
                }
            }
        }
    }
    
    bindEvents() {
        // Add event listener to the list using event delegation
        this.list.addEventListener('click', (event) => this.handleDelete(event));
    }
}
