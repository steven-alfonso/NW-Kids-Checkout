const API_URL = '';

const manualCheckinsBody = document.getElementById('manual-checkins-body');
const pageStatus = document.getElementById('page-status');

const modal = document.getElementById('manual-checkin-modal');
const manualCheckinForm = document.getElementById('manual-checkin-form');
const manualFirstName = document.getElementById('manual-first-name');
const manualLastName = document.getElementById('manual-last-name');
const manualSubmitButton = document.getElementById('manual-checkin-submit');

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
        const checkins = await globalThis.fetchJson(`${API_URL}/v1/checkins/manual-checkins?${query}`, {
            signal: controller.signal
        });
        renderManualCheckins(Array.isArray(checkins) ? checkins : []);
    } catch (error) {
        if (error?.name === 'AbortError') return;
        if (error instanceof window.SessionExpiredError) {
            window.location.href = '/login?next=' + encodeURIComponent(
                window.location.pathname + window.location.search
            );
            return;
        }
        setPageStatus(`Failed to load manual check-ins: ${error.message}`, 'error');
        if (manualCheckinsBody) {
            manualCheckinsBody.innerHTML = `
                <tr>
                    <td class="px-4 py-6 text-center text-slate-500" colspan="5">Unable to load manual check-ins.</td>
                </tr>
            `;
        }
    } finally {
        if (manualCheckinsController === controller) {
            manualCheckinsController = null;
        }
    }
}

window.createManualCheckin = createManualCheckin;
window.toggleManualCheckinModal = toggleManualCheckinModal;
window.setManualCheckinError = setManualCheckinError;

async function createManualCheckin(payload) {
    return globalThis.fetchJson(`${API_URL}/v1/checkins/manual-checkins`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(payload)
    });
}

async function checkOutManualCheckin(publicId, checkedOut) {
    if (!publicId) return;
    await globalThis.fetchJson(`${API_URL}/v1/checkins/manual-checkins/${publicId}/checked_out`, {
        method: 'PATCH',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({checked_out: Boolean(checkedOut)})
    });
}

document.addEventListener('DOMContentLoaded', () => {
    if (typeof window.initKebabMenu === "function") {
        window.initKebabMenu();
    } else if (window.NWKidsKebabMenu && typeof window.NWKidsKebabMenu.initKebabMenu === "function") {
        window.NWKidsKebabMenu.initKebabMenu();
    }

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
                if (error instanceof window.SessionExpiredError) {
                    window.location.href = '/login?next=' + encodeURIComponent(
                        window.location.pathname + window.location.search
                    );
                    return;
                }
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
                if (error instanceof window.SessionExpiredError) {
                    window.location.href = '/login?next=' + encodeURIComponent(
                        window.location.pathname + window.location.search
                    );
                    return;
                }
                setPageStatus(`Failed to check out: ${error.message}`, 'error');
                target.disabled = false;
                target.textContent = currentlyCheckedOut ? 'Undo Checkout' : 'Check Out';
            }
        });
    }

    loadManualCheckins();
    setInterval(loadManualCheckins, MANUAL_CHECKINS_REFRESH_INTERVAL_MS);
});
