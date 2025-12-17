import { isUnimplementedError, type UnimplementedError } from '@/domain';

interface ErrorBannerProps {
    /** The error to display */
    error: Error;
    /** Optional retry callback */
    onRetry?: () => void;
    /** Custom title (defaults based on error type) */
    title?: string;
}

/**
 * ErrorBanner component for displaying errors with optional retry.
 * Automatically handles UnimplementedError with a special "in development" UI.
 */
export function ErrorBanner({ error, onRetry, title }: ErrorBannerProps) {
    // Special handling for UnimplementedError
    if (isUnimplementedError(error)) {
        const unimplError = error as UnimplementedError;
        return (
            <div className="error-container unimplemented">
                <div className="error-icon">🚧</div>
                <h2>Feature in Development</h2>
                <p>
                    The feature <code>{unimplError.featureName}</code> is not
                    yet implemented.
                </p>
                <p className="hint">
                    This feature will be available in a future update.
                </p>
            </div>
        );
    }

    // General error UI
    return (
        <div className="error-container error">
            <div className="error-icon">⚠️</div>
            <h3>{title ?? 'An Error Occurred'}</h3>
            <p>{error.message}</p>
            {onRetry && (
                <button onClick={onRetry} className="retry-button">
                    🔄 Retry
                </button>
            )}
        </div>
    );
}
