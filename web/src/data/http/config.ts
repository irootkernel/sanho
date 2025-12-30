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
    /** Optional authentication token for API calls */
    authToken?: string;
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

    // Enforce same-origin absolute path prefix (starts with "/")
    if (!prefix.startsWith('/')) {
        throw new ApiConfigError(
            `Invalid API prefix "${prefix}": must start with "/". Use a path like "/api".`
        );
    }

    // Disallow URL fragments/query strings in prefix (path prefix only)
    if (prefix.includes('?') || prefix.includes('#')) {
        throw new ApiConfigError(
            `Invalid API prefix "${prefix}": query strings and fragments are not allowed. Use a path like "/api".`
        );
    }

    // Disallow whitespace in prefix
    if (/\s/.test(prefix)) {
        throw new ApiConfigError(
            `Invalid API prefix "${prefix}": whitespace is not allowed. Use a path like "/api".`
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
    const token = import.meta.env.VITE_KKACHI_AUTH_TOKEN;
    validateApiPrefix(prefix);
    return {
        apiPrefix: prefix,
        authToken: token,
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

/**
 * Get standard headers for API calls, including Authorization if token is set.
 * @returns Headers object
 */
export function getApiHeaders(contentType = 'application/json'): Record<string, string> {
    const { authToken } = getApiConfig();
    const headers: Record<string, string> = {};

    if (contentType) {
        headers['Content-Type'] = contentType;
    }

    if (authToken) {
        headers['Authorization'] = `Bearer ${authToken}`;
    }

    return headers;
}
