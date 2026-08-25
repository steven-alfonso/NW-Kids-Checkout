const entriesContainer = document.getElementById('entries-container');
const pageStatus = document.getElementById('page-status');

const STATUS_ORDER = ['approved', 'pending', 'entered', 'rejected'];
const STATUS_LABELS = {
    approved: 'Approved - needs Planning Center entry',
    pending: 'Pending',
    entered: 'Entered',
    rejected: 'Rejected'
};

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
    const button = document.createElement('button');
    button.type = 'button';
    button.dataset.copy = value ?? '';
    button.dataset.label = label;
    button.className = 'inline-flex items-center rounded-md border border-slate-300 bg-white px-2.5 py-1 text-sm text-slate-700 hover:bg-slate-100 cursor-pointer';
    button.textContent = value ?? '';
    button.title = `Copy ${label}`;
    return button;
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
    badge.className = 'inline-flex items-center rounded-full bg-slate-100 px-2.5 py-1 text-xs font-semibold text-slate-600';
    badge.textContent = STATUS_LABELS[entry.status] || entry.status;
    header.append(title, badge);
    card.appendChild(header);

    const parentBlock = document.createElement('div');
    parentBlock.className = 'mb-4';
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
        row.className = 'mb-1 flex flex-wrap gap-1';
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
    return container.querySelectorAll('[data-copy]');
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
            const heading = document.createElement('h2');
            heading.className = 'mt-6 mb-3 text-sm font-semibold uppercase tracking-wide text-slate-500';
            heading.textContent = STATUS_LABELS[status];
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
        if (target.dataset.copy !== undefined) {
            await copyValue(target.dataset.copy);
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
window.loadEntries = loadEntries;

loadEntries();