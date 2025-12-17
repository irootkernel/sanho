import { test, expect } from '@playwright/test';

const MOCK_STATE = {
    docs_heads: {
        sudal: 'abc123def456',
        kkachi: '789ghi012jkl',
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
            last_actor_email: 'dev@example.com',
        },
        {
            workspace_id: 'ws-002',
            project: 'sudal',
            docs_repo_id: 'docs-sudal',
            local_path: '/Users/dev2/projects/sudal',
            repo_url: 'https://github.com/example/sudal-fork',
            docs_hash: 'old-hash-123',
            last_reported_at: '2024-12-13T15:30:00Z',
            last_actor_email: 'dev2@example.com',
        },
        {
            workspace_id: 'ws-003',
            project: 'kkachi',
            docs_repo_id: 'docs-kkachi',
            local_path: '/Users/dev/projects/kkachi',
            repo_url: 'https://github.com/example/kkachi',
            docs_hash: '789ghi012jkl',
            last_reported_at: '2024-12-15T08:00:00Z',
            last_actor_email: 'dev@example.com',
        },
    ],
};

test.describe('Projects Dashboard → Detail Happy Path', () => {
    test.beforeEach(async ({ page }) => {
        // Mock the API response to ensure consistent data
        await page.route('**/api/state', async (route) => {
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(MOCK_STATE),
            });
        });
    });

    test('should navigate from dashboard to project detail', async ({
        page,
    }) => {
        // Step 1: Navigate to dashboard
        await page.goto('/');

        // Step 2: Verify dashboard renders with project list
        await expect(
            page.getByRole('heading', { name: /Projects Dashboard/i })
        ).toBeVisible();

        // Wait for table to load
        await expect(page.locator('.projects-table')).toBeVisible();
        await expect(page.getByRole('cell', { name: 'sudal' })).toBeVisible();
        await expect(page.getByRole('cell', { name: 'kkachi' })).toBeVisible();

        // Step 3: Click on a project row
        await page.getByRole('row', { name: /sudal/i }).click();

        // Step 4: Verify navigation to detail page
        await expect(page).toHaveURL(/\/projects\/sudal/);
        await expect(page.getByRole('heading', { name: 'sudal' })).toBeVisible();

        // Step 5: Verify workspace table is rendered
        await expect(page.locator('table')).toBeVisible();
        await expect(page.getByText('/Users/dev/projects/sudal')).toBeVisible();
        await expect(
            page.getByText('/Users/dev2/projects/sudal')
        ).toBeVisible();
    });

    test('should show correct project statistics', async ({ page }) => {
        await page.goto('/');

        // Wait for data
        await expect(page.locator('.projects-table')).toBeVisible();

        // Check workspace count for sudal (should be 2)
        const sudalRow = page.getByRole('row', { name: /sudal/i });
        await expect(sudalRow).toContainText('2');

        // Check workspace count for kkachi (should be 1)
        const kkachiRow = page.getByRole('row', { name: /kkachi/i });
        await expect(kkachiRow).toContainText('1');
    });

    test('should toggle sort mode', async ({ page }) => {
        await page.goto('/');
        await expect(page.locator('.projects-table')).toBeVisible();

        // Default: alphabetical - first should be 'kkachi'
        const rows = page.locator('tbody tr');
        const firstRowName = await rows.nth(0).locator('.project-name').innerText();
        expect(firstRowName).toBe('kkachi');

        // Toggle to "Outdated First"
        await page.getByRole('button', { name: /Sort by Name/i }).click();
        await expect(page.getByRole('button', { name: /Outdated First/i })).toBeVisible();

        // sudal has outdated workspaces, so it should now be first
        const newFirstRowName = await rows.nth(0).locator('.project-name').innerText();
        expect(newFirstRowName).toBe('sudal');
    });

    test('should use cached state when navigating back', async ({ page }) => {
        let apiCallCount = 0;

        await page.route('**/api/state', async (route) => {
            apiCallCount++;
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(MOCK_STATE),
            });
        });

        // Navigate to dashboard
        await page.goto('/');
        await expect(page.locator('.projects-table')).toBeVisible();
        const initialCallCount = apiCallCount;

        // Navigate to detail page (should use cached state)
        await page.getByRole('row', { name: /sudal/i }).click();
        await expect(page.getByRole('heading', { name: 'sudal' })).toBeVisible();

        // Navigate back to dashboard via breadcrumb
        await page.getByRole('link', { name: /Projects/i }).click();
        await expect(
            page.getByRole('heading', { name: /Projects Dashboard/i })
        ).toBeVisible();

        // API should have been called only once (on initial load)
        expect(apiCallCount).toBe(initialCallCount);
    });

    test('should refetch when refresh button is clicked', async ({ page }) => {
        let apiCallCount = 0;

        await page.route('**/api/state', async (route) => {
            apiCallCount++;
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(MOCK_STATE),
            });
        });

        await page.goto('/');
        await expect(page.locator('.projects-table')).toBeVisible();
        const initialCallCount = apiCallCount;

        // Click refresh button (use the one in main content area, not header)
        await page.getByRole('main').getByRole('button', { name: /🔄 Refresh/i }).click();

        // API should have been called again
        await expect
            .poll(() => apiCallCount)
            .toBeGreaterThan(initialCallCount);
    });
});

test.describe('Projects Dashboard Empty and Error States', () => {
    test('should show empty state when no projects', async ({ page }) => {
        await page.route('**/api/state', async (route) => {
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    docs_heads: {},
                    workspaces: [],
                }),
            });
        });

        await page.goto('/');
        await expect(page.getByText(/No Projects Yet/i)).toBeVisible();
        await expect(page.getByText(/kkachi init/i)).toBeVisible();
    });

    test('should show error state and allow retry', async ({
        page,
        context,
    }) => {
        let allowSuccess = false;

        await context.route('**/api/state', (route) => {
            if (!allowSuccess) {
                return route.fulfill({
                    status: 500,
                    contentType: 'application/json',
                    body: JSON.stringify({ error: 'Server error' }),
                });
            }

            return route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(MOCK_STATE),
            });
        });

        await page.goto('/');
        await expect(page.getByText(/Failed to Load Projects/i)).toBeVisible({
            timeout: 10000,
        });

        allowSuccess = true;

        await page.getByRole('button', { name: /🔄 Retry/i }).click();
        await expect(page.locator('.projects-table')).toBeVisible({
            timeout: 10000,
        });
    });
});
