// Mutable fake of the checkouts API. Every test gets its own state, so polls,
// confirmations, and simulated failures are isolated.
import { BASE_TIME, GROUPS, defaultChildren, childKey } from './fixtures.js';

export function createState(overrides = {}) {
  return {
    groups: GROUPS,
    children: defaultChildren(),
    groupsStatus: 200,
    groupsGate: null,
    ignoreLimit: false,
    nextPollFailure: null,
    confirmStatus: 204,
    applyConfirmations: true,
    confirmGate: null,
    requests: [],
    ...overrides
  };
}

function requestJSON(request) {
  try {
    return request.postDataJSON();
  } catch {
    return null;
  }
}

function findChild(state, id) {
  return state.children.find((child) => childKey(child) === id);
}

function applyCheckoutFilters(children, params, groups, ignoreLimit) {
  let out = children.slice();

  const ids = new Set();
  for (const raw of params.getAll('location_group_id')) {
    for (const part of raw.split(',')) {
      const id = Number(part.trim());
      if (Number.isFinite(id) && id > 0) ids.add(id);
    }
  }

  const names = new Set();
  for (const raw of params.getAll('location_group_name')) {
    for (const part of raw.split(',')) {
      const name = part.trim();
      if (name) names.add(name);
    }
  }
  for (const name of names) {
    const group = groups.find((candidate) => candidate.name === name);
    if (group && Number.isFinite(Number(group.id))) ids.add(Number(group.id));
  }

  const includeUnassigned = ['1', 'true'].includes(params.get('include_unassigned'));
  const hasGroupParam =
    params.has('location_group_id') ||
    params.has('location_group_name') ||
    params.has('include_unassigned');

  if (hasGroupParam) {
    if (ids.size === 0 && !includeUnassigned) return [];
    out = out.filter((child) => {
      if (child.source === 'manual') return true;
      if (child.location_group_id == null) return includeUnassigned;
      return ids.has(Number(child.location_group_id));
    });
  }

  if (!ignoreLimit) {
    const limit = Number(params.get('limit') || 100);
    if (Number.isFinite(limit) && limit > 0) out = out.slice(0, limit);
  }
  return out;
}

function listPayload(children) {
  return {
    checkins: children.filter((child) => child.source !== 'manual'),
    manual_checkins: children.filter((child) => child.source === 'manual')
  };
}

export async function installFakeApi(page, initialState = createState()) {
  const state = initialState;

  await page.route('**/v1/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname.replace(/\/+$/, '') || '/';
    const body = requestJSON(request);

    state.requests.push({
      method: request.method(),
      path,
      search: url.search,
      body
    });

    if (path === '/v1/location_groups') {
      if (state.groupsGate) await state.groupsGate();
      if (state.groupsStatus !== 200) {
        await route.fulfill({
          status: state.groupsStatus,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'forced groups failure' })
        });
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(state.groups)
      });
      return;
    }

    if (path === '/v1/checkins/checkouts') {
      if (state.nextPollFailure) {
        const mode = state.nextPollFailure;
        state.nextPollFailure = null;

        if (mode === 'abort') {
          await route.abort('failed');
          return;
        }

        if (mode === 'malformed') {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: '<html>not json</html>'
          });
          return;
        }

        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'forced poll failure' })
        });
        return;
      }

      const filtered = applyCheckoutFilters(
        state.children,
        url.searchParams,
        state.groups,
        state.ignoreLimit
      );
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(listPayload(filtered))
      });
      return;
    }

    const manualMatch = path.match(
      /^\/v1\/checkins\/manual-checkins\/([^/]+)\/checked_out_confirmed$/
    );
    const planningCenterMatch = path.match(
      /^\/v1\/checkins\/([^/]+)\/checked_out_confirmed$/
    );

    if (request.method() === 'PATCH' && (manualMatch || planningCenterMatch)) {
      if (state.confirmGate) await state.confirmGate();

      if (state.confirmStatus !== 204) {
        await route.fulfill({
          status: state.confirmStatus,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'forced confirmation failure' })
        });
        return;
      }

      if (state.applyConfirmations) {
        const child = manualMatch
          ? state.children.find(
              (candidate) => candidate.public_id === decodeURIComponent(manualMatch[1])
            )
          : findChild(state, `pc:${decodeURIComponent(planningCenterMatch[1])}`);
        if (child) {
          child.checked_out_confirmed_at = body?.confirmed
            ? BASE_TIME.toISOString()
            : null;
        }
      }

      await route.fulfill({ status: 204, body: '' });
      return;
    }

    await route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ error: `unhandled fake API request: ${request.method()} ${path}` })
    });
  });

  return {
    state,
    requests: state.requests,
    addChild(child) {
      state.children.push(child);
    },
    setChildren(children) {
      state.children = children;
    },
    removeChild(id) {
      const index = state.children.findIndex((child) => childKey(child) === id);
      if (index >= 0) state.children.splice(index, 1);
    },
    setConfirmed(id, confirmed) {
      const child = findChild(state, id);
      if (child) {
        child.checked_out_confirmed_at = confirmed ? BASE_TIME.toISOString() : null;
      }
    },
    failNextPoll(mode = 'http') {
      state.nextPollFailure = mode;
    },
    setConfirmStatus(status) {
      state.confirmStatus = status;
    },
    setApplyConfirmations(value) {
      state.applyConfirmations = value;
    },
    holdConfirmations() {
      let release;
      const gate = new Promise((resolve) => {
        release = resolve;
      });
      state.confirmGate = () => gate;
      return () => {
        state.confirmGate = null;
        release();
      };
    },
    lastPollRequest() {
      return [...state.requests]
        .reverse()
        .find((entry) => entry.path === '/v1/checkins/checkouts');
    },
    confirmationRequests() {
      return state.requests.filter((entry) => entry.method === 'PATCH');
    }
  };
}
