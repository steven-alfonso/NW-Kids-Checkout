const entriesContainer = document.getElementById('entries-container');
const pageStatus = document.getElementById('page-status');

const STATUS_ORDER = ['needs-entry', 'entered', 'rejected'];
const STATUS_TO_BUCKET = {
    approved: 'needs-entry',
    pending: 'needs-entry',
    entered: 'entered',
    rejected: 'rejected'
};
const BUCKET_FETCH_STATUSES = {
    'needs-entry': ['pending', 'approved'],
    entered: ['entered'],
    rejected: ['rejected']
};
const TAB_IDS = {
    'needs-entry': 'tab-needs-entry',
    entered: 'tab-entered',
    rejected: 'tab-rejected'
};
const VIEW_IDS = {
    'needs-entry': 'view-needs-entry',
    entered: 'view-entered',
    rejected: 'view-rejected'
};
const ENTRIES_IDS = {
    'needs-entry': 'entries-needs-entry',
    entered: 'entries-entered',
    rejected: 'entries-rejected'
};
const PAGINATION_TOP_IDS = {
    'needs-entry': 'pagination-needs-entry-top',
    entered: 'pagination-entered-top',
    rejected: 'pagination-rejected-top'
};
const PAGINATION_BOTTOM_IDS = {
    'needs-entry': 'pagination-needs-entry-bottom',
    entered: 'pagination-entered-bottom',
    rejected: 'pagination-rejected-bottom'
};
const pageState = { 'needs-entry': 1, entered: 1, rejected: 1 };
const STATUS_LABELS = {
    'needs-entry': 'Needs Planning Center Entry',
    entered: 'Entered',
    rejected: 'Rejected'
};
const STATUS_BADGE_CLASS = {
    'needs-entry': 'bg-amber-100 text-amber-800',
    entered: 'bg-emerald-100 text-emerald-700',
    rejected: 'bg-red-100 text-red-700'
};
const STATUS_LEFT_BAR_CLASS = {
    'needs-entry': 'border-l-amber-400',
    entered: 'border-l-emerald-400',
    rejected: 'border-l-red-400'
};

function setStatus(message, tone = 'info') {
    if (!pageStatus) return;
    pageStatus.classList.remove('hidden');
    pageStatus.textContent = message;
    if (tone === 'error') pageStatus.classList.add('text-red-700');
    else if (tone === 'success') pageStatus.classList.add('text-emerald-700');
}

function chip(value, label) {
    const labelText = label.replace(/_/g, ' ');

    const wrap = document.createElement('div');
    wrap.className = 'relative inline-flex flex-col';

    const lbl = document.createElement('span');
    lbl.className = 'mb-1 text-xs font-semibold uppercase tracking-wide text-slate-500';
    lbl.textContent = labelText;

    const btn = document.createElement('button');
    btn.type = 'button';
    btn.dataset.copy = value ?? '';
    btn.dataset.label = label;
    btn.className = 'inline-flex items-center rounded-md border border-slate-300 bg-white px-2.5 py-1 text-sm text-slate-700 hover:bg-slate-100 cursor-pointer';
    btn.textContent = (value ?? '') || '—';
    btn.title = `Copy ${labelText}`;

    wrap.append(lbl, btn);
    return wrap;
}

let activeTooltip = null;

function showCopiedTooltip(field) {
    if (activeTooltip) activeTooltip.remove();
    const tip = document.createElement('span');
    tip.className = 'copy-tooltip';
    tip.textContent = 'Copied';
    field.appendChild(tip);
    activeTooltip = tip;
    setTimeout(() => {
        if (activeTooltip === tip) activeTooltip = null;
        tip.remove();
    }, 1000);
}

function renderEntry(container, entry) {
    const card = document.createElement('div');
    card.className = 'rounded-xl border border-slate-200 bg-white p-5 shadow-sm';

    const bucket = STATUS_TO_BUCKET[entry.status] || entry.status;

    const header = document.createElement('div');
    header.className = 'mb-4 flex items-center justify-between';
    const title = document.createElement('h3');
    title.className = 'font-semibold text-slate-900';
    title.textContent = `${entry.parent.first_name} ${entry.parent.last_name}`;
    const badge = document.createElement('span');
    badge.className = `inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold ${STATUS_BADGE_CLASS[bucket] || 'bg-slate-100 text-slate-600'}`;
    badge.textContent = STATUS_LABELS[bucket] || entry.status;
    header.append(title, badge);
    card.appendChild(header);

    const leftBar = STATUS_LEFT_BAR_CLASS[bucket];
    if (leftBar) {
        card.classList.add('border-l-4', leftBar);
    }

    const parentBlock = document.createElement('div');
    const parentTitle = document.createElement('p');
    parentTitle.className = 'mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500';
    parentTitle.textContent = 'Parent';
    parentBlock.appendChild(parentTitle);
    const parentRows = document.createElement('div');
    parentRows.className = 'mb-4 flex flex-wrap gap-x-6 gap-y-4';
    ['first_name', 'last_name', 'phone', 'email'].forEach(field => {
        parentRows.appendChild(chip(entry.parent[field], field));
    });
    parentBlock.appendChild(parentRows);
    card.appendChild(parentBlock);

    const childrenBlock = document.createElement('div');
    const childrenTitle = document.createElement('p');
    childrenTitle.className = 'mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500';
    childrenTitle.textContent = 'Children';
    childrenBlock.appendChild(childrenTitle);
    (entry.children || []).forEach(child => {
        const row = document.createElement('div');
        row.className = 'mb-4 flex flex-wrap gap-x-6 gap-y-4 items-end';
        ['first_name', 'last_name', 'dob', 'grade'].forEach(field => {
            row.appendChild(chip(child[field], field));
        });
        childrenBlock.appendChild(row);
    });
    card.appendChild(childrenBlock);

    if (bucket === 'needs-entry') {
        const markEntered = document.createElement('button');
        markEntered.type = 'button';
        markEntered.dataset.markEntered = entry.public_id;
        markEntered.className = 'mt-4 inline-flex items-center rounded-md bg-slate-900 px-3 py-1.5 text-xs font-semibold text-white hover:bg-slate-800 cursor-pointer';
        markEntered.textContent = 'Mark entered';
        card.appendChild(markEntered);
    }

    container.appendChild(card);
    return card.querySelectorAll('[data-copy]');
}

async function copyValue(value) {
    if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(value ?? '');
    }
    return value;
}

function normalizePage(data) {
    return {
        items: Array.isArray(data?.items) ? data.items : [],
        total: Number(data?.total) || 0,
        totalPages: Number(data?.total_pages) || 0
    };
}

const PAGE_WINDOW = 5;

function pageRange(current, total) {
    const pages = [];
    if (total <= PAGE_WINDOW) {
        for (let i = 1; i <= total; i++) pages.push(i);
        return pages;
    }
    const start = Math.min(Math.max(current - 2, 1), total - PAGE_WINDOW + 1);
    for (let i = start; i < start + PAGE_WINDOW; i++) pages.push(i);
    return pages;
}

function buildPaginationBar(page, totalPages, total) {
    const bar = document.createElement('div');
    bar.className = 'flex flex-wrap items-center justify-between gap-3';

    const nav = document.createElement('div');
    nav.className = 'inline-flex items-center rounded-lg border border-slate-200 bg-white shadow-sm';

    const pageBtn = (label, targetPage, opts = {}) => {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.dataset.gotoPage = String(targetPage);
        btn.className = 'px-3 py-1.5 text-sm font-medium border-r border-slate-200 last:border-r-0';
        if (opts.current) {
            btn.classList.add('bg-slate-900', 'text-white');
            btn.setAttribute('aria-current', 'page');
        } else {
            btn.classList.add('text-slate-700', 'hover:bg-slate-100');
        }
        btn.disabled = opts.disabled || false;
        btn.textContent = label;
        return btn;
    };

    nav.append(pageBtn('First', 1, {disabled: page === 1}));
    nav.append(pageBtn('‹ Prev', Math.max(1, page - 1), {disabled: page === 1}));
    pageRange(page, totalPages).forEach(n => nav.append(pageBtn(String(n), n, {current: n === page})));
    nav.append(pageBtn('Next ›', Math.min(totalPages, page + 1), {disabled: page === totalPages}));
    nav.append(pageBtn('Last', totalPages, {disabled: page === totalPages}));

    const summary = document.createElement('p');
    summary.className = 'text-sm text-slate-600';
    summary.textContent = `Page ${page} of ${totalPages} · ${total} ${total === 1 ? 'entry' : 'entries'}`;

    bar.append(nav, summary);
    return bar;
}

function renderPagination(bucket, page, totalPages, total) {
    const hide = totalPages <= 0;
    [PAGINATION_TOP_IDS[bucket], PAGINATION_BOTTOM_IDS[bucket]].forEach(id => {
        const el = document.getElementById(id);
        if (!el) return;
        el.classList.toggle('hidden', hide);
        if (hide) return;
        el.innerHTML = '';
        el.appendChild(buildPaginationBar(page, totalPages, total));
    });
}

async function markEntered(publicId) {
    const data = await fetchJson(`/v1/checkins/guest-submissions/${publicId}/status`, {
        method: 'PATCH',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({status: 'entered'})
    });
    return data;
}

async function loadEntries(bucket, attempt = 0) {
    const container = document.getElementById(ENTRIES_IDS[bucket]);
    if (!container) return;
    container.innerHTML = '<p class="text-sm text-slate-500">Loading...</p>';
    const page = pageState[bucket] || 1;
    try {
        const fetches = BUCKET_FETCH_STATUSES[bucket].map(status =>
            fetchJson(`/v1/admin/guest-submissions?status=${status}&page=${page}`)
        );
        const results = await Promise.all(fetches);
        const pages = results.map(normalizePage);
        const entries = pages.flatMap(p => p.items).sort((a, b) => (b.created_at || '').localeCompare(a.created_at || ''));
        const total = pages.reduce((sum, p) => sum + p.total, 0);
        const totalPages = Math.max(0, ...pages.map(p => p.totalPages));
        if (entries.length === 0 && page > 1 && attempt < 1) {
            pageState[bucket] = page - 1;
            return loadEntries(bucket, attempt + 1);
        }
        if (!entries.length) {
            container.innerHTML = '<p class="text-sm text-slate-500">No guest entries yet.</p>';
            renderPagination(bucket, page, 0, 0);
            return;
        }
        container.innerHTML = '';
        entries.forEach(entry => renderEntry(container, entry));
        renderPagination(bucket, page, totalPages, total);
    } catch (error) {
        if (error instanceof window.SessionExpiredError) {
            window.location.href = '/login?next=' + encodeURIComponent(
                window.location.pathname + window.location.search
            );
            return;
        }
        container.innerHTML = '';
        setStatus(`Failed to load: ${error.message}`, 'error');
    }
}

function setActiveTab(bucket) {
    STATUS_ORDER.forEach(status => {
        const tab = document.getElementById(TAB_IDS[status]);
        if (tab) tab.setAttribute('aria-selected', String(status === bucket));
        const view = document.getElementById(VIEW_IDS[status]);
        if (view) view.classList.toggle('hidden', status !== bucket);
    });
}

function activeBucket() {
    const active = STATUS_ORDER.find(status => {
        const tab = document.getElementById(TAB_IDS[status]);
        return tab && tab.getAttribute('aria-selected') === 'true';
    });
    return active || 'needs-entry';
}

if (entriesContainer) {
    entriesContainer.addEventListener('click', async (event) => {
        const target = event.target;
        if (!(target instanceof HTMLElement)) return;
        if (target.dataset.copy !== undefined) {
            const field = target.closest('.relative') || target;
            await copyValue(target.dataset.copy);
            showCopiedTooltip(field);
            target.classList.add('ring-2', 'ring-emerald-400');
            setTimeout(() => target.classList.remove('ring-2', 'ring-emerald-400'), 300);
            return;
        }
        if (target.dataset.gotoPage) {
            const bucket = activeBucket();
            pageState[bucket] = parseInt(target.dataset.gotoPage, 10);
            loadEntries(bucket);
            return;
        }
        if (target.dataset.markEntered) {
            target.disabled = true;
            try {
                await markEntered(target.dataset.markEntered);
                await loadEntries(activeBucket());
                setStatus('Entry marked as entered.', 'success');
            } catch (error) {
                if (error instanceof window.SessionExpiredError) {
                    target.disabled = false;
                    window.location.href = '/login?next=' + encodeURIComponent(
                        window.location.pathname + window.location.search
                    );
                    return;
                }
                setStatus(error.message, 'error');
                target.disabled = false;
            }
        }
    });
}

STATUS_ORDER.forEach(status => {
    const tab = document.getElementById(TAB_IDS[status]);
    if (tab) tab.addEventListener('click', () => {
        setActiveTab(status);
        loadEntries(status);
    });
});

window.renderEntry = renderEntry;
window.copyValue = copyValue;
window.showCopiedTooltip = showCopiedTooltip;
window.loadEntries = loadEntries;
window.setActiveTab = setActiveTab;
window.activeBucket = activeBucket;
window.renderPagination = renderPagination;
window.pageRange = pageRange;

loadEntries('needs-entry');