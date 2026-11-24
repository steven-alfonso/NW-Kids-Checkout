// The Go handler replaces the placeholder inside #checkouts-menu-source with
// auth-aware links. Read that markup and remove the source node so the menu ids
// exist exactly once in the document.
export function readMenuHtml(documentRef = document) {
    const source = documentRef.getElementById('checkouts-menu-source');
    if (!source) return '';
    const html = source.innerHTML;
    source.remove();
    return html;
}
