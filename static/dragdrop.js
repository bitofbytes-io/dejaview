// Drag and drop functionality for movie reordering within groups
(function() {
    'use strict';

    // Initialize drag and drop for all sortable grids
    function initDragDrop() {
        document.querySelectorAll('.sortable-grid').forEach(initGrid);
    }

    function initGrid(grid) {
        const groupNum = grid.dataset.group;
        if (!groupNum) return;

        let draggedItem = null;

        grid.querySelectorAll('.draggable-item').forEach(item => {
            if (item.dataset.dndBound === 'true') {
                return;
            }
            item.dataset.dndBound = 'true';

            item.setAttribute('draggable', 'true');

            const handle = item.querySelector('.drag-handle');
            if (handle) {
                handle.setAttribute('draggable', 'true');
            }

            item.querySelectorAll('a, img').forEach(el => {
                el.setAttribute('draggable', 'false');
            });
        });

        if (grid.dataset.dndGridBound === 'true') {
            return;
        }
        grid.dataset.dndGridBound = 'true';

        grid.addEventListener('dragstart', function(e) {
            const targetItem = e.target.closest('.draggable-item');
            if (!targetItem || !grid.contains(targetItem)) return;

            draggedItem = targetItem;
            draggedItem.classList.add('dragging');

            if (e.dataTransfer) {
                e.dataTransfer.effectAllowed = 'move';
                e.dataTransfer.setData('text/plain', targetItem.dataset.entryId || '');
                if (e.target.classList && e.target.classList.contains('drag-handle')) {
                    e.dataTransfer.setDragImage(targetItem, 20, 20);
                }
            }
        });

        grid.addEventListener('dragend', function(e) {
            if (!draggedItem) return;

            draggedItem.classList.remove('dragging');
            draggedItem = null;

            // Save the new order
            saveOrder(grid, groupNum);
        });

        grid.addEventListener('dragover', function(e) {
            if (!draggedItem) return;

            e.preventDefault();
            e.dataTransfer.dropEffect = 'move';

            const overItem = e.target.closest('.draggable-item');
            if (!overItem || overItem === draggedItem) return;

            const rect = overItem.getBoundingClientRect();
            const midX = rect.left + rect.width / 2;
            const midY = rect.top + rect.height / 2;
            const afterElement = (e.clientX > midX || e.clientY > midY);

            if (afterElement) {
                overItem.parentNode.insertBefore(draggedItem, overItem.nextSibling);
            } else {
                overItem.parentNode.insertBefore(draggedItem, overItem);
            }
        });

        // Handle drop on the grid itself
        grid.addEventListener('dragover', function(e) {
            e.preventDefault();
        });

        grid.addEventListener('drop', function(e) {
            e.preventDefault();
        });
    }

    function saveOrder(grid, groupNum) {
        const items = grid.querySelectorAll('.draggable-item');
        const entryIds = Array.from(items).map(item => item.dataset.entryId);

        fetch('/api/groups/' + groupNum + '/reorder', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ entry_ids: entryIds })
        })
        .then(response => {
            if (!response.ok) {
                // 409: the group changed since the page loaded.
                orderNotSaved(response.status === 409
                    ? 'This group changed, so here is the latest order'
                    : 'Failed to save order');
                return;
            }
            showToast('Order updated!', 'success');
        })
        .catch(error => {
            console.error('Error saving order:', error);
            orderNotSaved('Failed to save order');
        });
    }

    // orderNotSaved reports the failure and reloads the groups, so the page
    // does not keep showing an order the server did not save.
    function orderNotSaved(message) {
        showToast(message, 'error');
        document.body.dispatchEvent(new CustomEvent('refreshGroups'));
    }

    function showToast(message, type) {
        document.body.dispatchEvent(new CustomEvent('showToast', {
            detail: { message: message, type: type }
        }));
    }

    // Initialize on page load
    document.addEventListener('DOMContentLoaded', initDragDrop);

    // Reinitialize after HTMX content swaps
    document.body.addEventListener('htmx:afterSettle', initDragDrop);
})();
