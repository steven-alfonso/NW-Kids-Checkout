const API_URL = '';

const manualCheckinsBody = document.getElementById('manual-checkins-body');
const pageStatus = document.getElementById('page-status');
const pendingFamiliesContainer = document.getElementById('pending-families');

const modal = document.getElementById('manual-checkin-modal');
const manualCheckinForm = document.getElementById('manual-checkin-form');
const manualFirstName = document.getElementById('manual-first-name');
const manualLastName = document.getElementById('manual-last-name');
const manualSubmitButton = document.getElementById('manual-checkin-submit');

const PENDING_FAMILIES_REFRESH_INTERVAL_MS = 5000;
const DEFAULT_CHECKED_OUT_AFTER = '-12h';
const MANUAL_CHECKINS_REFRESH_INTERVAL_MS = 5000;
let manualCheckinsController = null;

function setPageStatus(message, tone = 'info') {
    pageStatus.classList.remove('hidden');
    pageStatus.classList.remove('border-red-200', 'bg-red-50', 'text-red-700');
    pageStatus.classList.remove('border-emerald-200', 'bg-emerald-50', 'text-emerald-700');

    if (tone === 'error') {
        pageStatus.classList.add('border-red-200', 'bg-red-50', 'text-red-700');
    } else if (tone === 'success') {
        pageStatus.classList.add('border-emerald-200', 'bg-emerald-50', 'text-emerald-700');
    }

    pageStatus.textContent = message;
}

function clearPageStatus() {
    pageStatus.classList.add('hidden');
    pageStatus.textContent = '';
}

function setManualCheckinError(message) {
    const errorEl = document.getElementById('manual-checkin-error');
    if (!errorEl) return;

    if (message) {
        errorEl.textContent = message;
        errorEl.classList.remove('hidden');
    } else {
        errorEl.textContent = '';
        errorEl.classList.add('hidden');
    }
}

function toggleManualCheckinModal(open) {
    if (!modal) return;

    if (open) {
        modal.classList.remove('hidden');
        modal.setAttribute('aria-hidden', 'false');
    } else {
        modal.classList.add('hidden');
        modal.setAttribute('aria-hidden', 'true');
        setManualCheckinError('');
        if (manualCheckinForm) manualCheckinForm.reset();
    }
}

function escapeHtml(value) {
    const div = document.createElement('div');
    div.textContent = String(value ?? '');
    return div.innerHTML;
}

async function fetchJson(path, options = {}) {
    const response = await fetch(`${API_URL}${path}`, options);
    if (!response.ok) {
        const message = await response.text();
        throw new Error(message || `Request failed with status ${response.status}`);
    }
    if (response.status === 204) {
        return null;
    }
    return response.json();
}

function buildManualCheckinsQuery() {
    const params = new URLSearchParams(window.location.search);
    const query = new URLSearchParams();

    const checkedOutAfter = params.get('checked_out_after') || DEFAULT_CHECKED_OUT_AFTER;
    query.set('checked_out_after', checkedOutAfter);

    if (params.get('limit')) {
        query.set('limit', params.get('limit'));
    }

    query.set('include_unchecked', params.get('include_unchecked') || 'true');
    query.set('sort', 'created');
    return query.toString();
}

function formatCheckedOutAt(value) {
    if (!value) return '—';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '—';
    return date.toLocaleString();
}

function formatCreatedAt(value) {
    if (!value) return '—';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '—';
    return date.toLocaleString();
}

function renderManualCheckins(checkins) {
    if (!manualCheckinsBody) return;

    if (!checkins.length) {
        manualCheckinsBody.innerHTML = `
            <tr>
                <td class="px-4 py-6 text-center text-slate-500" colspan="5">No manual check-ins found.</td>
            </tr>
        `;
        return;
    }

    manualCheckinsBody.innerHTML = '';

    checkins.forEach(checkin => {
        const row = document.createElement('tr');
        const nameCell = document.createElement('td');
        nameCell.className = 'px-4 py-4';
        nameCell.dataset.label = 'Name';
        const nameValue = document.createElement('span');
        nameValue.className = 'font-medium text-slate-900';
        nameValue.textContent = `${checkin.first_name || ''} ${checkin.last_name || ''}`.trim();
        nameCell.appendChild(nameValue);

        const createdCell = document.createElement('td');
        createdCell.className = 'px-4 py-4';
        createdCell.dataset.label = 'Created';
        const createdValue = document.createElement('span');
        createdValue.className = 'text-slate-600';
        createdValue.textContent = formatCreatedAt(checkin.created_at);
        createdCell.appendChild(createdValue);

        const statusCell = document.createElement('td');
        statusCell.className = 'px-4 py-4';
        statusCell.dataset.label = 'Status';

        const statusBadge = document.createElement('span');
        const isCheckedOut = Boolean(checkin.checked_out_at);
        statusBadge.className = `inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold ${isCheckedOut ? 'bg-emerald-100 text-emerald-700' : 'bg-amber-100 text-amber-700'}`;
        statusBadge.textContent = isCheckedOut ? 'Checked out' : 'Pending';
        statusCell.appendChild(statusBadge);

        const checkedOutCell = document.createElement('td');
        checkedOutCell.className = 'px-4 py-4';
        checkedOutCell.dataset.label = 'Checked Out';
        const checkedOutValue = document.createElement('span');
        checkedOutValue.className = 'text-slate-600';
        checkedOutValue.textContent = formatCheckedOutAt(checkin.checked_out_at);
        checkedOutCell.appendChild(checkedOutValue);

        const actionCell = document.createElement('td');
        actionCell.className = 'px-4 py-4';
        actionCell.dataset.label = 'Action';
        const actionButton = document.createElement('button');
        actionButton.className = 'inline-flex items-center rounded-md border border-slate-300 px-3 py-1.5 text-sm font-semibold text-slate-700 hover:bg-slate-50 cursor-pointer';
        actionButton.textContent = isCheckedOut ? 'Undo Checkout' : 'Check Out';
        actionButton.dataset.publicId = checkin.public_id;
        actionButton.dataset.checkedOut = isCheckedOut ? 'true' : 'false';
        actionButton.classList.add('bg-white');
        actionCell.appendChild(actionButton);

        row.appendChild(nameCell);
        row.appendChild(statusCell);
        row.appendChild(createdCell);
        row.appendChild(checkedOutCell);
        row.appendChild(actionCell);

        manualCheckinsBody.appendChild(row);
    });
}

async function loadManualCheckins() {
    clearPageStatus();
    if (manualCheckinsController) {
        manualCheckinsController.abort();
    }
    const controller = new AbortController();
    manualCheckinsController = controller;
    try {
        const query = buildManualCheckinsQuery();
        const checkins = await fetchJson(`/v1/checkins/manual-checkins?${query}`, {
            signal: controller.signal
        });
        renderManualCheckins(Array.isArray(checkins) ? checkins : []);
    } catch (error) {
        if (error?.name === 'AbortError') return;
        setPageStatus(`Failed to load manual check-ins: ${error.message}`, 'error');
        if (manualCheckinsBody) {
            manualCheckinsBody.innerHTML = `
                <tr>
                    <td class="px-4 py-6 text-center text-slate-500" colspan="4">Unable to load manual check-ins.</td>
                </tr>
            `;
        }
    } finally {
        if (manualCheckinsController === controller) {
            manualCheckinsController = null;
        }
    }
}

function renderPendingFamilies(submissions) {
    if (!pendingFamiliesContainer) return;
    if (!submissions.length) {
        pendingFamiliesContainer.innerHTML = '<p class="text-sm text-slate-500">No pending families.</p>';
        return;
    }
    pendingFamiliesContainer.innerHTML = '';
    submissions.forEach(sub => {
        const childrenHtml = sub.children
            .map(child => `<span class="block">${escapeHtml(child.first_name)} ${escapeHtml(child.last_name)}</span>`)
            .join('');
        const isEntered = sub.status === 'entered';
        const card = document.createElement('div');
        card.className = isEntered
            ? 'rounded-md border border-emerald-200 bg-emerald-50 p-4'
            : 'rounded-md border border-amber-200 bg-amber-50 p-4';
        card.innerHTML = `
            <p class="font-semibold text-slate-900">
                ${escapeHtml(sub.parent.first_name)} ${escapeHtml(sub.parent.last_name)}
                ${isEntered ? '<span class="ml-2 inline-flex items-center rounded-full bg-emerald-100 px-2.5 py-1 text-xs font-semibold text-emerald-700">Entered</span>' : ''}
            </p>
            <div class="text-sm text-slate-600">${childrenHtml}</div>
            <p class="text-xs text-slate-400">${formatCreatedAt(sub.created_at)}</p>
            ${isEntered
                ? `<div class="mt-3 flex gap-2">
                    <button data-create-checkins="${escapeHtml(sub.public_id)}" class="rounded-md border border-slate-700 px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-100 cursor-pointer">Create manual check-in</button>
                </div>`
                : `<div class="mt-3 flex gap-2">
                    <button data-approve="${escapeHtml(sub.public_id)}" class="rounded-md border border-emerald-600 px-3 py-1.5 text-xs font-semibold text-emerald-700 hover:bg-emerald-50 cursor-pointer">Approve</button>
                    <button data-reject="${escapeHtml(sub.public_id)}" class="rounded-md border border-red-600 px-3 py-1.5 text-xs font-semibold text-red-700 hover:bg-red-50 cursor-pointer">Reject</button>
                </div>`}`;
        pendingFamiliesContainer.appendChild(card);
    });
}

async function loadPendingFamilies() {
    if (!pendingFamiliesContainer) return;
    try {
        const [pending, entered] = await Promise.all([
            fetchJson('/v1/checkins/guest-submissions?status=pending'),
            fetchJson('/v1/checkins/guest-submissions?status=entered&without_manual_checkins=true')
        ]);
        const merged = [...(Array.isArray(pending) ? pending : []), ...(Array.isArray(entered) ? entered : [])]
            .sort((a, b) => new Date(b.created_at) - new Date(a.created_at));
        renderPendingFamilies(merged);
    } catch (error) {
        if (error?.name === 'AbortError') return;
        pendingFamiliesContainer.innerHTML = '<p class="text-sm text-red-600">Failed to load pending families.</p>';
    }
}

async function setSubmissionStatus(publicId, status) {
    await fetchJson(`/v1/checkins/guest-submissions/${publicId}/status`, {
        method: 'PATCH',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({status})
    });
}

async function approveSubmission(publicId) {
    await setSubmissionStatus(publicId, 'approved');
    await loadPendingFamilies();
    await loadManualCheckins();
}

async function rejectSubmission(publicId) {
    await setSubmissionStatus(publicId, 'rejected');
    await loadPendingFamilies();
}

async function createManualCheckins(publicId) {
    await fetchJson(`/v1/checkins/guest-submissions/${publicId}/checkins`, {
        method: 'POST'
    });
    await loadPendingFamilies();
    await loadManualCheckins();
}

// event delegation
if (pendingFamiliesContainer) {
    pendingFamiliesContainer.addEventListener('click', async (event) => {
        const target = event.target;
        if (!(target instanceof HTMLButtonElement)) return;
        const id = target.dataset.approve || target.dataset.reject || target.dataset.createCheckins;
        if (!id || target.disabled) return;
        target.disabled = true;
        try {
            if (target.dataset.approve) {
                await approveSubmission(id);
            } else if (target.dataset.reject) {
                await rejectSubmission(id);
            } else {
                await createManualCheckins(id);
            }
        } catch (error) {
            setPageStatus(`Failed to update: ${error.message}`, 'error');
            target.disabled = false;
        }
    });
}

window.renderPendingFamilies = renderPendingFamilies;
window.loadPendingFamilies = loadPendingFamilies;
window.approveSubmission = approveSubmission;
window.rejectSubmission = rejectSubmission;
window.createManualCheckins = createManualCheckins;
window.createManualCheckin = createManualCheckin;
window.toggleManualCheckinModal = toggleManualCheckinModal;
window.setManualCheckinError = setManualCheckinError;

async function createManualCheckin(payload) {
    return fetchJson('/v1/checkins/manual-checkins', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(payload)
    });
}

async function checkOutManualCheckin(publicId, checkedOut) {
    if (!publicId) return;
    await fetchJson(`/v1/checkins/manual-checkins/${publicId}/checked_out`, {
        method: 'PATCH',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({checked_out: Boolean(checkedOut)})
    });
}

document.addEventListener('DOMContentLoaded', () => {
    const openManualCheckinButton = document.getElementById('open-manual-checkin');

    if (openManualCheckinButton) {
        openManualCheckinButton.addEventListener('click', () => {
            toggleManualCheckinModal(true);
            if (manualFirstName) manualFirstName.focus();
        });
    }

    document.querySelectorAll('[data-modal-close]').forEach((closeButton) => {
        closeButton.addEventListener('click', () => toggleManualCheckinModal(false));
    });

    if (manualCheckinForm) {
        manualCheckinForm.addEventListener('submit', async (event) => {
            event.preventDefault();
            setManualCheckinError('');

            const firstName = manualFirstName?.value.trim() || '';
            const lastName = manualLastName?.value.trim() || '';

            if (!firstName || !lastName) {
                setManualCheckinError('First and last name are required.');
                return;
            }

            if (manualSubmitButton) {
                manualSubmitButton.disabled = true;
                manualSubmitButton.textContent = 'Saving...';
            }

            try {
                await createManualCheckin({
                    first_name: firstName,
                    last_name: lastName
                });
                toggleManualCheckinModal(false);
                await loadManualCheckins();
            } catch (error) {
                setManualCheckinError(error.message || 'Unable to save manual check-in.');
            } finally {
                if (manualSubmitButton) {
                    manualSubmitButton.disabled = false;
                    manualSubmitButton.textContent = 'Save';
                }
            }
        });
    }

    document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') {
            toggleManualCheckinModal(false);
        }
    });

    if (manualCheckinsBody) {
        manualCheckinsBody.addEventListener('click', async (event) => {
            const target = event.target;
            if (!(target instanceof HTMLButtonElement)) return;
            const publicId = target.dataset.publicId;
            if (!publicId || target.disabled) return;

            const currentlyCheckedOut = target.dataset.checkedOut === 'true';
            const nextCheckedOut = !currentlyCheckedOut;

            target.disabled = true;
            target.textContent = nextCheckedOut ? 'Checking out...' : 'Undoing...';

            try {
                await checkOutManualCheckin(publicId, nextCheckedOut);
                await loadManualCheckins();
            } catch (error) {
                setPageStatus(`Failed to check out: ${error.message}`, 'error');
                target.disabled = false;
                target.textContent = currentlyCheckedOut ? 'Undo Checkout' : 'Check Out';
            }
        });
    }

    loadManualCheckins();
    setInterval(loadManualCheckins, MANUAL_CHECKINS_REFRESH_INTERVAL_MS);

    loadPendingFamilies();
    setInterval(loadPendingFamilies, PENDING_FAMILIES_REFRESH_INTERVAL_MS);
});
