import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { getApiConfig, buildApiUrl } from './config'

describe('config', () => {
    const originalEnv = { ...import.meta.env }

    beforeEach(() => {
        vi.resetModules()
    })

    afterEach(() => {
        // Restore original env
        Object.assign(import.meta.env, originalEnv)
    })

    describe('getApiConfig', () => {
        it('should return default /api prefix when no env var is set', () => {
            // Clear any env vars
            delete import.meta.env.VITE_KKACHI_API_PREFIX
            delete import.meta.env.VITE_KKACHI_API_URL

            const config = getApiConfig()

            expect(config.apiPrefix).toBe('/api')
        })

        it('should use VITE_KKACHI_API_PREFIX when set', () => {
            import.meta.env.VITE_KKACHI_API_PREFIX = '/custom-api'

            const config = getApiConfig()

            expect(config.apiPrefix).toBe('/custom-api')
        })
    })

    describe('buildApiUrl', () => {
        it('should build URL with default prefix', () => {
            delete import.meta.env.VITE_KKACHI_API_PREFIX

            const url = buildApiUrl('/state')

            expect(url).toBe('/api/state')
        })

        it('should build URL with custom prefix', () => {
            import.meta.env.VITE_KKACHI_API_PREFIX = '/v2/api'

            const url = buildApiUrl('/state')

            expect(url).toBe('/v2/api/state')
        })

        it('should auto-add leading slash if missing', () => {
            delete import.meta.env.VITE_KKACHI_API_PREFIX

            const url = buildApiUrl('state')

            expect(url).toBe('/api/state')
        })

        it('should handle empty endpoint (adds trailing slash)', () => {
            delete import.meta.env.VITE_KKACHI_API_PREFIX

            const url = buildApiUrl('')

            expect(url).toBe('/api/')
        })
    })
})
