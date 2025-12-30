import { test, expect } from '@playwright/test';

test.describe('Terminal Reorder E2E', () => {
    test.beforeEach(async ({ page }) => {
        // Mock state with 2 workspaces
        await page.route('**/state', async (route) => {
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    docs_heads: { 'sudal': 'hash1' },
                    workspaces: [
                        { workspace_id: 'sudal:/path/ws1', project: 'sudal', local_path: '/path/ws1', repo_url: '', docs_repo_id: '', docs_hash: 'hash1', last_actor_email: '' },
                        { workspace_id: 'sudal:/path/ws2', project: 'sudal', local_path: '/path/ws2', repo_url: '', docs_repo_id: '', docs_hash: 'hash1', last_actor_email: '' }
                    ]
                })
            });
        });

        // Mock session creation
        await page.route('**/pty/sessions', async (route) => {
            const body = route.request().postDataJSON();
            // Extracted title for sudal:/path/ws1 will be ws1
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    session_id: `session-${body.workspace_id}`,
                    ws_url: `ws://localhost:5789/ws/${body.workspace_id}`,
                    resolved_cwd: `/abs/${body.workspace_id}`
                })
            });
        });

        await page.goto('/terminal');
    });

    test('should reorder terminal sessions and maintain selection', async ({ page }) => {
        // 1. Create 2 sessions
        // Session 1
        await page.getByRole('button', { name: 'New' }).click();
        await expect(page.getByText('Loading workspaces...')).not.toBeVisible();
        await page.locator('.workspace-card').filter({ hasText: 'ws1' }).click();
        await expect(page.getByText('Session created: ws1')).toBeVisible();
        
        // Session 2
        await page.getByRole('button', { name: 'New' }).click();
        await expect(page.getByText('Loading workspaces...')).not.toBeVisible();
        await page.locator('.workspace-card').filter({ hasText: 'ws2' }).click();
        await expect(page.getByText('Session created: ws2')).toBeVisible();

        // Initially, Session 2 should be selected and visible
        const item1 = page.getByTestId('console-item-ws1');
        const item2 = page.getByTestId('console-item-ws2');
        
        // Verify Session 2 terminal is visible
        await expect(page.locator('.terminal-pane').filter({ hasText: 'ws2' }).first()).toBeVisible();
        await expect(page.locator('.terminal-pane').filter({ hasText: 'ws1' }).first()).not.toBeVisible();
        
        // 2. Drag Session 2 to position of Session 1
        // Manually simulate drag to ensure sensors trigger correctly
        const box1 = await item1.boundingBox();
        const box2 = await item2.boundingBox();
        if (box1 && box2) {
            await page.mouse.move(box2.x + box2.width / 2, box2.y + box2.height / 2);
            await page.mouse.down();
            // Move slowly to ensure distance constraint is met
            await page.mouse.move(box1.x + box1.width / 2, box1.y + box1.height / 2, { steps: 10 });
            await page.mouse.up();
        }

        // 3. Verify order changed in the DOM
        const items = page.locator('.console-item');
        await expect(items.first()).toContainText('ws2');
        await expect(items.last()).toContainText('ws1');

        // 4. Verify selection is still on Session 2 (terminal still visible)
        await expect(page.locator('.terminal-pane').filter({ hasText: 'ws2' }).first()).toBeVisible();
        await expect(page.locator('.terminal-pane').filter({ hasText: 'ws1' }).first()).not.toBeVisible();
    });
});
