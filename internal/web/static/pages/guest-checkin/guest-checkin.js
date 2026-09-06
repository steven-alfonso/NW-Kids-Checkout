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
const GENDER_OPTIONS = ['Boy', 'Girl'];
const RELATIONSHIP_OPTIONS = ['Parent', 'Guardian', 'Grandparent', 'Other'];
const MAX_CHILDREN = 10;

const DIETARY_PRESETS = ['Peanut allergy', 'Tree nut allergy', 'Dairy allergy', 'Egg allergy', 'Soy allergy', 'Sesame allergy', 'Gluten free', 'Avoid food dye'];

const inputClass = 'mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-base text-gray-900 shadow-sm focus:border-gray-500 focus:outline-none focus:ring-1 focus:ring-gray-500';

let dietaryHintIdCounter = 0;

const dietaryPillBaseClass = 'dietary-pill inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-medium cursor-pointer transition-colors';
const dietaryPillOffClass = 'border-slate-300 bg-white text-slate-700 hover:bg-slate-50';
const dietaryPillOnClass = 'border-amber-300 bg-amber-50 text-amber-900 hover:bg-amber-100';

function parseDietaryTokens(value) {
    if (!value) return [];
    return value.split(',').map(s => s.trim()).filter(Boolean);
}

function syncDietaryPills(row) {
    const textarea = row.querySelector('.child-dietary');
    if (!textarea) return;
    const tokensLower = new Set(parseDietaryTokens(textarea.value).map(t => t.toLowerCase()));
    row.querySelectorAll('.dietary-pill').forEach(btn => {
        const pillValue = btn.dataset.value || '';
        const active = tokensLower.has(pillValue.toLowerCase());
        btn.setAttribute('aria-pressed', String(active));
        btn.className = `${dietaryPillBaseClass} ${active ? dietaryPillOnClass : dietaryPillOffClass}`;
    });
}

function toggleDietaryPill(row, pillText) {
    const textarea = row.querySelector('.child-dietary');
    if (!textarea) return;
    const tokens = parseDietaryTokens(textarea.value);
    const lowerPill = pillText.toLowerCase();
    const idx = tokens.findIndex(t => t.toLowerCase() === lowerPill);
    let newTokens;
    if (idx !== -1) {
        newTokens = tokens.slice(0, idx).concat(tokens.slice(idx + 1));
    } else {
        const candidate = tokens.length === 0 ? pillText : tokens.join(', ') + ', ' + pillText;
        if (candidate.length > 500) return;
        newTokens = [...tokens, pillText];
    }
    textarea.value = newTokens.join(', ');
    syncDietaryPills(row);
    textarea.dispatchEvent(new Event('input', { bubbles: true }));
}

function dietaryPillsMarkup() {
    return DIETARY_PRESETS.map(p => `<button type="button" class="${dietaryPillBaseClass} ${dietaryPillOffClass}" data-value="${p}" aria-pressed="false">${p}</button>`).join('');
}

function childRowTemplate() {
    const row = document.createElement('div');
    row.className = 'child-row rounded-lg border border-slate-200 bg-slate-50 p-5';
    const gradeOptions = GRADE_OPTIONS.map(grade => `<option value="${grade}">${grade}</option>`).join('');
    const genderOptions = GENDER_OPTIONS.map(g => `<option value="${g}">${g}</option>`).join('');
    const relationshipOptions = RELATIONSHIP_OPTIONS.map(r => `<option value="${r}">${r}</option>`).join('');
    const dietaryHintId = `dietary-hint-${++dietaryHintIdCounter}`;
    row.innerHTML = `
        <div class="kiosk-field grid gap-3 sm:grid-cols-2">
            <label class="block text-sm font-semibold text-gray-700">First name <span class="text-red-600">*</span>
                <input class="child-first-name ${inputClass}" name="child_first_name" type="text" autocomplete="off" required maxlength="100">
            </label>
            <label class="block text-sm font-semibold text-gray-700">Last name <span class="text-red-600">*</span>
                <input class="child-last-name ${inputClass}" name="child_last_name" type="text" autocomplete="off" required maxlength="100">
            </label>
            <label class="block text-sm font-semibold text-gray-700">Birthdate <span class="text-red-600">*</span>
                <input class="child-dob ${inputClass}" name="child_dob" type="date" autocomplete="off" max="${new Date().toLocaleDateString('en-CA')}" required>
            </label>
            <label class="block text-sm font-semibold text-gray-700">Grade <span class="text-red-600">*</span>
                <select class="child-grade ${inputClass}" name="child_grade" autocomplete="off" required>
                    ${gradeOptions}
                </select>
            </label>
            <label class="block text-sm font-semibold text-gray-700">Gender <span class="text-red-600">*</span>
                <select class="child-gender ${inputClass}" name="child_gender" autocomplete="off" required>
                    <option value="">Select</option>
                    ${genderOptions}
                </select>
            </label>
            <label class="block text-sm font-semibold text-gray-700">Relationship to child <span class="text-red-600">*</span>
                <select class="child-relationship ${inputClass}" name="child_relationship" autocomplete="off" required>
                    <option value="">Select</option>
                    ${relationshipOptions}
                </select>
            </label>
            <div class="sm:col-span-2">
                <label class="block text-sm font-semibold text-gray-700">Dietary restriction(s)
                    <textarea class="child-dietary ${inputClass}" name="child_dietary" autocomplete="off" maxlength="500" rows="2" placeholder="Optional" aria-describedby="${dietaryHintId}"></textarea>
                </label>
                <div class="dietary-pills mt-2 flex flex-wrap gap-1.5" role="group" aria-label="Quick select dietary restrictions">
                    ${dietaryPillsMarkup()}
                </div>
                <p id="${dietaryHintId}" class="dietary-hint mt-1.5 text-xs leading-4 text-slate-500">Tap a pill to add it, or type your own — separate with commas.</p>
            </div>
            <label class="block text-sm font-semibold text-gray-700 sm:col-span-2">Special needs
                <textarea class="child-special-needs ${inputClass}" name="child_special_needs" autocomplete="off" maxlength="500" rows="2" placeholder="Optional"></textarea>
            </label>
        </div>
        <button type="button" class="remove-child mt-2 rounded-md border border-red-300 px-3 py-1.5 text-sm font-semibold text-red-600 hover:bg-red-50 cursor-pointer" aria-label="Remove child">Remove Child</button>
    `;
    row.querySelector('.remove-child').addEventListener('click', () => removeChildRow(row));
    const dietaryTextarea = row.querySelector('.child-dietary');
    if (dietaryTextarea) {
        dietaryTextarea.addEventListener('input', () => syncDietaryPills(row));
    }
    row.querySelectorAll('.dietary-pill').forEach(btn => {
        btn.addEventListener('click', () => toggleDietaryPill(row, btn.dataset.value));
    });
    syncDietaryPills(row);
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
            grade: row.querySelector('.child-grade').value.trim(),
            gender: row.querySelector('.child-gender').value,
            dietary_restrictions: row.querySelector('.child-dietary').value.trim(),
            special_needs: row.querySelector('.child-special-needs').value.trim(),
            relationship: row.querySelector('.child-relationship').value
        });
    });
    return {
        parent: {
            first_name: document.getElementById('parent-first-name').value.trim(),
            last_name: document.getElementById('parent-last-name').value.trim(),
            phone: document.getElementById('parent-phone').value.trim(),
            email: document.getElementById('parent-email').value.trim(),
            address1: document.getElementById('parent-address1').value.trim(),
            address2: document.getElementById('parent-address2').value.trim(),
            city: document.getElementById('parent-city').value.trim(),
            state: document.getElementById('parent-state').value.trim().toUpperCase(),
            zip: document.getElementById('parent-zip').value.trim()
        },
        children,
        safety_ack: document.getElementById('safety-ack')?.checked ?? false
    };
}

function resetForm() {
    kioskForm.reset();
    setUseParentLastNameToggleVisual(document.getElementById('use-parent-last-name')?.checked ?? false);
    const rows = childrenContainer.querySelectorAll('.child-row');
    for (let i = rows.length - 1; i >= 1; i--) {
        rows[i].remove();
    }
    childrenContainer.querySelectorAll('.child-row').forEach(row => syncDietaryPills(row));
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
    try {
        const data = await globalThis.fetchJson('/v1/checkins/guest-submissions', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        return data;
    } catch (error) {
        if (error instanceof window.SessionExpiredError) {
            throw new Error('Please ask a staff member to sign in');
        }
        throw error;
    }
}

function showForm() {
    stopCountdown();
    if (welcomePanel) welcomePanel.classList.add('hidden');
    if (kioskForm) kioskForm.classList.remove('hidden');
}

function validateForm() {
    if (!kioskForm) return true;
    const phoneInput = document.getElementById('parent-phone');
    const address1Input = document.getElementById('parent-address1');
    const cityInput = document.getElementById('parent-city');
    const stateInput = document.getElementById('parent-state');
    const zipInput = document.getElementById('parent-zip');
    const safetyAck = document.getElementById('safety-ack');
    const phone = phoneInput?.value.trim() ?? '';

    phoneInput?.setCustomValidity('');
    address1Input?.setCustomValidity('');
    cityInput?.setCustomValidity('');
    stateInput?.setCustomValidity('');
    zipInput?.setCustomValidity('');
    safetyAck?.setCustomValidity('');

    if (!phone) {
        phoneInput?.setCustomValidity('Phone is required');
        return false;
    }
    const digits = phone.replace(/\D/g, '').length;
    if (digits < 7) {
        phoneInput?.setCustomValidity('Phone must contain at least 7 digits');
        return false;
    }
    if (!address1Input?.value.trim()) {
        address1Input?.setCustomValidity('Address is required');
        return false;
    }
    if (!cityInput?.value.trim()) {
        cityInput?.setCustomValidity('City is required');
        return false;
    }
    if (!stateInput?.value.trim()) {
        stateInput?.setCustomValidity('State is required');
        return false;
    }
    if (stateInput?.value.trim().length !== 2 || !/^[A-Za-z]{2}$/.test(stateInput.value.trim())) {
        stateInput?.setCustomValidity('State must be a 2-letter code');
        return false;
    }
    if (!zipInput?.value.trim()) {
        zipInput?.setCustomValidity('Zip is required');
        return false;
    }
    if (!/^\d{5}(-\d{4})?$/.test(zipInput.value.trim())) {
        zipInput?.setCustomValidity('Zip must be 5 digits or 5+4 format');
        return false;
    }
    if (safetyAck && !safetyAck.checked) {
        safetyAck.setCustomValidity('You must acknowledge the safety policy');
        setKioskError('You must acknowledge the safety policy');
        return false;
    }

    const childRows = childrenContainer.querySelectorAll('.child-row');
    if (childRows.length === 0) {
        setKioskError('At least one child is required');
        return false;
    }

    const today = new Date().toLocaleDateString('en-CA');
    for (const row of childRows) {
        const firstName = row.querySelector('.child-first-name');
        const lastName = row.querySelector('.child-last-name');
        const dob = row.querySelector('.child-dob');
        const gender = row.querySelector('.child-gender');
        const relationship = row.querySelector('.child-relationship');
        firstName?.setCustomValidity('');
        lastName?.setCustomValidity('');
        dob?.setCustomValidity('');
        gender?.setCustomValidity('');
        relationship?.setCustomValidity('');
        if (dob) dob.max = today;
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
        if (!dob?.value) {
            dob?.setCustomValidity('Birthdate is required');
            setKioskError('');
            return false;
        }
        if (dob.value > today) {
            dob.setCustomValidity('Birthdate cannot be in the future');
            setKioskError('');
            return false;
        }
        if (!gender?.value) {
            gender?.setCustomValidity('Gender is required');
            setKioskError('');
            return false;
        }
        if (!relationship?.value) {
            relationship?.setCustomValidity('Relationship is required');
            setKioskError('');
            return false;
        }
    }

    setKioskError('');
    return kioskForm.checkValidity();
}

async function submitKioskForm() {
    if (submitButton && submitButton.disabled) return;
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

// Handle bfcache/page refresh where browser may show a cached date but
// input.value is empty (date inputs restore locale string which is invalid
// for type="date" → value stays "" → required fails). Clear ghost display
// and re-apply max so validation is consistent.
function handlePageshow(event) {
    const isPersisted = Boolean(event && event.persisted);
    // Also handle normal reload where browser autofills after JS created rows
    // Use a microtask to let browser finish autofill
    setTimeout(() => {
        const today = new Date().toLocaleDateString('en-CA');
        childrenContainer.querySelectorAll('.child-dob').forEach(input => {
            input.max = today;
            // If input appears filled (has attribute value or autofill) but
            // value is empty and validity is valueMissing, clear any stale
            // display by resetting value to itself (forces browser to sync)
            if (!input.value && input.hasAttribute('value') && input.getAttribute('value')) {
                input.value = input.getAttribute('value');
            }
            // Detect WebKit/Blink autofill ghost: matches :autofill but value empty
            try {
                if (!input.value && input.matches(':autofill')) {
                    // Force browser to re-evaluate; clearing and refocusing helps
                    const val = input.getAttribute('value') || '';
                    input.value = val;
                }
            } catch (_) { }
            input.setCustomValidity('');
        });
        if (isPersisted) {
            validateForm();
        }
    }, 0);
}

window.addEventListener('pageshow', handlePageshow);
window.addEventListener('DOMContentLoaded', () => handlePageshow({ persisted: false }));
window.handlePageshow = handlePageshow;

window.addChildRow = addChildRow;
window.removeChildRow = removeChildRow;
window.updateChildCount = updateChildCount;
window.syncChildrenLastName = syncChildrenLastName;
window.handleParentLastNameInput = handleParentLastNameInput;
window.handleUseParentLastNameChange = handleUseParentLastNameChange;
window.setUseParentLastNameToggleVisual = setUseParentLastNameToggleVisual;
window.toggleDietaryPill = toggleDietaryPill;
window.syncDietaryPills = syncDietaryPills;
window.parseDietaryTokens = parseDietaryTokens;
window.DIETARY_PRESETS = DIETARY_PRESETS;

setUseParentLastNameToggleVisual(document.getElementById('use-parent-last-name')?.checked);
window.buildPayload = buildPayload;
window.resetForm = resetForm;
window.showWelcome = showWelcome;
window.showForm = showForm;
window.startCountdown = startCountdown;
window.stopCountdown = stopCountdown;
window.validateForm = validateForm;
window.submitKioskForm = submitKioskForm;
