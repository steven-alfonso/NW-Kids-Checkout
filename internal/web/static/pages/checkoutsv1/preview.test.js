import { describe, it, expect, vi } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { createPreviewChildren } from '../../../dev-assets/preview-data.js';

const previewPath = path.resolve(process.cwd(), 'internal/web/dev-assets/preview.js');
const previewScript = fs.readFileSync(previewPath, 'utf8');

function loadWindow() {
    const dom = new JSDOM('<!doctype html><html><body></body></html>', {
        runScripts: 'dangerously'
    });
    dom.window.eval(previewScript);
    return dom.window;
}

describe('checkoutsv1/preview', () => {
    it('delegates to the Svelte page preview handle', () => {
        const window = loadWindow();
        const loadPreviewData = vi.fn();
        window.__checkoutsPreview = { loadPreviewData };

        window.loadPreviewData();

        expect(loadPreviewData).toHaveBeenCalledTimes(1);
    });

    it('warns instead of throwing when the page is not ready', () => {
        const window = loadWindow();
        const warn = vi.spyOn(window.console, 'warn').mockImplementation(() => {});

        expect(() => window.loadPreviewData()).not.toThrow();
        expect(warn).toHaveBeenCalledTimes(1);
    });

    it('seeds the same demo rows the vanilla preview used', () => {
        const data = createPreviewChildren(Date.parse('2026-01-15T12:00:00.000Z'));

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
        expect(data.map((child) => child.location_group_id)).toEqual([
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
