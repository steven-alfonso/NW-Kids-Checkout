const entriesContainer = document.getElementById('entries-container');
const pageStatus = document.getElementById('page-status');

const STATUS_ORDER = ['approved', 'pending', 'entered', 'rejected'];
const STATUS_LABELS = {
    approved: 'Approved - needs Planning Center entry',
    pending: 'Pending',
    entered: 'Entered',
    rejected: 'Rejected'
};
const STATUS_BADGE_CLASS = {
    approved: 'bg-amber-100 text-amber-800',
    pending: 'bg-sky-100 text-sky-700',
    entered: 'bg-emerald-100 text-emerald-700',
    rejected: 'bg-red-100 text-red-700'
};
const STATUS_HEADING_CLASS = {
    approved: 'text-amber-600',
    pending: 'text-sky-600',
    entered: 'text-emerald-600',
    rejected: 'text-red-600'
};
const ENTERED_COLLAPSED = true;
let enteredCollapsed = ENTERED_COLLAPSED;

function setStatus(message, tone = 'info') {
    if (!pageStatus) return;
    pageStatus.classList.remove('hidden');
    pageStatus.textContent = message;
    if (tone === 'error') pageStatus.classList.add('text-red-700');
    else if (tone === 'success') pageStatus.classList.add('text-emerald-700');
}

function groupByStatus(submissions) {
    const byStatus = new Map();
    STATUS_ORDER.forEach(status => byStatus.set(status, []));
    submissions.forEach(s => {
        if (!byStatus.has(s.status)) byStatus.set(s.status, []);
        byStatus.get(s.status).push(s);
    });
    return STATUS_ORDER.filter(status => byStatus.get(status).length > 0)
        .map(status => [status, byStatus.get(status)]);
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

function showCopiedTooltip(field) {
    const tip = document.createElement('span');
    tip.className = 'copy-tooltip';
    tip.textContent = 'Copied';
    field.appendChild(tip);
    setTimeout(() => tip.remove(), 1000);
}

function renderEntry(container, entry) {
    const card = document.createElement('div');
    card.className = 'rounded-xl border border-slate-200 bg-white p-5 shadow-sm';

    const header = document.createElement('div');
    header.className = 'mb-4 flex items-center justify-between';
    const title = document.createElement('h3');
    title.className = 'font-semibold text-slate-900';
    title.textContent = `${entry.parent.first_name} ${entry.parent.last_name}`;
    const badge = document.createElement('span');
    badge.className = `inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold ${STATUS_BADGE_CLASS[entry.status] || 'bg-slate-100 text-slate-600'}`;
    badge.textContent = STATUS_LABELS[entry.status] || entry.status;
    header.append(title, badge);
    card.appendChild(header);

    if (entry.status === 'approved') {
        card.classList.add('border-l-4', 'border-l-amber-400');
    }

    const parentBlock = document.createElement('div');
    parentBlock.className = 'mb-4 flex flex-wrap gap-x-6 gap-y-4';
    parentBlock.innerHTML = '<p class="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500">Parent</p>';
    ['first_name', 'last_name', 'phone', 'email'].forEach(field => {
        parentBlock.appendChild(chip(entry.parent[field], field));
    });
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

    if (entry.status === 'approved') {
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

async function markEntered(publicId) {
    const response = await fetch(`/v1/checkins/guest-submissions/${publicId}/status`, {
        method: 'PATCH',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({status: 'entered'})
    });
    if (!response.ok) throw new Error('Failed to mark entered');
}

async function loadEntries() {
    if (!entriesContainer) return;
    entriesContainer.innerHTML = '<p class="text-sm text-slate-500">Loading...</p>';
    try {
        const data = await fetch('/v1/admin/guest-submissions').then(r => {
            if (!r.ok) throw new Error('Failed to load');
            return r.json();
        });
        entriesContainer.innerHTML = '';
        const groups = groupByStatus(Array.isArray(data) ? data : []);
        groups.forEach(([status, entries]) => {
            const isEntered = status === 'entered';
            const sectionId = `section-${status}`;
            const heading = document.createElement('h2');
            const headingClass = STATUS_HEADING_CLASS[status] || 'text-slate-500';
            heading.className = `mt-6 mb-3 text-sm font-semibold uppercase tracking-wide ${headingClass}`;
            heading.textContent = `${STATUS_LABELS[status]} (${entries.length})`;

            if (isEntered) {
                const body = document.createElement('div');
                body.id = `collapsible-${sectionId}`;
                body.className = enteredCollapsed ? 'hidden' : 'space-y-4';
                entries.forEach(entry => renderEntry(body, entry));

                const toggle = document.createElement('button');
                toggle.type = 'button';
                toggle.dataset.collapseToggle = status;
                toggle.setAttribute('aria-expanded', String(!enteredCollapsed));
                toggle.className = 'mt-6 mb-3 flex w-full items-center justify-between text-left text-sm font-semibold uppercase tracking-wide cursor-pointer hover:opacity-80';
                toggle.innerHTML = `<span class="${headingClass}">${STATUS_LABELS[status]} (${entries.length})</span><span aria-hidden="true" class="text-xl leading-none">${enteredCollapsed ? '▸' : '▾'}</span>`;
                entriesContainer.appendChild(toggle);
                entriesContainer.appendChild(body);
                return;
            }

            heading.textContent = `${STATUS_LABELS[status]} (${entries.length})`;
            entriesContainer.appendChild(heading);
            entries.forEach(entry => renderEntry(entriesContainer, entry));
        });
        if (!groups.length) {
            entriesContainer.innerHTML = '<p class="text-sm text-slate-500">No guest entries yet.</p>';
        }
    } catch (error) {
        if (entriesContainer) entriesContainer.innerHTML = '';
        setStatus(`Failed to load: ${error.message}`, 'error');
    }
}

if (entriesContainer) {
    entriesContainer.addEventListener('click', async (event) => {
        const target = event.target;
        if (!(target instanceof HTMLElement)) return;
        if (target.dataset.collapseToggle) {
            enteredCollapsed = !enteredCollapsed;
            const body = document.getElementById(`collapsible-section-${target.dataset.collapseToggle}`);
            if (body) {
                body.classList.toggle('hidden', enteredCollapsed);
                body.classList.toggle('space-y-4', !enteredCollapsed);
            }
            target.setAttribute('aria-expanded', String(!enteredCollapsed));
            target.querySelector('span[aria-hidden="true"]').textContent = enteredCollapsed ? '▸' : '▾';
            return;
        }
        if (target.dataset.copy !== undefined) {
            const field = target.closest('.relative') || target;
            await copyValue(target.dataset.copy);
            showCopiedTooltip(field);
            target.classList.add('ring-2', 'ring-emerald-400');
            setTimeout(() => target.classList.remove('ring-2', 'ring-emerald-400'), 300);
            return;
        }
        if (target.dataset.markEntered) {
            target.disabled = true;
            try {
                await markEntered(target.dataset.markEntered);
                await loadEntries();
                setStatus('Entry marked as entered.', 'success');
            } catch (error) {
                setStatus(error.message, 'error');
                target.disabled = false;
            }
        }
    });
}

window.groupByStatus = groupByStatus;
window.renderEntry = renderEntry;
window.copyValue = copyValue;
window.showCopiedTooltip = showCopiedTooltip;
window.loadEntries = loadEntries;

loadEntries();
