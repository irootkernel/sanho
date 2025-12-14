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
 * Get the API configuration from environment variables.
 *
 * @returns ApiConfig with VITE_KKACHI_API_PREFIX or default "/api"
 */
export function getApiConfig(): ApiConfig {
    return {
        apiPrefix: import.meta.env.VITE_KKACHI_API_PREFIX || '/api',
    };
}

/**
 * Build the full URL for an API endpoint.
 * Automatically ensures the endpoint starts with a slash.
 * @param endpoint - API endpoint (e.g., "/state" or "state")
 * @returns Full URL path (e.g., "/api/state")
 */
export function buildApiUrl(endpoint: string): string {
    const { apiPrefix } = getApiConfig();
    // Ensure endpoint starts with a slash
    const normalizedEndpoint = endpoint.startsWith('/') ? endpoint : `/${endpoint}`;
    return `${apiPrefix}${normalizedEndpoint}`;
}
