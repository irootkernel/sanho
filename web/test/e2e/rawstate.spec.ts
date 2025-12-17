import { test, expect } from '@playwright/test'

/**
 * E2E tests for RawStatePage (/debug/state)
 *
 * These tests mock /api/state for determinism and do not require kkachi-server.
 */

const MOCK_STATE = {
    docs_heads: { test: 'abc123' },
    workspaces: [],
}

test.describe('RawStatePage E2E', () => {
    test.beforeEach(async ({ page }) => {
        await page.route('**/api/state*', async (route) => {
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(MOCK_STATE),
            })
        })

        // Navigate to the debug state page
        await page.goto('/debug/state')
    })

    test('should display page heading', async ({ page }) => {
        await expect(page.getByRole('heading', { name: /Debug: Raw State/i })).toBeVisible()
    })

    test('should load and display state data', async ({ page }) => {
        // Wait for the data to load (badges should appear)
        // Use .badge class to be more specific and avoid matching JSON content
        await expect(page.locator('.badge').filter({ hasText: /projects/i })).toBeVisible({ timeout: 10000 })
        await expect(page.locator('.badge').filter({ hasText: /workspaces/i })).toBeVisible()
    })

    test('should display JSON data in pre element', async ({ page }) => {
        // Wait for JSON to be rendered
        const jsonDisplay = page.locator('.json-display')
        await expect(jsonDisplay).toBeVisible({ timeout: 10000 })

        // Should contain expected JSON structure
        const jsonText = await jsonDisplay.textContent()
        expect(jsonText).toContain('docs_heads')
        expect(jsonText).toContain('workspaces')
    })

    test('should have a working refresh button', async ({ page }) => {
        let apiCallCount = 0
        await page.unroute('**/api/state*')
        await page.route('**/api/state*', async (route) => {
            apiCallCount++
            await route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(MOCK_STATE),
            })
        })

        // Reload so the new route is used for initial load.
        await page.reload()
        await expect(page.locator('.badge').first()).toBeVisible({
            timeout: 10000,
        })
        const initialCallCount = apiCallCount

        // Get initial JSON content
        const jsonDisplay = page.locator('.json-display')
        await expect(jsonDisplay).toBeVisible()

        // Click the refresh button in the main content area (not the header one)
        await page.getByRole('main').getByRole('button', { name: /🔄 Refresh/i }).click()

        await expect
            .poll(() => apiCallCount)
            .toBeGreaterThan(initialCallCount)

        // Content should still be present (may be same or updated)
        const refreshedContent = await jsonDisplay.textContent()
        expect(refreshedContent).toContain('docs_heads')
    })

    test('should display project and workspace counts', async ({ page }) => {
        // Wait for badges to appear
        await expect(page.locator('.badge').first()).toBeVisible({ timeout: 10000 })

        // Get all badges
        const badges = page.locator('.badge')
        const badgeCount = await badges.count()

        // Should have at least 2 badges (projects and workspaces)
        expect(badgeCount).toBeGreaterThanOrEqual(2)
    })

    test('should navigate from dashboard to debug page', async ({ page }) => {
        // Start from homepage
        await page.goto('/')

        // Click on Debug link in nav
        await page.getByRole('link', { name: /Debug/i }).click()

        // Should be on debug/state page
        await expect(page).toHaveURL(/\/debug\/state/)
        await expect(page.getByRole('heading', { name: /Debug: Raw State/i })).toBeVisible()
    })

    test('should show header with Kkachi Web v2 title', async ({ page }) => {
        await expect(page.getByRole('heading', { name: /Kkachi Web v2/i })).toBeVisible()
    })

    test('should have navigation links', async ({ page }) => {
        await expect(page.getByRole('link', { name: /Dashboard/i })).toBeVisible()
        await expect(page.getByRole('link', { name: /Debug/i })).toBeVisible()
    })

    test('should display error state when server is down', async ({ page, context }) => {
        await page.unroute('**/api/state*')
        await context.route('**/api/state*', (route) => {
            route.fulfill({
                status: 500,
                contentType: 'application/json',
                body: JSON.stringify({ error: 'Server error' }),
            })
        })

        // Reload the page to trigger the mocked request
        await page.reload()

        // Should show error message
        await expect(page.getByText(/Failed to Load State/i)).toBeVisible({ timeout: 10000 })
    })

    test('should retry when retry button is clicked after error', async ({ page, context }) => {
        await page.unroute('**/api/state*')

        let allowSuccess = false

        await context.route('**/api/state*', (route) => {
            if (!allowSuccess) {
                return route.fulfill({
                    status: 500,
                    contentType: 'application/json',
                    body: JSON.stringify({ error: 'Server error' }),
                })
            }

            return route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify({
                    docs_heads: { test: 'abc123' },
                    workspaces: [],
                }),
            })
        })

        // Reload to ensure the mocked route is used.
        await page.reload()

        // Wait for error state
        await expect(page.getByText(/Failed to Load State/i)).toBeVisible({ timeout: 10000 })

        allowSuccess = true

        // Click retry
        await page.getByRole('button', { name: /🔄 Retry/i }).click()

        // Should now show success state
        await expect(page.locator('.badge').filter({ hasText: /1 projects/i })).toBeVisible({ timeout: 10000 })
    })
})

test.describe('RawStatePage Responsive', () => {
    test('should be accessible on mobile viewport', async ({ page }) => {
        await page.setViewportSize({ width: 375, height: 667 })
        await page.goto('/debug/state')

        await expect(page.getByRole('heading', { name: /Debug: Raw State/i })).toBeVisible()
        await expect(page.getByText(/projects/i)).toBeVisible({ timeout: 10000 })
    })

    test('should be accessible on tablet viewport', async ({ page }) => {
        await page.setViewportSize({ width: 768, height: 1024 })
        await page.goto('/debug/state')

        await expect(page.getByRole('heading', { name: /Debug: Raw State/i })).toBeVisible()
        await expect(page.getByText(/projects/i)).toBeVisible({ timeout: 10000 })
    })
})

test.describe('RawStatePage Performance', () => {
    test('should load within 3 seconds', async ({ page }) => {
        const startTime = Date.now()

        await page.goto('/debug/state')
        await expect(page.getByText(/projects/i)).toBeVisible({ timeout: 10000 })

        const loadTime = Date.now() - startTime
        expect(loadTime).toBeLessThan(3000)
    })
})
