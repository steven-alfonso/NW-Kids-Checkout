// Height animation for the collapsible search panel. The panel starts at
// height 0 from the page's inline CSS; expanding measures the content after the
// class flip, matching the vanilla implementation's scrollHeight dance.
export function searchPanel(node, expanded) {
    let current = Boolean(expanded);

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

    function onResize() {
        if (current) node.style.height = `${node.scrollHeight}px`;
    }

    if (current) expand();
    window.addEventListener('resize', onResize);

    return {
        update(value) {
            const next = Boolean(value);
            if (next === current) return;
            current = next;
            if (current) expand();
            else collapse();
        },
        destroy() {
            window.removeEventListener('resize', onResize);
        }
    };
}
