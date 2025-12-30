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
        await expect(page.getByTestId('no-active-consoles')).toBeVisible();

        // 2. Open Modal
        await page.getByRole('button', { name: /New/i }).click();
        await expect(page.getByRole('dialog')).toBeVisible();
        await expect(page.getByText('Test Project')).toBeVisible();

        // 3. Select Workspace & Create Session
        await page.getByText('ws-test').click();
        
        // Modal should close
        await expect(page.getByRole('dialog')).not.toBeVisible();

        // Console should appear in sidebar
        const consoleItem = page.getByTestId('console-item-ws-test');
        await expect(consoleItem).toBeVisible();
        
        // 4. Terminate Session
        const closeButton = consoleItem.getByTestId('close-console-button');
        await closeButton.click();

        // Console should be removed from sidebar
        await expect(consoleItem).not.toBeVisible();
        await expect(page.getByTestId('no-active-consoles')).toBeVisible();
    });

    test('should support multiple consoles and switching', async ({ page }) => {
        const mockWorkspacesMulti = [
            { workspace_id: 'ws-1', project: 'Proj 1', local_path: '/p1', repo_url: '', docs_repo_id: '', docs_hash: '', last_actor_email: '' },
            { workspace_id: 'ws-2', project: 'Proj 2', local_path: '/p2', repo_url: '', docs_repo_id: '', docs_hash: '', last_actor_email: '' }
        ];

        await page.route(/\/api\/state$/, async (route) => {
            await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ docs_heads: {}, workspaces: mockWorkspacesMulti }) });
        });

        // 1. Create first console
        await page.getByRole('button', { name: /New/i }).click();
        await page.getByText('ws-1').click();
        await expect(page.getByTestId('console-item-ws-1')).toBeVisible();

        // 2. Create second console
        await page.getByRole('button', { name: /New/i }).click();
        await page.getByText('ws-2').click();
        await expect(page.getByTestId('console-item-ws-2')).toBeVisible();

        // 3. Switch back to first console
        await page.getByTestId('console-item-ws-1').click();
        
        // Check that the terminal pane for ws-1 is visible
        // We look for the title in the toolbar of the visible pane
        await expect(page.locator('.terminal-pane').filter({ hasText: 'ws-1' })).toBeVisible();
        
        // 4. Switch to second console
        await page.getByTestId('console-item-ws-2').click();
        await expect(page.locator('.terminal-pane').filter({ hasText: 'ws-2' })).toBeVisible();
        await expect(page.locator('.terminal-pane').filter({ hasText: 'ws-1' })).not.toBeVisible();
    });

    test('should enforce 5-console limit', async ({ page }) => {
        const manyWorkspaces = Array.from({ length: 6 }, (_, i) => ({
            workspace_id: `ws-${i}`, project: `Proj ${i}`, local_path: `/p${i}`, repo_url: '', docs_repo_id: '', docs_hash: '', last_actor_email: ''
        }));

        await page.route(/\/api\/state$/, async (route) => {
            await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ docs_heads: {}, workspaces: manyWorkspaces }) });
        });

        // Open 5 consoles
        for (let i = 0; i < 5; i++) {
            await page.getByRole('button', { name: /New/i }).click();
            await page.getByText(`ws-${i}`).click();
            await expect(page.getByTestId(`console-item-ws-${i}`)).toBeVisible();
        }

        // 6th attempt: 'New' button should be disabled
        const newButton = page.getByRole('button', { name: /New/i });
        await expect(newButton).toBeDisabled();
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
        await page.getByText('ws-test').click();

        // Should show ERROR status in sidebar
        const consoleItem = page.getByTestId('console-item-ws-test');
        await expect(consoleItem).toBeVisible();

        // Should show error message in main pane
        await expect(page.getByText(/Internal Server Error/i).first()).toBeVisible();
    });
});