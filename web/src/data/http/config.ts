/**
 * API configuration for the kkachi-web client.
 *
 * The client always uses a relative path prefix (e.g., "/api").
 * In development, Vite's proxy forwards these requests to the actual server.
 * In production, the server serves both the SPA and handles /api routes.
 */
export interface ApiConfig {
    /** Base path prefix for API calls (e.g., "/api") */
    apiPrefix: string;
}

/**
 * Error thrown when API configuration is invalid.
 */
export class ApiConfigError extends Error {
    constructor(message: string) {
        super(message);
        this.name = 'ApiConfigError';
    }
}

/**
 * Validates that the given prefix is a same-origin relative path.
 * Full URLs (http://, https://, //) are not allowed for security reasons.
 *
 * @param prefix - The API prefix to validate
 * @throws ApiConfigError if prefix is a full URL
 */
function validateApiPrefix(prefix: string): void {
    // Check for full URL patterns
    if (/^https?:\/\//i.test(prefix) || prefix.startsWith('//')) {
        throw new ApiConfigError(
            `Invalid API prefix "${prefix}": full URLs are not allowed. Use a relative path like "/api".`
        );
    }
}

/**
 * Get the API configuration from environment variables.
 *
 * @returns ApiConfig with VITE_KKACHI_API_PREFIX or default "/api"
 * @throws ApiConfigError if prefix is a full URL
 */
export function getApiConfig(): ApiConfig {
    const prefix = import.meta.env.VITE_KKACHI_API_PREFIX || '/api';
    validateApiPrefix(prefix);
    return {
        apiPrefix: prefix,
    };
}

/**
 * Build the full URL for an API endpoint.
 * Automatically ensures the endpoint starts with a slash.
 * @param endpoint - API endpoint (e.g., "/state" or "state")
 * @returns Full URL path (e.g., "/api/state")
 * @throws ApiConfigError if API prefix is invalid
 */
export function buildApiUrl(endpoint: string): string {
    const { apiPrefix } = getApiConfig();
    // Normalize prefix: remove trailing slash if present
    const normalizedPrefix = apiPrefix.endsWith('/') ? apiPrefix.slice(0, -1) : apiPrefix;
    // Ensure endpoint starts with a slash
    const normalizedEndpoint = endpoint.startsWith('/') ? endpoint : `/${endpoint}`;
    return `${normalizedPrefix}${normalizedEndpoint}`;
}

