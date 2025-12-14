import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

/**
 * Vitest configuration for Integration tests.
 * Integration tests focus on React components with mocked dependencies.
 */
export default defineConfig(({ mode }) => {
    const env = loadEnv(mode, process.cwd(), '')
    void env // suppress unused warning

    return {
        plugins: [react()],
        resolve: {
            alias: {
                '@': path.resolve(__dirname, './src'),
            },
        },
        test: {
            globals: true,
            environment: 'jsdom', // Required for React components
            setupFiles: ['./src/test/setup.ts'],
            include: [
                // React component tests
                'src/presentation/**/*.test.tsx',
                // Repository tests (need fetch mocking)
                'src/data/repositories/**/*.test.ts',
            ],
        },
    }
})
