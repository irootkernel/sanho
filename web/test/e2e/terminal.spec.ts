import { test, expect } from '@playwright/test';

test.describe('TerminalPage E2E', () => {
    test.beforeEach(async ({ page }) => {
        // Mock state API to ensure we have context
        await page.route('/api/state', async (route) => {
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    docs_heads: {},
                    workspaces: []
                }),
            });
        });
        await page.goto('/terminal');
    });

    test('should display terminal layout elements', async ({ page }) => {
        // Check sidebar header
        await expect(page.getByRole('heading', { name: /Consoles/i })).toBeVisible();
        
        // Check empty state message
        await expect(page.getByText(/No active consoles/i)).toBeVisible();
        
        // Check main pane placeholder
        await expect(page.getByText(/Select a console from the list to start/i)).toBeVisible();
    });

    test('should have a working "New" console button', async ({ page }) => {
        const newButton = page.getByRole('button', { name: /New/i });
        await expect(newButton).toBeVisible();
        
        // Currently the button doesn't do much until CTASK-2, 
        // but we verify it exists and is clickable.
        await newButton.click();
    });

    test('should have navigation link back to dashboard', async ({ page }) => {
        const dashboardLink = page.getByRole('link', { name: /Dashboard/i });
        await dashboardLink.click();
        await expect(page).toHaveURL('/');
    });
});
