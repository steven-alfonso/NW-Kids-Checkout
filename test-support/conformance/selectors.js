// The DOM hooks the conformance suite is allowed to depend on. Keep this file
// as the single place that names them; a replacement implementation must
// satisfy these, but may change everything underneath.

export function childCard(page, id) {
  return page.locator(`#children-list > div:has([data-child-id="${id}"])`);
}

export function childCheckbox(page, id) {
  return page.locator(`#children-list input.child-confirmed-checkbox[data-child-id="${id}"]`);
}

export function childLabel(page, id) {
  return childCard(page, id).locator('[data-confirmed-label]');
}

export function timeText(page, id) {
  return page.locator(`#children-list .child-time[data-child-id="${id}"]`);
}

export function visibleChildRows(page) {
  return page.locator('#children-list .child-time[data-child-id]');
}

export async function childIds(page) {
  return visibleChildRows(page).evaluateAll((elements) =>
    elements.map((element) => element.dataset.childId)
  );
}

export function overdueRow(page, id) {
  return page.locator(`#overdue-sheet-list > div:has([data-child-id="${id}"])`);
}

export function overdueCheckbox(page, id) {
  return page.locator(
    `#overdue-sheet-list input.child-confirmed-checkbox[data-child-id="${id}"]`
  );
}

export function overdueLabel(page, id) {
  return overdueRow(page, id).locator('[data-confirmed-label]');
}

export async function overdueIds(page) {
  return page
    .locator('#overdue-sheet-list .child-time[data-child-id]')
    .evaluateAll((elements) => elements.map((element) => element.dataset.childId));
}

export function groupCheckbox(page, id) {
  return page.locator(`#location-group-checkboxes input[data-lg-id="${id}"]`);
}

export async function isGroupChecked(page, id) {
  return groupCheckbox(page, id).isChecked();
}
