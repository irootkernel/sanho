import { useKkachiState } from '@/application';
import { Loading, ErrorBanner } from '@/presentation/components';

/**
 * RawStatePage shows the raw JSON state from /api/state.
 * Displays prettified JSON for debugging purposes.
 */
export function RawStatePage() {
    const { data, isLoading, error, refresh } = useKkachiState();

    if (isLoading && !data) {
        return (
            <div className="page raw-state-page">
                <h2>Debug: Raw State</h2>
                <Loading message="Loading state..." />
            </div>
        );
    }

    if (error && !data) {
        return (
            <div className="page raw-state-page">
                <h2>Debug: Raw State</h2>
                <ErrorBanner
                    error={error}
                    onRetry={refresh}
                    title="Failed to Load State"
                />
            </div>
        );
    }

    return (
        <div className="page raw-state-page">
            <h2>Debug: Raw State</h2>
            <div className="state-info">
                <span className="badge">
                    {Object.keys(data?.docs_heads ?? {}).length} projects
                </span>
                <span className="badge">
                    {data?.workspaces.length ?? 0} workspaces
                </span>
                <button onClick={refresh} className="refresh-button small">
                    🔄 Refresh
                </button>
            </div>
            <pre className="json-display">{JSON.stringify(data, null, 2)}</pre>
        </div>
    );
}
