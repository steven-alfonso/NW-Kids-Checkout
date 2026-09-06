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

const TOAST_ANIM_MS = 300;
let statusTimeoutId = null;
let statusAnimTimeoutId = null;

function clearStatusTimeouts() {
    if (statusTimeoutId) {
        clearTimeout(statusTimeoutId);
        statusTimeoutId = null;
    }
    if (statusAnimTimeoutId) {
        clearTimeout(statusAnimTimeoutId);
        statusAnimTimeoutId = null;
    }
}

function hideStatus() {
    if (!pageStatus) return;
    clearStatusTimeouts();
    const isToast = pageStatus.classList.contains('toast-base');
    if (isToast) {
        // animate leaving to the bottom
        pageStatus.classList.remove('toast-visible');
        statusAnimTimeoutId = setTimeout(() => {
            pageStatus.classList.add('hidden');
            pageStatus.textContent = '';
            pageStatus.classList.remove('border-red-200', 'bg-red-50', 'text-red-700');
            pageStatus.classList.remove('border-emerald-200', 'bg-emerald-100', 'text-emerald-700');
            pageStatus.classList.remove('toast-base', 'toast-visible');
            pageStatus.classList.add('bg-white', 'border-slate-200');
            if (!pageStatus.classList.contains('mb-4')) pageStatus.classList.add('mb-4');
            statusAnimTimeoutId = null;
        }, TOAST_ANIM_MS);
    } else {
        pageStatus.classList.add('hidden');
        pageStatus.textContent = '';
        pageStatus.classList.remove('border-red-200', 'bg-red-50', 'text-red-700');
        pageStatus.classList.remove('border-emerald-200', 'bg-emerald-100', 'text-emerald-700');
        pageStatus.classList.remove('toast-base', 'toast-visible');
        pageStatus.classList.add('bg-white', 'border-slate-200');
        if (!pageStatus.classList.contains('mb-4')) pageStatus.classList.add('mb-4');
    }
}

function setStatus(message, tone = 'info') {
    if (!pageStatus) return;
    clearStatusTimeouts();
    pageStatus.classList.remove('hidden');
    pageStatus.classList.remove('border-red-200', 'bg-red-50', 'text-red-700');
    pageStatus.classList.remove('border-emerald-200', 'bg-emerald-100', 'text-emerald-700');
    pageStatus.classList.remove('toast-base', 'toast-visible');
    pageStatus.classList.remove('bg-white', 'border-slate-200');
    pageStatus.textContent = message;
    if (tone === 'error') {
        pageStatus.classList.add('border-red-200', 'bg-red-50', 'text-red-700');
        if (!pageStatus.classList.contains('mb-4')) pageStatus.classList.add('mb-4');
    } else if (tone === 'success') {
        pageStatus.classList.add('border-emerald-200', 'bg-emerald-100', 'text-emerald-700');
        pageStatus.classList.remove('mb-4');
        pageStatus.classList.add('toast-base');
        void pageStatus.offsetHeight;
        pageStatus.classList.add('toast-visible');
        statusTimeoutId = setTimeout(hideStatus, 4000);
    } else {
        pageStatus.classList.add('bg-white', 'border-slate-200');
        if (!pageStatus.classList.contains('mb-4')) pageStatus.classList.add('mb-4');
    }
}

function formatDob(value) {
    if (!value) return value;
    const str = String(value).slice(0, 10);
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(str);
    if (m) return `${m[2]}/${m[3]}/${m[1]}`;
    return value;
}

function chip(value, label) {
    const labelText = label === 'dietary_restrictions' ? 'dietary restriction(s)' : label.replace(/_/g, ' ');

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

function showTooltip(field, text) {
    if (activeTooltip) activeTooltip.remove();
    const tip = document.createElement('span');
    tip.className = 'copy-tooltip';
    tip.textContent = text;
    field.appendChild(tip);
    activeTooltip = tip;
    setTimeout(() => {
        if (activeTooltip === tip) activeTooltip = null;
        tip.remove();
    }, 1000);
}

function showCopiedTooltip(field) {
    showTooltip(field, 'Copied');
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
    parentTitle.textContent = 'Parent / Guardian';
    parentBlock.appendChild(parentTitle);
    const parentContactRow = document.createElement('div');
    parentContactRow.className = 'mb-3 flex flex-wrap gap-x-6 gap-y-4';
    ['first_name', 'last_name', 'phone', 'email'].forEach(field => {
        parentContactRow.appendChild(chip(entry.parent[field], field));
    });
    parentBlock.appendChild(parentContactRow);
    const parentAddressRow = document.createElement('div');
    parentAddressRow.className = 'mb-3 flex flex-wrap gap-x-6 gap-y-4';
    ['address1', 'address2', 'city', 'state', 'zip'].forEach(field => {
        parentAddressRow.appendChild(chip(entry.parent[field], field));
    });
    parentBlock.appendChild(parentAddressRow);
    card.appendChild(parentBlock);

    const childrenBlock = document.createElement('div');
    const childrenTitle = document.createElement('p');
    childrenTitle.className = 'mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500';
    childrenTitle.textContent = 'Children';
    childrenBlock.appendChild(childrenTitle);
    (entry.children || []).forEach(child => {
        const row = document.createElement('div');
        row.className = 'mb-4 rounded-lg border border-slate-100 bg-slate-50 p-3';
        const topRow = document.createElement('div');
        topRow.className = 'flex flex-wrap gap-x-6 gap-y-4';
        ['first_name', 'last_name', 'dob', 'grade', 'gender', 'relationship'].forEach(field => {
            const raw = child[field];
            const display = field === 'dob' ? formatDob(raw) : raw;
            topRow.appendChild(chip(display, field));
        });
        row.appendChild(topRow);
        const notesRow = document.createElement('div');
        notesRow.className = 'mt-3 flex flex-wrap gap-x-6 gap-y-4';
        const dietary = child.dietary_restrictions;
        const special = child.special_needs;
        const dietaryChip = chip(dietary, 'dietary_restrictions');
        const specialChip = chip(special, 'special_needs');
        if (dietary && String(dietary).trim()) {
            dietaryChip.querySelector('button').className = 'inline-flex items-center rounded-md border border-amber-300 bg-amber-50 px-2.5 py-1 text-sm text-amber-900 hover:bg-amber-100 cursor-pointer';
        }
        if (special && String(special).trim()) {
            specialChip.querySelector('button').className = 'inline-flex items-center rounded-md border border-sky-300 bg-sky-50 px-2.5 py-1 text-sm text-sky-900 hover:bg-sky-100 cursor-pointer';
        }
        notesRow.appendChild(dietaryChip);
        notesRow.appendChild(specialChip);
        row.appendChild(notesRow);
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
    const text = value ?? '';
    if (navigator.clipboard?.writeText) {
        try {
            await navigator.clipboard.writeText(text);
            return true;
        } catch {
        }
    }
    try {
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.setAttribute('readonly', '');
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        const copied = document.execCommand ? document.execCommand('copy') : false;
        ta.remove();
        return !!copied;
    } catch {
        return false;
    }
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
        btn.className = 'px-3 py-1.5 text-sm font-medium border-r border-slate-200 last:border-r-0 cursor-pointer disabled:cursor-not-allowed disabled:opacity-50 disabled:text-slate-400 disabled:bg-slate-50';
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
        const statuses = BUCKET_FETCH_STATUSES[bucket] || [];
        const statusParam = statuses.join(',');
        const data = await fetchJson(`/v1/admin/guest-submissions?status=${statusParam}&page=${page}`);
        const normalized = normalizePage(data);
        const entries = [...normalized.items].sort((a, b) => (b.created_at || '').localeCompare(a.created_at || ''));
        const total = normalized.total;
        const totalPages = normalized.totalPages;
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
            const ok = await copyValue(target.dataset.copy);
            if (ok) {
                showTooltip(field, 'Copied');
                target.classList.add('ring-2', 'ring-emerald-400');
                setTimeout(() => target.classList.remove('ring-2', 'ring-emerald-400'), 300);
            } else {
                showTooltip(field, 'Copy unavailable');
            }
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
window.showTooltip = showTooltip;
window.showCopiedTooltip = showCopiedTooltip;
window.loadEntries = loadEntries;
window.setActiveTab = setActiveTab;
window.activeBucket = activeBucket;
window.renderPagination = renderPagination;
window.pageRange = pageRange;
window.formatDob = formatDob;

loadEntries('needs-entry');