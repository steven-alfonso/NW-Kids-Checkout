const kioskForm = document.getElementById('kiosk-form');
const childrenContainer = document.getElementById('children-container');
const kioskError = document.getElementById('kiosk-error');
const welcomePanel = document.getElementById('welcome-panel');
const submitButton = document.getElementById('kiosk-submit');

function setKioskError(message) {
    if (!kioskError) return;
    kioskError.textContent = message || '';
    kioskError.classList.toggle('hidden', !message);
}

const GRADE_OPTIONS = ['None', 'Pre-K', 'Kindergarten', '1st', '2nd', '3rd', '4th', '5th', '6th', '7th', '8th', '9th', '10th', '11th', '12th'];
const MAX_CHILDREN = 10;

const inputClass = 'mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-base text-gray-900 shadow-sm focus:border-gray-500 focus:outline-none focus:ring-1 focus:ring-gray-500';

function childRowTemplate() {
    const row = document.createElement('div');
    row.className = 'child-row rounded-lg border border-slate-200 bg-slate-50 p-5';
    const gradeOptions = GRADE_OPTIONS.map(grade => `<option value="${grade}">${grade}</option>`).join('');
    row.innerHTML = `
        <div class="kiosk-field grid gap-3 sm:grid-cols-2">
            <label class="block text-sm font-semibold text-gray-700">First name
                <input class="child-first-name ${inputClass}" name="child_first_name" type="text" autocomplete="off" required>
            </label>
            <label class="block text-sm font-semibold text-gray-700">Last name
                <input class="child-last-name ${inputClass}" name="child_last_name" type="text" autocomplete="off" required>
            </label>
            <label class="block text-sm font-semibold text-gray-700">Birthdate
                <input class="child-dob ${inputClass}" name="child_dob" type="date" autocomplete="off" max="${new Date().toISOString().split('T')[0]}" required>
            </label>
            <label class="block text-sm font-semibold text-gray-700">Grade
                <select class="child-grade ${inputClass}" name="child_grade" autocomplete="off">
                    ${gradeOptions}
                </select>
            </label>
        </div>
        <button type="button" class="remove-child mt-2 rounded-md border border-red-300 px-3 py-1.5 text-sm font-semibold text-red-600 hover:bg-red-50 cursor-pointer" aria-label="Remove child">Remove</button>
    `;
    row.querySelector('.remove-child').addEventListener('click', () => removeChildRow(row));
    return row;
}

function addChildRow() {
    if (childrenContainer.querySelectorAll('.child-row').length >= MAX_CHILDREN) return;
    const row = childRowTemplate();
    const toggle = document.getElementById('use-parent-last-name');
    const lastName = getParentLastName();
    if (toggle?.checked && lastName) {
        row.querySelector('.child-last-name').value = lastName;
    }
    childrenContainer.appendChild(row);
    updateChildCount();
}

function removeChildRow(row) {
    if (childrenContainer.querySelectorAll('.child-row').length === 1) return;
    row.remove();
    updateChildCount();
}

function updateChildCount() {
    const count = childrenContainer.querySelectorAll('.child-row').length;
    const atMax = count >= MAX_CHILDREN;
    const addChildButton = document.getElementById('add-child');
    if (addChildButton) {
        addChildButton.disabled = atMax;
    }
    const hint = document.getElementById('add-child-hint');
    if (hint) {
        hint.classList.toggle('hidden', !atMax);
    }
}

function buildPayload() {
    const children = [];
    childrenContainer.querySelectorAll('.child-row').forEach(row => {
        children.push({
            first_name: row.querySelector('.child-first-name').value.trim(),
            last_name: row.querySelector('.child-last-name').value.trim(),
            dob: row.querySelector('.child-dob').value,
            grade: row.querySelector('.child-grade').value.trim()
        });
    });
    return {
        parent: {
            first_name: document.getElementById('parent-first-name').value.trim(),
            last_name: document.getElementById('parent-last-name').value.trim(),
            phone: document.getElementById('parent-phone').value.trim(),
            email: document.getElementById('parent-email').value.trim()
        },
        children
    };
}

function resetForm() {
    kioskForm.reset();
    const rows = childrenContainer.querySelectorAll('.child-row');
    for (let i = rows.length - 1; i >= 1; i--) {
        rows[i].remove();
    }
    updateChildCount();
}

function showWelcome() {
    if (welcomePanel) welcomePanel.classList.remove('hidden');
    if (kioskForm) kioskForm.classList.add('hidden');
    startCountdown();
}

const COUNTDOWN_SECONDS = 10;
let countdownInterval = null;

function updateCountdownText(remaining) {
    const el = document.getElementById('countdown-text');
    if (el) el.textContent = `Going back to a new guest form in ${remaining}s`;
}

function stopCountdown() {
    if (countdownInterval) {
        clearInterval(countdownInterval);
        countdownInterval = null;
    }
}

function startCountdown() {
    stopCountdown();
    let remaining = COUNTDOWN_SECONDS;
    updateCountdownText(remaining);
    countdownInterval = setInterval(() => {
        remaining -= 1;
        if (remaining <= 0) {
            stopCountdown();
            resetForm();
            showForm();
            return;
        }
        updateCountdownText(remaining);
    }, 1000);
}

async function postSubmission(payload) {
    const response = await fetch('/v1/checkins/guest-submissions', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(payload)
    });
    if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `Request failed with status ${response.status}`);
    }
    return response.json();
}

function showForm() {
    stopCountdown();
    if (welcomePanel) welcomePanel.classList.add('hidden');
    if (kioskForm) kioskForm.classList.remove('hidden');
}

function validateForm() {
    if (!kioskForm) return true;
    const phoneInput = document.getElementById('parent-phone');
    const emailInput = document.getElementById('parent-email');
    const phone = phoneInput?.value.trim() ?? '';
    const email = emailInput?.value.trim() ?? '';

    phoneInput?.setCustomValidity('');
    emailInput?.setCustomValidity('');

    if (!phone && !email) {
        phoneInput?.setCustomValidity('Please provide either a phone number or an email');
        return false;
    }
    if (phone) {
        const digits = phone.replace(/\D/g, '').length;
        if (digits < 7) {
            phoneInput?.setCustomValidity('Phone must contain at least 7 digits');
            return false;
        }
    }

    const childRows = childrenContainer.querySelectorAll('.child-row');
    if (childRows.length === 0) {
        setKioskError('At least one child is required');
        return false;
    }

    for (const row of childRows) {
        const firstName = row.querySelector('.child-first-name');
        const lastName = row.querySelector('.child-last-name');
        firstName?.setCustomValidity('');
        lastName?.setCustomValidity('');
        if (!firstName?.value.trim()) {
            firstName?.setCustomValidity('First name is required');
            setKioskError('');
            return false;
        }
        if (!lastName?.value.trim()) {
            lastName?.setCustomValidity('Last name is required');
            setKioskError('');
            return false;
        }
    }

    setKioskError('');
    return kioskForm.checkValidity();
}

async function submitKioskForm() {
    setKioskError('');
    if (!validateForm()) {
        if (kioskForm.reportValidity) kioskForm.reportValidity();
        return;
    }
    if (submitButton) submitButton.disabled = true;
    try {
        await postSubmission(buildPayload());
        resetForm();
        showWelcome();
    } catch (error) {
        setKioskError(error.message || 'Unable to submit. Please try again.');
    } finally {
        if (submitButton) submitButton.disabled = false;
    }
}

const newSubmissionButton = document.getElementById('new-submission-button');
if (newSubmissionButton) {
    newSubmissionButton.addEventListener('click', () => {
        resetForm();
        showForm();
    });
}

if (kioskForm) {
    kioskForm.addEventListener('submit', (event) => {
        event.preventDefault();
        submitKioskForm();
    });
}

function getParentLastName() {
    const input = document.getElementById('parent-last-name');
    return input ? input.value.trim() : '';
}

function syncChildrenLastName() {
    const lastName = document.getElementById('parent-last-name')?.value ?? '';
    childrenContainer.querySelectorAll('.child-last-name').forEach(input => {
        input.value = lastName;
    });
}

function handleParentLastNameInput() {
    if (document.getElementById('use-parent-last-name')?.checked) {
        syncChildrenLastName();
    }
}

function setUseParentLastNameToggleVisual(checked) {
    const bg = document.getElementById('use-parent-last-name-toggle-bg');
    const knob = document.getElementById('use-parent-last-name-toggle-knob');
    if (bg && knob) {
        bg.style.backgroundColor = checked ? 'var(--color-emerald-500)' : 'var(--color-slate-200)';
        knob.style.transform = checked ? 'translateX(1rem)' : 'translateX(0)';
    }
}

function handleUseParentLastNameChange() {
    const toggle = document.getElementById('use-parent-last-name');
    const checked = Boolean(toggle?.checked);
    setUseParentLastNameToggleVisual(checked);
    if (checked) {
        syncChildrenLastName();
    }
}

const addChildButton = document.getElementById('add-child');
if (addChildButton) addChildButton.addEventListener('click', addChildRow);

const useParentLastNameToggle = document.getElementById('use-parent-last-name');
if (useParentLastNameToggle) {
    useParentLastNameToggle.addEventListener('change', handleUseParentLastNameChange);
}
const parentLastNameInput = document.getElementById('parent-last-name');
if (parentLastNameInput) {
    parentLastNameInput.addEventListener('input', handleParentLastNameInput);
}

if (childrenContainer && !childrenContainer.querySelector('.child-row')) {
    addChildRow();
}
updateChildCount();

window.addChildRow = addChildRow;
window.removeChildRow = removeChildRow;
window.updateChildCount = updateChildCount;
window.syncChildrenLastName = syncChildrenLastName;
window.handleParentLastNameInput = handleParentLastNameInput;
window.handleUseParentLastNameChange = handleUseParentLastNameChange;
window.setUseParentLastNameToggleVisual = setUseParentLastNameToggleVisual;

setUseParentLastNameToggleVisual(document.getElementById('use-parent-last-name')?.checked);
window.buildPayload = buildPayload;
window.resetForm = resetForm;
window.showWelcome = showWelcome;
window.showForm = showForm;
window.startCountdown = startCountdown;
window.stopCountdown = stopCountdown;
window.validateForm = validateForm;
window.submitKioskForm = submitKioskForm;
