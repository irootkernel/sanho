import { defineConfig, devices } from '@playwright/test'

const webDevPortValue = process.env.WEB_DEV_PORT || '5790'
const webDevPort = Number(webDevPortValue)

if (!Number.isInteger(webDevPort) || webDevPort < 1 || webDevPort > 65535) {
    throw new Error(`WEB_DEV_PORT must be an integer between 1 and 65535: ${webDevPortValue}`)
}

const webServerURL = `http://localhost:${webDevPort}`

/**
 * Playwright configuration for E2E tests.
 * 
 * Prerequisites:
 * - kkachi-server must be running on port 5789
 * - kkachi-web must be running on port 5790
 * 
 * Quick start: `make run-server-with-web` in one terminal, then `make test-web-e2e`
 * 
 * @see https://playwright.dev/docs/test-configuration
 */
export default defineConfig({
    testDir: './test/e2e',
    // Run tests in files in parallel
    fullyParallel: true,
    // Fail the build on CI if you accidentally left test.only in the source code
    forbidOnly: !!process.env.CI,
    // Retry on CI only
    retries: process.env.CI ? 2 : 0,
    // Opt out of parallel tests on CI
    workers: process.env.CI ? 1 : undefined,
    // Reporter to use
    reporter: [
        ['list'],
        ['html', { open: 'never' }],
    ],
    // Shared settings for all the projects below
    use: {
        // Base URL for web frontend (distinct from KKACHI_E2E_BASE_URL used for server tests)
        baseURL: process.env.WEB_E2E_BASE_URL || webServerURL,
        // Collect trace when retrying the failed test
        trace: 'on-first-retry',
        // Capture screenshot on failure
        screenshot: 'only-on-failure',
    },
    // Configure projects for major browsers
    projects: [
        {
            name: 'chromium',
            use: { ...devices['Desktop Chrome'] },
        },
    ],
    // Web server configuration
    // IMPORTANT: This only starts the web dev server. The kkachi-server (port 5789)
    // must be started separately before running E2E tests.
    // Use `make run-server-with-web` to start both, or run them separately:
    //   - Terminal 1: `make run-server`
    //   - Terminal 2: `make test-web-e2e`
    webServer: [
        {
            command: 'npm run dev',
            url: webServerURL,
            env: {
                WEB_DEV_PORT: String(webDevPort),
            },
            reuseExistingServer: !process.env.CI,
            timeout: 30000,
        },
    ],
})
