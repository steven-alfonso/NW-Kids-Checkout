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

function childRowTemplate() {
    const row = document.createElement('div');
    row.className = 'child-row grid gap-2';
    row.innerHTML = `
        <input class="child-first-name" name="child_first_name" type="text" autocomplete="off" placeholder="First name" required>
        <input class="child-last-name" name="child_last_name" type="text" autocomplete="off" placeholder="Last name" required>
        <input class="child-dob" name="child_dob" type="date" autocomplete="off" required>
        <input class="child-grade" name="child_grade" type="text" autocomplete="off" placeholder="Grade" required>
        <button type="button" class="remove-child" aria-label="Remove child">Remove</button>
    `;
    row.querySelector('.remove-child').addEventListener('click', () => removeChildRow(row));
    return row;
}

function addChildRow() {
    childrenContainer.appendChild(childRowTemplate());
}

function removeChildRow(row) {
    if (childrenContainer.querySelectorAll('.child-row').length === 1) return;
    row.remove();
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
}

function showWelcome() {
    if (welcomePanel) welcomePanel.classList.remove('hidden');
    if (kioskForm) kioskForm.classList.add('hidden');
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

async function submitKioskForm() {
    setKioskError('');
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

if (kioskForm) {
    kioskForm.addEventListener('submit', (event) => {
        event.preventDefault();
        submitKioskForm();
    });
}

const addChildButton = document.getElementById('add-child');
if (addChildButton) addChildButton.addEventListener('click', addChildRow);

if (childrenContainer && !childrenContainer.querySelector('.child-row')) {
    addChildRow();
}

window.addChildRow = addChildRow;
window.removeChildRow = removeChildRow;
window.buildPayload = buildPayload;
window.resetForm = resetForm;
window.showWelcome = showWelcome;
window.submitKioskForm = submitKioskForm;