import { useState, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { useKkachiState } from '@/application';
import type { ProjectSummary } from '@/domain';
import {
    isEmptyState,
    computeProjectSummaries,
    sortByOutdatedFirst,
    formatRelativeTime,
    formatAbsoluteTime,
    formatHash,
} from '@/domain';
import { Loading, ErrorBanner, EmptyState } from '@/presentation/components';

type SortMode = 'name' | 'outdated';

/**
 * ProjectsPage is the main dashboard showing all projects.
 * Displays project summaries with their docs HEAD and workspace status.
 */
export function ProjectsPage() {
    const { data: state, isLoading, error, refresh } = useKkachiState();
    const navigate = useNavigate();
    const [sortMode, setSortMode] = useState<SortMode>('name');

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
    if (isLoading && !state) {
        return (
            <div className="page projects-page">
                <h2>Projects Dashboard</h2>
                <Loading message="Loading projects..." />
            </div>
        );
    }

    // Error state
    if (error && !state) {
        return (
            <div className="page projects-page">
                <h2>Projects Dashboard</h2>
                <ErrorBanner
                    error={error}
                    onRetry={refresh}
                    title="Failed to Load Projects"
                />
            </div>
        );
    }

    // Empty state (no projects and no workspaces)
    if (state && isEmptyState(state)) {
        return (
            <div className="page projects-page">
                <h2>Projects Dashboard</h2>
                <EmptyState
                    icon="📁"
                    title="No Projects Yet"
                    description="kkachi-server has no registered projects or workspaces."
                    hint={
                        <>
                            Use <code>kkachi init</code> in a Git repository to
                            register your first workspace.
                        </>
                    }
                />
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
                    <button onClick={refresh} className="refresh-button small">
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
                                    <span
                                        title={formatAbsoluteTime(
                                            summary.last_reported_at_max
                                        )}
                                    >
                                        {formatRelativeTime(
                                            summary.last_reported_at_max
                                        )}
                                    </span>
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </div>
    );
}
