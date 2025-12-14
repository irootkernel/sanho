import { useEffect, useState, useCallback } from 'react';
import { useRuntime } from '@/app/di/RuntimeContext';
import type { KkachiState } from '@/domain';
import { isUnimplementedError } from '@/domain';

/**
 * RawStatePage shows the raw JSON state from /api/state.
 * Displays prettified JSON for debugging purposes.
 */
export function RawStatePage() {
    const { getKkachiState } = useRuntime();
    const [state, setState] = useState<KkachiState | null>(null);
    const [error, setError] = useState<Error | null>(null);
    const [isLoading, setIsLoading] = useState(true);

    const fetchState = useCallback(async () => {
        setIsLoading(true);
        setError(null);
        try {
            const result = await getKkachiState.execute();
            setState(result);
        } catch (err) {
            setError(err instanceof Error ? err : new Error('Unknown error'));
        } finally {
            setIsLoading(false);
        }
    }, [getKkachiState]);

    useEffect(() => {
        fetchState();
    }, [fetchState]);

    if (isLoading) {
        return (
            <div className="page raw-state-page">
                <h2>Debug: Raw State</h2>
                <div className="loading-container">
                    <div className="loading-spinner"></div>
                    <p>Loading state...</p>
                </div>
            </div>
        );
    }

    if (error) {
        if (isUnimplementedError(error)) {
            return (
                <div className="error-container unimplemented">
                    <div className="error-icon">🚧</div>
                    <h2>Feature in Development</h2>
                    <p>
                        The feature <code>{error.featureName}</code> is not yet
                        implemented.
                    </p>
                    <p className="hint">
                        This feature will be available in a future update.
                    </p>
                </div>
            );
        }

        return (
            <div className="page raw-state-page">
                <h2>Debug: Raw State</h2>
                <div className="error-container error">
                    <div className="error-icon">⚠️</div>
                    <h3>Failed to Load State</h3>
                    <p>{error.message}</p>
                    <button onClick={fetchState} className="retry-button">
                        🔄 Retry
                    </button>
                </div>
            </div>
        );
    }

    return (
        <div className="page raw-state-page">
            <h2>Debug: Raw State</h2>
            <div className="state-info">
                <span className="badge">
                    {Object.keys(state?.docs_heads ?? {}).length} projects
                </span>
                <span className="badge">
                    {state?.workspaces.length ?? 0} workspaces
                </span>
                <button onClick={fetchState} className="refresh-button small">
                    🔄 Refresh
                </button>
            </div>
            <pre className="json-display">{JSON.stringify(state, null, 2)}</pre>
        </div>
    );
}
