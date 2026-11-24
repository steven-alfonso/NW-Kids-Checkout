// Height animation for the collapsible search panel. The panel starts at
// height 0 from the page's inline CSS; expanding measures the content after the
// class flip, matching the vanilla implementation's scrollHeight dance.
//
// The parameter is either the open flag or `{ expanded, groupsLoaded }`. The
// group checkboxes arrive after the panel may already be open, so every update
// while expanded re-measures; otherwise the late rows are clipped by the
// panel's overflow:hidden height.
function readExpanded(value) {
    if (value && typeof value === 'object') return Boolean(value.expanded);
    return Boolean(value);
}

export function searchPanel(node, params) {
    let current = readExpanded(params);

    function expand() {
        node.classList.add('is-expanded');
        node.style.height = '0px';
        void node.offsetHeight;
        node.style.height = `${node.scrollHeight}px`;
    }

    function collapse() {
        node.classList.remove('is-expanded');
        node.style.height = `${node.scrollHeight}px`;
        void node.offsetHeight;
        node.style.height = '0px';
    }

    function measure() {
        if (current) node.style.height = `${node.scrollHeight}px`;
    }

    function onResize() {
        measure();
    }

    if (current) expand();
    window.addEventListener('resize', onResize);

    return {
        update(nextParams) {
            const next = readExpanded(nextParams);
            if (next === current) {
                measure();
                return;
            }
            current = next;
            if (current) expand();
            else collapse();
        },
        destroy() {
            window.removeEventListener('resize', onResize);
        }
    };
}
