import { useEffect, useState, useCallback, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { useRuntime } from '@/app/di/RuntimeContext';
import type { KkachiState, ProjectSummary } from '@/domain';
import {
    isUnimplementedError,
    isEmptyState,
    computeProjectSummaries,
    sortByOutdatedFirst,
} from '@/domain';

/**
 * Formats relative time from ISO timestamp
 */
function formatRelativeTime(isoTimestamp: string | null): string {
    if (!isoTimestamp) return '—';

    const date = new Date(isoTimestamp);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMinutes = Math.floor(diffMs / (1000 * 60));
    const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

    if (diffMinutes < 1) return 'Just now';
    if (diffMinutes < 60) return `${diffMinutes}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    if (diffDays < 7) return `${diffDays}d ago`;

    return date.toLocaleDateString('en-US', {
        month: 'short',
        day: 'numeric',
        year: date.getFullYear() !== now.getFullYear() ? 'numeric' : undefined,
    });
}

/**
 * Truncates hash for display
 */
function formatHash(hash: string | null): string {
    if (!hash) return '—';
    return hash.length > 8 ? `${hash.substring(0, 8)}...` : hash;
}

type SortMode = 'name' | 'outdated';

/**
 * ProjectsPage is the main dashboard showing all projects.
 * Displays project summaries with their docs HEAD and workspace status.
 */
export function ProjectsPage() {
    const { getKkachiState } = useRuntime();
    const navigate = useNavigate();

    const [state, setState] = useState<KkachiState | null>(null);
    const [error, setError] = useState<Error | null>(null);
    const [isLoading, setIsLoading] = useState(true);
    const [sortMode, setSortMode] = useState<SortMode>('name');

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

    // Compute and sort project summaries
    const summaries = useMemo<ProjectSummary[]>(() => {
        if (!state) return [];
        const computed = computeProjectSummaries(state);
        return sortMode === 'outdated'
            ? sortByOutdatedFirst(computed)
            : computed;
    }, [state, sortMode]);

    // Check for projects with no workspaces
    const projectsWithNoWorkspaces = useMemo(
        () => summaries.filter((s) => s.workspace_count === 0 && s.docs_head),
        [summaries]
    );

    const handleRowClick = (projectName: string) => {
        navigate(`/projects/${encodeURIComponent(projectName)}`);
    };

    const toggleSort = () => {
        setSortMode((prev) => (prev === 'name' ? 'outdated' : 'name'));
    };

    // Loading state
    if (isLoading) {
        return (
            <div className="page projects-page">
                <h2>Projects Dashboard</h2>
                <div className="loading-container">
                    <div className="loading-spinner"></div>
                    <p>Loading projects...</p>
                </div>
            </div>
        );
    }

    // Unimplemented error
    if (error && isUnimplementedError(error)) {
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

    // Other errors
    if (error) {
        return (
            <div className="page projects-page">
                <h2>Projects Dashboard</h2>
                <div className="error-container error">
                    <div className="error-icon">⚠️</div>
                    <h3>Failed to Load Projects</h3>
                    <p>{error.message}</p>
                    <button onClick={fetchState} className="retry-button">
                        🔄 Retry
                    </button>
                </div>
            </div>
        );
    }

    // Empty state (no projects and no workspaces)
    if (state && isEmptyState(state)) {
        return (
            <div className="page projects-page">
                <h2>Projects Dashboard</h2>
                <div className="empty-state">
                    <div className="empty-icon">📁</div>
                    <h3>No Projects Yet</h3>
                    <p>
                        kkachi-server has no registered projects or workspaces.
                    </p>
                    <p className="hint">
                        Use <code>kkachi init</code> in a Git repository to
                        register your first workspace.
                    </p>
                </div>
            </div>
        );
    }

    return (
        <div className="page projects-page">
            <div className="page-header">
                <h2>Projects Dashboard</h2>
                <div className="page-actions">
                    <button
                        onClick={toggleSort}
                        className="sort-toggle-button"
                        aria-pressed={sortMode === 'outdated'}
                    >
                        {sortMode === 'outdated'
                            ? '🔥 Outdated First'
                            : '🔤 Sort by Name'}
                    </button>
                    <button onClick={fetchState} className="refresh-button small">
                        🔄 Refresh
                    </button>
                </div>
            </div>

            {/* Warning banner for projects with docs_head but no workspaces */}
            {projectsWithNoWorkspaces.length > 0 && (
                <div className="warning-banner">
                    <span className="warning-icon">ℹ️</span>
                    <span>
                        {projectsWithNoWorkspaces.length} project(s) have docs
                        HEAD but no registered workspaces.
                    </span>
                </div>
            )}

            <div className="table-container">
                <table className="projects-table">
                    <thead>
                        <tr>
                            <th>Project</th>
                            <th>Docs HEAD</th>
                            <th>Workspaces</th>
                            <th>Status</th>
                            <th>Last Updated</th>
                        </tr>
                    </thead>
                    <tbody>
                        {summaries.map((summary) => (
                            <tr
                                key={summary.project}
                                onClick={() => handleRowClick(summary.project)}
                                tabIndex={0}
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter' || e.key === ' ') {
                                        e.preventDefault();
                                        handleRowClick(summary.project);
                                    }
                                }}
                                className={
                                    summary.outdated_count > 0
                                        ? 'row-outdated'
                                        : ''
                                }
                            >
                                <td className="project-name">
                                    {summary.project}
                                </td>
                                <td className="docs-head">
                                    <code>{formatHash(summary.docs_head)}</code>
                                </td>
                                <td className="workspace-count">
                                    {summary.workspace_count}
                                </td>
                                <td className="status-cell">
                                    {summary.docs_head === null ? (
                                        <span className="status-badge unknown">
                                            Unknown
                                        </span>
                                    ) : summary.outdated_count > 0 ? (
                                        <span className="status-badge outdated">
                                            {summary.outdated_count} Outdated
                                        </span>
                                    ) : (
                                        <span className="status-badge up-to-date">
                                            ✓ Up-to-date
                                        </span>
                                    )}
                                </td>
                                <td className="last-updated">
                                    {formatRelativeTime(
                                        summary.last_reported_at_max
                                    )}
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </div>
    );
}
