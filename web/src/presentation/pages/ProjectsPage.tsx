import { useEffect, useState } from 'react';
import { useRuntime } from '@/app/di/RuntimeContext';
import { isUnimplementedError } from '@/domain';

/**
 * ProjectsPage is the main dashboard showing all projects.
 * In CTASK-1, this triggers an UnimplementedError when loading data.
 * In CTASK-3, this will display actual project summaries.
 */
export function ProjectsPage() {
    const { getKkachiState } = useRuntime();
    const [error, setError] = useState<Error | null>(null);

    useEffect(() => {
        // Trigger state loading - will throw UnimplementedError in CTASK-1
        getKkachiState.execute().catch((err) => {
            setError(err);
        });
    }, [getKkachiState]);

    if (error) {
        if (isUnimplementedError(error)) {
            return (
                <div className="error-container unimplemented">
                    <div className="error-icon">🚧</div>
                    <h2>Feature in Development</h2>
                    <p>
                        The feature <code>{error.featureName}</code> is not yet implemented.
                    </p>
                    <p className="hint">This feature will be available in a future update.</p>
                </div>
            );
        }

        return (
            <div className="error-container error">
                <div className="error-icon">⚠️</div>
                <h2>An Error Occurred</h2>
                <p>{error.message}</p>
                <button onClick={() => setError(null)} className="retry-button">
                    Retry
                </button>
            </div>
        );
    }

    return (
        <div className="page projects-page">
            <h2>Projects Dashboard</h2>
            <p>Loading projects...</p>
        </div>
    );
}
