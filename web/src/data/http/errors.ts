/**
 * Base error for all API-related errors.
 */
export class ApiError extends Error {
    readonly statusCode?: number;

    constructor(message: string, statusCode?: number) {
        super(message);
        this.name = 'ApiError';
        this.statusCode = statusCode;
    }
}

/**
 * Error thrown when a network request fails (no connection, timeout, etc.).
 */
export class NetworkError extends Error {
    readonly cause?: Error;

    constructor(message: string, cause?: Error) {
        super(message);
        this.name = 'NetworkError';
        this.cause = cause;
    }
}

/**
 * Type guard for ApiError.
 */
export function isApiError(error: unknown): error is ApiError {
    return error instanceof ApiError;
}

/**
 * Type guard for NetworkError.
 */
export function isNetworkError(error: unknown): error is NetworkError {
    return error instanceof NetworkError;
}
