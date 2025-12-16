import { useEffect, useState, useMemo, useCallback } from 'react';
import { useParams, Link } from 'react-router-dom';
import { useRuntime } from '@/app/di/RuntimeContext';
import type {
    KkachiState,
    StatusFilter,
    SortOption,
    SortField,
    WorkspaceWithStatus,
} from '@/domain';
import {
    isUnimplementedError,
    filterAndSortWorkspaces,
} from '@/domain';
import { WorkspaceTable } from '@/presentation/components';

/**
 * Truncates hash for display
 */
function formatHash(hash: string | null): string {
    if (!hash) return '—';
    return hash.length > 8 ? `${hash.substring(0, 8)}...` : hash;
}

export function ProjectDetailPage() {
    const { projectName } = useParams<{ projectName: string }>();
    const { getKkachiState } = useRuntime();

    const [state, setState] = useState<KkachiState | null>(null);
    const [error, setError] = useState<Error | null>(null);
    const [isLoading, setIsLoading] = useState(true);

    // Filters
    const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
    const [searchQuery, setSearchQuery] = useState('');
    const [sort, setSort] = useState<SortOption>({
        field: 'last_reported_at',
        direction: 'desc',
    });

    const fetchState = useCallback(async () => {
        setIsLoading(true);
        setError(null);
        try {
            const result = await getKkachiState.execute();
            setState(result);
        } catch (err) {
            console.error('ProjectDetailPage Fetch Error:', err);
            setError(err instanceof Error ? err : new Error('Unknown error'));
        } finally {
            setIsLoading(false);
        }
    }, [getKkachiState]);

    useEffect(() => {
        fetchState();
    }, [fetchState]);

    const decodedProjectName = decodeURIComponent(projectName ?? '');

    const projectData = useMemo(() => {
        if (!state) return null;
        const docsHead = state.docs_heads[decodedProjectName] ?? null;
        const workspaces = state.workspaces.filter(
            (ws) => ws.project === decodedProjectName
        );

        // Check if project exists (has head OR has workspaces)
        if (docsHead === null && workspaces.length === 0) {
            return null;
        }

        return { docsHead, workspaces };
    }, [state, decodedProjectName]);


    const filteredWorkspaces = useMemo<WorkspaceWithStatus[]>(() => {
        if (!projectData) return [];
        return filterAndSortWorkspaces(
            projectData.workspaces,
            projectData.docsHead,
            statusFilter,
            searchQuery,
            sort
        );
    }, [projectData, statusFilter, searchQuery, sort]);

    const handleSortChange = (field: SortField) => {
        setSort((prev) => ({
            field,
            direction:
                prev.field === field && prev.direction === 'desc'
                    ? 'asc'
                    : 'desc',
        }));
    };

    if (isLoading) {
        return (
            <div className="page project-detail-page">
                <div className="breadcrumbs">
                    <Link to="/">Projects</Link> &gt; <span>{decodedProjectName}</span>
                </div>
                <h2>Project: {decodedProjectName}</h2>
                <div className="loading-container">
                    <div className="loading-spinner"></div>
                    <p>Loading project details...</p>
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
                    <p>The feature is not yet implemented.</p>
                </div>
            );
        }
        return (
            <div className="error-container error">
                <div className="error-icon">⚠️</div>
                <h2>An Error Occurred</h2>
                <p>{error.message}</p>
                <button onClick={fetchState} className="retry-button">
                    Retry
                </button>
            </div>
        );
    }

    if (!projectData) {
        return (
            <div className="page project-detail-page">
                <div className="breadcrumbs">
                    <Link to="/">Projects</Link> &gt; <span>{decodedProjectName}</span>
                </div>
                <div className="error-container error">
                    <h3>Project Not Found</h3>
                    <p>The project "{decodedProjectName}" does not exist in the state.</p>
                    <Link to="/" className="retry-button">Go to Dashboard</Link>
                </div>
            </div>
        );
    }

    // Determine status counts for badges
    const totalCount = projectData.workspaces.length;
    // We can use simplified status counts from filtered list or recalculate
    // Let's rely on filter options

    return (
        <div className="page project-detail-page">
            <div className="breadcrumbs">
                <Link to="/">Projects</Link> &gt; <span>{decodedProjectName}</span>
            </div>

            <header className="project-header">
                <div>
                    <h2>{decodedProjectName}</h2>
                    <div className="project-meta">
                        <span className="meta-item">
                            <span className="label">Docs HEAD:</span>{' '}
                            <code>{formatHash(projectData.docsHead)}</code>
                        </span>
                        <span className="meta-item">
                            <span className="label">Workspaces:</span>{' '}
                            <strong>{totalCount}</strong>
                        </span>
                    </div>
                </div>
            </header>

            {projectData.docsHead === null && (
                <div className="warning-banner">
                    <span className="warning-icon">ℹ️</span>
                    <span>
                        This project does not have a registered Docs HEAD yet. All workspaces are Unknown.
                    </span>
                </div>
            )}

            <div className="controls-bar">
                <div className="filters">
                    <select
                        value={statusFilter}
                        onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
                        className="status-filter"
                    >
                        <option value="all">All Status</option>
                        <option value="up_to_date">Up-to-date</option>
                        <option value="outdated">Outdated</option>
                        <option value="unknown">Unknown</option>
                    </select>

                    <input
                        type="text"
                        placeholder="Search workspace..."
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                        className="search-input"
                    />
                </div>

                <div className="sort-controls">
                    <span className="label">Sort by:</span>
                    <button
                        className={`sort-btn ${sort.field === 'last_reported_at' ? 'active' : ''}`}
                        onClick={() => handleSortChange('last_reported_at')}
                    >
                        Last Reported {sort.field === 'last_reported_at' && (sort.direction === 'asc' ? '↑' : '↓')}
                    </button>
                    <button
                        className={`sort-btn ${sort.field === 'local_path' ? 'active' : ''}`}
                        onClick={() => handleSortChange('local_path')}
                    >
                        Path {sort.field === 'local_path' && (sort.direction === 'asc' ? '↑' : '↓')}
                    </button>
                </div>
            </div>

            <div className="table-container">
                {projectData.workspaces.length === 0 ? (
                    <div className="empty-state">
                        <div className="empty-icon">📭</div>
                        <h3>No Workspaces</h3>
                        <p>This project has no registered workspaces yet.</p>
                        <p className="hint">Use <code>kkachi init</code> to register a workspace.</p>
                    </div>
                ) : filteredWorkspaces.length === 0 ? (
                    <div className="empty-filter-state">
                        <p>No workspaces match your filter.</p>
                        <button
                            className="text-button"
                            onClick={() => {
                                setStatusFilter('all');
                                setSearchQuery('');
                            }}
                        >
                            Reset Filters
                        </button>
                    </div>
                ) : (
                    <WorkspaceTable workspaces={filteredWorkspaces} />
                )}
            </div>
        </div>
    );
}
