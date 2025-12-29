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
            <div className="page raw-state-page container-fluid">
                <h2 className="mb-4">Debug: Raw State</h2>
                <Loading message="Loading state..." />
            </div>
        );
    }

    if (error && !data) {
        return (
            <div className="page raw-state-page container-fluid">
                <h2 className="mb-4">Debug: Raw State</h2>
                <ErrorBanner
                    error={error}
                    onRetry={refresh}
                    title="Failed to Load State"
                />
            </div>
        );
    }

    const projectCount = Object.keys(data?.docs_heads ?? {}).length;
    const workspaceCount = data?.workspaces.length ?? 0;

    return (
        <div className="page raw-state-page container-fluid">
            <header className="d-flex justify-content-between align-items-center mb-4">
                <h2 className="mb-0">Debug: Raw State</h2>
                <button 
                    className="btn btn-outline-primary btn-sm d-flex align-items-center gap-2"
                    onClick={() => refresh()}
                >
                    🔄 Refresh
                </button>
            </header>
            
            <div className="card shadow-sm border-0 mb-4">
                <div className="card-body d-flex align-items-center gap-3">
                    <span className="badge bg-primary rounded-pill fs-6 px-3">
                        {projectCount} projects
                    </span>
                    <span className="badge bg-success rounded-pill fs-6 px-3">
                        {workspaceCount} workspaces
                    </span>
                </div>
            </div>

            <div className="card shadow-sm border-0">
                <div className="card-header bg-dark text-white d-flex justify-content-between align-items-center">
                    <span className="font-monospace small">state.json</span>
                    <span className="badge bg-secondary">JSON</span>
                </div>
                <div className="card-body p-0 bg-light">
                    <pre className="m-0 p-3 json-display font-monospace" style={{ fontSize: '0.9rem', overflow: 'auto', maxHeight: '70vh' }}>
                        {JSON.stringify(data, null, 2)}
                    </pre>
                </div>
            </div>
        </div>
    );
}