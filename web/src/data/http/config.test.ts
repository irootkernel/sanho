import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { getApiConfig, buildApiUrl, ApiConfigError } from './config'

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

        it('should throw ApiConfigError for http:// URLs', () => {
            import.meta.env.VITE_KKACHI_API_PREFIX = 'http://example.com/api'

            expect(() => getApiConfig()).toThrow(ApiConfigError)
            expect(() => getApiConfig()).toThrow(/full URLs are not allowed/)
        })

        it('should throw ApiConfigError for https:// URLs', () => {
            import.meta.env.VITE_KKACHI_API_PREFIX = 'https://example.com/api'

            expect(() => getApiConfig()).toThrow(ApiConfigError)
            expect(() => getApiConfig()).toThrow(/full URLs are not allowed/)
        })

        it('should throw ApiConfigError for protocol-relative URLs', () => {
            import.meta.env.VITE_KKACHI_API_PREFIX = '//example.com/api'

            expect(() => getApiConfig()).toThrow(ApiConfigError)
            expect(() => getApiConfig()).toThrow(/full URLs are not allowed/)
        })

        it('should throw ApiConfigError for non-slash-prefixed values', () => {
            import.meta.env.VITE_KKACHI_API_PREFIX = 'api'

            expect(() => getApiConfig()).toThrow(ApiConfigError)
            expect(() => getApiConfig()).toThrow(/must start with "\/"/)
        })

        it('should throw ApiConfigError for query strings or fragments', () => {
            import.meta.env.VITE_KKACHI_API_PREFIX = '/api?x=1'
            expect(() => getApiConfig()).toThrow(ApiConfigError)

            import.meta.env.VITE_KKACHI_API_PREFIX = '/api#hash'
            expect(() => getApiConfig()).toThrow(ApiConfigError)
        })

        it('should allow relative paths without protocol', () => {
            import.meta.env.VITE_KKACHI_API_PREFIX = '/my/api/v2'

            const config = getApiConfig()

            expect(config.apiPrefix).toBe('/my/api/v2')
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

        it('should throw ApiConfigError when prefix is a full URL', () => {
            import.meta.env.VITE_KKACHI_API_PREFIX = 'https://malicious.com'

            expect(() => buildApiUrl('/state')).toThrow(ApiConfigError)
        })
    })
})
