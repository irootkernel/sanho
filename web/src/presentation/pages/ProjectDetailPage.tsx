import { useState, useMemo } from 'react';
import { useParams, Link } from 'react-router-dom';
import { useKkachiState } from '@/application';
import type {
    StatusFilter,
    SortOption,
    SortField,
    WorkspaceWithStatus,
} from '@/domain';
import { filterAndSortWorkspaces, formatHash, isEmptyState } from '@/domain';
import {
    Loading,
    ErrorBanner,
    EmptyState,
    WorkspaceTable,
} from '@/presentation/components';

export function ProjectDetailPage() {
    const { projectName } = useParams<{ projectName: string }>();
    const { data: state, isLoading, error, refresh } = useKkachiState();

    // Filters
    const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
    const [searchQuery, setSearchQuery] = useState('');
    const [sort, setSort] = useState<SortOption>({
        field: 'last_reported_at',
        direction: 'desc',
    });

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

    if (isLoading && !state) {
        return (
            <div className="page project-detail-page container-fluid">
                <nav aria-label="breadcrumb" className="mb-4">
                    <ol className="breadcrumb">
                        <li className="breadcrumb-item"><Link to="/">Projects</Link></li>
                        <li className="breadcrumb-item active" aria-current="page">{decodedProjectName}</li>
                    </ol>
                </nav>
                <Loading message="Loading project details..." />
            </div>
        );
    }

    if (error && !state) {
        return (
            <div className="page project-detail-page container-fluid">
                <nav aria-label="breadcrumb" className="mb-4">
                    <ol className="breadcrumb">
                        <li className="breadcrumb-item"><Link to="/">Projects</Link></li>
                        <li className="breadcrumb-item active" aria-current="page">{decodedProjectName}</li>
                    </ol>
                </nav>
                <ErrorBanner
                    error={error}
                    onRetry={refresh}
                    title="Failed to Load Project"
                />
            </div>
        );
    }

    if (state && isEmptyState(state)) {
        return (
            <div className="page project-detail-page container-fluid">
                <nav aria-label="breadcrumb" className="mb-4">
                    <ol className="breadcrumb">
                        <li className="breadcrumb-item"><Link to="/">Projects</Link></li>
                        <li className="breadcrumb-item active" aria-current="page">{decodedProjectName}</li>
                    </ol>
                </nav>
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

    if (!projectData) {
        return (
            <div className="page project-detail-page container-fluid">
                <nav aria-label="breadcrumb" className="mb-4">
                    <ol className="breadcrumb">
                        <li className="breadcrumb-item"><Link to="/">Projects</Link></li>
                        <li className="breadcrumb-item active" aria-current="page">{decodedProjectName}</li>
                    </ol>
                </nav>
                <div className="alert alert-danger" role="alert">
                    <h4 className="alert-heading">Project Not Found</h4>
                    <p>
                        The project "{decodedProjectName}" does not exist in the
                        state.
                    </p>
                    <hr />
                    <Link to="/" className="btn btn-outline-danger">
                        Go to Dashboard
                    </Link>
                </div>
            </div>
        );
    }

    const totalCount = projectData.workspaces.length;

    return (
        <div className="page project-detail-page container-fluid">
            <nav aria-label="breadcrumb" className="mb-4">
                <ol className="breadcrumb">
                    <li className="breadcrumb-item"><Link to="/">Projects</Link></li>
                    <li className="breadcrumb-item active" aria-current="page">{decodedProjectName}</li>
                </ol>
            </nav>

            <header className="card mb-4 shadow-sm border-0">
                <div className="card-body d-flex justify-content-between align-items-center">
                    <div>
                        <h2 className="card-title h3 mb-1">{decodedProjectName}</h2>
                        <div className="text-muted small">Project Overview</div>
                    </div>
                    <div className="d-flex gap-3 text-end">
                        <div className="d-flex flex-column">
                            <span className="text-muted small">Docs HEAD</span>
                            <code className="bg-light px-2 py-1 rounded">{formatHash(projectData.docsHead)}</code>
                        </div>
                        <div className="vr"></div>
                        <div className="d-flex flex-column">
                            <span className="text-muted small">Workspaces</span>
                            <strong className="fs-5">{totalCount}</strong>
                        </div>
                    </div>
                </div>
            </header>

            {projectData.docsHead === null && (
                <div className="alert alert-warning d-flex align-items-center mb-4" role="alert">
                    <i className="bi bi-info-circle-fill me-2"></i>
                    <div>
                        This project does not have a registered Docs HEAD yet. All workspaces are Unknown.
                    </div>
                </div>
            )}

            <div className="card shadow-sm border-0">
                <div className="card-header bg-white py-3">
                    <div className="row g-3 align-items-center">
                        <div className="col-auto">
                            <select
                                className="form-select form-select-sm"
                                value={statusFilter}
                                onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
                            >
                                <option value="all">All Status</option>
                                <option value="up_to_date">Up-to-date</option>
                                <option value="outdated">Outdated</option>
                                <option value="unknown">Unknown</option>
                            </select>
                        </div>
                        <div className="col">
                            <input
                                type="text"
                                className="form-control form-control-sm"
                                placeholder="Search workspace..."
                                value={searchQuery}
                                onChange={(e) => setSearchQuery(e.target.value)}
                            />
                        </div>
                        <div className="col-auto d-flex gap-2">
                             <span className="col-form-label col-form-label-sm text-muted me-1">Sort by:</span>
                             <div className="btn-group btn-group-sm" role="group">
                                <button
                                    type="button"
                                    className={`btn btn-outline-secondary ${sort.field === 'last_reported_at' ? 'active' : ''}`}
                                    onClick={() => handleSortChange('last_reported_at')}
                                >
                                    Last Reported {sort.field === 'last_reported_at' && (sort.direction === 'asc' ? '↑' : '↓')}
                                </button>
                                <button
                                    type="button"
                                    className={`btn btn-outline-secondary ${sort.field === 'local_path' ? 'active' : ''}`}
                                    onClick={() => handleSortChange('local_path')}
                                >
                                    Path {sort.field === 'local_path' && (sort.direction === 'asc' ? '↑' : '↓')}
                                </button>
                             </div>
                        </div>
                    </div>
                </div>
                
                <div className="card-body p-0">
                    {projectData.workspaces.length === 0 ? (
                        <div className="p-5">
                             <EmptyState
                                icon="📭"
                                title="No Workspaces"
                                description="This project has no registered workspaces yet."
                                hint={
                                    <>
                                        Use <code>kkachi init</code> to register a workspace.
                                    </>
                                }
                            />
                        </div>
                    ) : filteredWorkspaces.length === 0 ? (
                        <div className="text-center py-5">
                            <p className="text-muted mb-3">No workspaces match your filter.</p>
                            <button
                                className="btn btn-outline-primary btn-sm"
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
        </div>
    );
}