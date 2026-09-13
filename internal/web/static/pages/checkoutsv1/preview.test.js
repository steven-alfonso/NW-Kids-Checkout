import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const checkoutsPath = path.resolve(process.cwd(), 'internal/web/static/pages/checkoutsv1/checkouts.js');
const previewPath = path.resolve(process.cwd(), 'internal/web/dev-assets/preview.js');
const checkoutsScript = fs.readFileSync(checkoutsPath, 'utf8');
const previewScript = fs.readFileSync(previewPath, 'utf8');

function loadWindow() {
    const dom = new JSDOM('<!doctype html><html><body></body></html>', {
        runScripts: 'dangerously',
        url: 'http://localhost/'
    });
    dom.window.fetch = async () => ({
        ok: true,
        json: async () => [],
        text: async () => ''
    });
    dom.window.setInterval = () => 0;
    dom.window.eval(checkoutsScript);
    // No Alpine here: drive the board factory directly, like init() would.
    dom.window.__checkoutsBoard = dom.window.checkoutsBoardData();
    dom.window.eval(previewScript);
    return dom.window;
}

describe('checkoutsv1/preview', () => {
    it('loadPreviewData seeds demo checkouts and blocks auto-refresh', () => {
        const window = loadWindow();
        window.loadPreviewData();
        const board = window.__checkoutsBoard;

        expect(board.fetchBlocked).toBe(true);

        expect(board.children).toHaveLength(10);
        expect(board.children.map((child) => child.source))
            .toEqual(['planning_center', 'planning_center', 'planning_center', 'planning_center', 'planning_center', 'manual', 'manual', 'manual', 'manual', 'manual']);
        expect(board.children.map((child) => child.planning_center_id || child.public_id))
            .toEqual(['demo-0', 'demo-4', 'demo-5', 'demo-8', 'demo-c', 'demo-m0', 'demo-m4', 'demo-m5', 'demo-m8', 'demo-mc']);
        expect(board.children[0].checked_out_confirmed_at).toBeNull();
        expect(board.children[4].checked_out_confirmed_at).toBe('2024-01-01T00:00:00Z');
        expect(board.children[9].checked_out_confirmed_at).toBe('2024-01-01T00:00:00Z');
    });

    it('preview seeds location_group_id', () => {
        const w = loadWindow(); w.loadPreviewData();
        expect(w.__checkoutsBoard.children[0].location_group_id).toBeDefined();
        expect(w.__checkoutsBoard.children.map((c) => c.location_group_id)).toEqual([1, 2, 2, 1, null, null, null, null, null, null]);
    });
});
