import { test, expect } from '@playwright/test';

const MOCK_STATE = {
    docs_heads: {
        sudal: 'abc123def456',
        kkachi: '789ghi012jkl'
    },
    workspaces: [
        {
            workspace_id: 'ws-001',
            project: 'sudal',
            docs_repo_id: 'docs-sudal',
            local_path: '/Users/dev/projects/sudal',
            repo_url: 'https://github.com/example/sudal',
            docs_hash: 'abc123def456',
            last_reported_at: '2024-12-14T10:00:00Z',
            last_actor_email: 'dev@example.com'
        },
        {
            workspace_id: 'ws-002',
            project: 'sudal',
            docs_repo_id: 'docs-sudal',
            local_path: '/Users/dev2/projects/sudal',
            repo_url: 'https://github.com/example/sudal-fork',
            docs_hash: 'old-hash-123',
            last_reported_at: '2024-12-13T15:30:00Z',
            last_actor_email: 'dev2@example.com'
        }
    ]
};

test.describe('ProjectDetailPage', () => {
    test.beforeEach(async ({ page }) => {
        // Mock the API response to ensure consistent data
        await page.route('**/api/state', async (route) => {
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(MOCK_STATE),
            });
        });

        // Navigate to the project page
        await page.goto('/projects/sudal');
    });

    test('should render project header and metadata', async ({ page }) => {
        // Check for project title
        await expect(page.getByRole('heading', { name: 'sudal' })).toBeVisible();

        // Check for Docs HEAD (use regex as it is truncated)
        await expect(page.locator('.project-meta').getByText(/abc123de/)).toBeVisible();

        // Check for Workspace count (look for "Workspaces: 2")
        await expect(page.locator('.project-meta')).toContainText('2');
    });

    test('should list workspaces with correct columns', async ({ page }) => {
        // Check for table headers
        // Use columnheader role or just text
        await expect(page.getByRole('columnheader', { name: 'Local Path' })).toBeVisible();
        await expect(page.getByRole('columnheader', { name: 'Status' })).toBeVisible();

        // Check for workspace data
        await expect(page.getByText('/Users/dev/projects/sudal')).toBeVisible();
        await expect(page.getByText('/Users/dev2/projects/sudal')).toBeVisible();
    });

    test('should filter workspaces by status', async ({ page }) => {
        // Initial: 2 workspaces
        await expect(page.getByText('/Users/dev/projects/sudal')).toBeVisible();
        await expect(page.getByText('/Users/dev2/projects/sudal')).toBeVisible();

        // Filter by Outdated (ws-002 is outdated because hash differs)
        await page.getByRole('combobox').selectOption('outdated');

        // Should see ws-002 (outdated)
        await expect(page.getByText('/Users/dev2/projects/sudal')).toBeVisible();

        // Should NOT see ws-001 (up-to-date)
        await expect(page.getByText('/Users/dev/projects/sudal')).toBeHidden();
    });

    test('should filter workspaces by search query', async ({ page }) => {
        // Search for specific path part "dev2"
        await page.getByPlaceholder('Search workspace...').fill('dev2');

        // Should see ws-002
        await expect(page.getByText('/Users/dev2/projects/sudal')).toBeVisible();

        // Should NOT see ws-001
        await expect(page.getByText('/Users/dev/projects/sudal')).toBeHidden();
    });

    test('should sort workspaces', async ({ page }) => {
        const rows = page.locator('tbody tr');
        await expect(rows).toHaveCount(2);

        // Get initial order
        const firstRowText = await rows.nth(0).innerText();

        // Click on Path header to toggle sort
        await page.getByRole('button', { name: /Path/i }).click();

        // Wait for order to change
        await expect(async () => {
            const newFirstRowText = await rows.nth(0).innerText();
            expect(newFirstRowText).not.toBe(firstRowText);
        }).toPass({ timeout: 5000 });

        // Click again to toggle back
        await page.getByRole('button', { name: /Path/i }).click();

        // Should revert to initial or at least change again
        await expect(async () => {
            const revertedFirstRowText = await rows.nth(0).innerText();
            expect(revertedFirstRowText).toBe(firstRowText);
        }).toPass({ timeout: 5000 });
    });

    test('should handle project not found', async ({ page }) => {
        await page.goto('/projects/non-existent');
        // Warning: The mock data has "sudal" and "kkachi". "non-existent" is not in MOCK_STATE.
        // The component logic checks if state.docs_heads['non-existent'] exists OR worksapces has it.
        // It returns null -> Project Not Found.

        await expect(page.getByRole('heading', { name: 'Project Not Found' })).toBeVisible();
    });
});
