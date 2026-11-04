import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const previewPath = path.resolve(
  process.cwd(),
  'internal/web/dev-assets/preview.js'
);
const previewScript = fs.readFileSync(previewPath, 'utf8');

function loadWindow() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'dangerously'
  });
  const calls = [];
  dom.window.__checkoutsPreview = {
    loadPreviewData(data) {
      calls.push(data);
    }
  };
  dom.window.eval(previewScript);
  return { window: dom.window, calls };
}

describe('checkoutsv1/preview', () => {
  it('passes demo checkouts to the Preact preview handle', () => {
    const { window, calls } = loadWindow();
    window.loadPreviewData();

    expect(calls).toHaveLength(1);
    const data = calls[0];
    expect(data).toHaveLength(10);
    expect(data.map((child) => child.source)).toEqual([
      'planning_center',
      'planning_center',
      'planning_center',
      'planning_center',
      'planning_center',
      'manual',
      'manual',
      'manual',
      'manual',
      'manual'
    ]);
    expect(data.map((child) => child.planning_center_id || child.public_id)).toEqual([
      'demo-0',
      'demo-4',
      'demo-5',
      'demo-8',
      'demo-c',
      'demo-m0',
      'demo-m4',
      'demo-m5',
      'demo-m8',
      'demo-mc'
    ]);
    expect(data[0].checked_out_confirmed_at).toBeNull();
    expect(data[4].checked_out_confirmed_at).toBe('2024-01-01T00:00:00Z');
    expect(data[9].checked_out_confirmed_at).toBe('2024-01-01T00:00:00Z');
  });

  it('preview seeds location_group_id', () => {
    const { window, calls } = loadWindow();
    window.loadPreviewData();

    expect(calls[0].map((child) => child.location_group_id)).toEqual([
      1,
      2,
      2,
      1,
      null,
      null,
      null,
      null,
      null,
      null
    ]);
  });
});
