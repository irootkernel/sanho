import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

/**
 * Vitest configuration for Unit tests.
 * Unit tests focus on pure logic without React/DOM dependencies.
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
            environment: 'node', // Faster for pure logic tests
            // Use unit-specific setup file (no DOM dependencies)
            setupFiles: ['./src/test/setup.unit.ts'],
            include: [
                // Domain layer tests
                'src/domain/**/*.test.ts',
                // Application layer tests (pure usecase logic)
                'src/application/**/*.test.ts',
                // Data layer pure logic tests
                'src/data/http/**/*.test.ts',
            ],
            exclude: [
                '**/*.test.tsx', // Exclude React component tests
                'src/data/repositories/**', // Exclude repository tests (need mocking)
            ],
        },
    }
})
