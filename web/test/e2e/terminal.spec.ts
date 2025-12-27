import { test, expect } from '@playwright/test';

test.describe('TerminalPage E2E', () => {
    const mockWorkspaces = [
        {
            workspace_id: 'ws-test',
            project: 'Test Project',
            local_path: '/tmp/test',
            repo_url: '',
            docs_repo_id: '',
            docs_hash: '',
            last_actor_email: ''
        }
    ];

    test.beforeEach(async ({ page }) => {
        // Mock state API
        await page.route(/\/api\/state$/, async (route) => {
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    docs_heads: {},
                    workspaces: mockWorkspaces
                }),
            });
        });

        // Mock Session Create
        await page.route('/api/pty/sessions', async (route) => {
            if (route.request().method() === 'POST') {
                await route.fulfill({
                    status: 201,
                    contentType: 'application/json',
                    body: JSON.stringify({
                        session_id: 'sess-123',
                        ws_url: '/api/pty/sessions/sess-123/ws',
                        resolved_cwd: '/tmp/test'
                    }),
                });
            }
        });

        // Mock Session Terminate
        await page.route('/api/pty/sessions/sess-123', async (route) => {
            if (route.request().method() === 'DELETE') {
                await route.fulfill({ status: 200 });
            }
        });

        await page.goto('/terminal');
    });

    test('should manage full console lifecycle', async ({ page }) => {
        // 1. Initial State
        await expect(page.getByText(/No active consoles/i)).toBeVisible();

        // 2. Open Modal
        await page.getByRole('button', { name: /New/i }).click();
        await expect(page.getByRole('dialog')).toBeVisible();
        await expect(page.getByText('Test Project')).toBeVisible();

        // 3. Select Workspace & Create Session
        await page.getByText('Test Project').click();
        
        // Modal should close
        await expect(page.getByRole('dialog')).not.toBeVisible();

        // Console should appear in sidebar
        const consoleItem = page.locator('.list-group-item', { hasText: 'Test Project' });
        await expect(consoleItem).toBeVisible();
        
        // Status should eventually be CREATED
        await expect(consoleItem.getByText('CREATED')).toBeVisible();

        // 4. Terminate Session
        const closeButton = page.locator('.terminal-pane .btn-outline-light');
        await closeButton.click();

        // Console should be removed from sidebar
        await expect(consoleItem).not.toBeVisible();
        await expect(page.getByText(/No active consoles/i)).toBeVisible();
    });

    test('should handle session creation error', async ({ page }) => {
        // Mock error for this specific test
        await page.unroute('/api/pty/sessions');
        await page.route('/api/pty/sessions', async (route) => {
            await route.fulfill({
                status: 500,
                contentType: 'application/json',
                body: JSON.stringify({ message: 'Internal Server Error' })
            });
        });

        await page.getByRole('button', { name: /New/i }).click();
        await page.getByText('Test Project').click();

        // Should show ERROR status in sidebar
        const consoleItem = page.locator('.list-group-item', { hasText: 'Test Project' });
        await expect(consoleItem.getByText('ERROR')).toBeVisible();

        // Should show error message in main pane
        await expect(page.getByText(/Internal Server Error/i)).toBeVisible();
    });
});