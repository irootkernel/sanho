import React, { useEffect, useState, useMemo } from 'react';
import { fetchWorkspaces, type Workspace } from '@/api/state';

interface WorkspacePickerModalProps {
    isOpen: boolean;
    onClose: () => void;
    onSelect: (workspace: Workspace) => void;
}

export const WorkspacePickerModal: React.FC<WorkspacePickerModalProps> = ({
    isOpen,
    onClose,
    onSelect,
}) => {
    const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
    const [isLoading, setIsLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [searchQuery, setSearchQuery] = useState('');

    // Fetch workspaces when modal opens
    useEffect(() => {
        if (isOpen) {
            loadWorkspaces();
            setSearchQuery(''); // Reset search
        }
    }, [isOpen]);

    const loadWorkspaces = async () => {
        setIsLoading(true);
        setError(null);
        try {
            const data = await fetchWorkspaces();
            setWorkspaces(data);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to load workspaces');
        } finally {
            setIsLoading(false);
        }
    };

    // Filter workspaces based on search query
    const filteredWorkspaces = useMemo(() => {
        if (!searchQuery.trim()) return workspaces;
        const query = searchQuery.toLowerCase();
        return workspaces.filter(
            (ws) =>
                ws.project.toLowerCase().includes(query) ||
                ws.workspace_id.toLowerCase().includes(query) ||
                ws.local_path.toLowerCase().includes(query)
        );
    }, [workspaces, searchQuery]);

    if (!isOpen) return null;

    return (
        <>
            <div className="modal fade show" style={{ display: 'block' }} tabIndex={-1} role="dialog">
                <div className="modal-dialog modal-lg modal-dialog-scrollable">
                    <div className="modal-content">
                        <div className="modal-header">
                            <h5 className="modal-title">Open New Console</h5>
                            <button
                                type="button"
                                className="btn-close"
                                onClick={onClose}
                                aria-label="Close"
                            ></button>
                        </div>
                        <div className="modal-body">
                            <div className="mb-3">
                                <input
                                    type="text"
                                    className="form-control"
                                    placeholder="Search workspaces (project, ID, path)..."
                                    value={searchQuery}
                                    onChange={(e) => setSearchQuery(e.target.value)}
                                    autoFocus
                                />
                            </div>

                            {isLoading && (
                                <div className="text-center p-4">
                                    <div className="spinner-border text-primary" role="status">
                                        <span className="visually-hidden">Loading...</span>
                                    </div>
                                </div>
                            )}

                            {error && (
                                <div className="alert alert-danger" role="alert">
                                    {error}
                                    <button
                                        className="btn btn-sm btn-outline-danger ms-3"
                                        onClick={loadWorkspaces}
                                    >
                                        Retry
                                    </button>
                                </div>
                            )}

                            {!isLoading && !error && (
                                <div className="list-group">
                                    {filteredWorkspaces.length === 0 ? (
                                        <div className="text-center p-3 text-muted">
                                            No workspaces found.
                                        </div>
                                    ) : (
                                        filteredWorkspaces.map((ws) => (
                                            <button
                                                key={ws.workspace_id}
                                                type="button"
                                                className="list-group-item list-group-item-action"
                                                onClick={() => onSelect(ws)}
                                            >
                                                <div className="d-flex w-100 justify-content-between">
                                                    <h6 className="mb-1">{ws.project}</h6>
                                                    <small className="text-muted">{ws.workspace_id}</small>
                                                </div>
                                                <p className="mb-1 text-truncate" title={ws.local_path}>
                                                    <small className="text-muted font-monospace">
                                                        {ws.local_path}
                                                    </small>
                                                </p>
                                            </button>
                                        ))
                                    )}
                                </div>
                            )}
                        </div>
                        <div className="modal-footer">
                            <button
                                type="button"
                                className="btn btn-secondary"
                                onClick={onClose}
                            >
                                Cancel
                            </button>
                        </div>
                    </div>
                </div>
            </div>
            <div className="modal-backdrop fade show"></div>
        </>
    );
};