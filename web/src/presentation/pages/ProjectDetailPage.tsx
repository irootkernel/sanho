import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { useRuntime } from '@/app/di/RuntimeContext';
import { isUnimplementedError } from '@/domain';

/**
 * ProjectDetailPage shows details for a specific project.
 * In CTASK-1, this triggers an UnimplementedError when loading data.
 * In CTASK-4, this will display workspace table with status.
 */
export function ProjectDetailPage() {
    const { projectName } = useParams<{ projectName: string }>();
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
        <div className="page project-detail-page">
            <h2>Project: {projectName}</h2>
            <p>Loading project details...</p>
        </div>
    );
}
