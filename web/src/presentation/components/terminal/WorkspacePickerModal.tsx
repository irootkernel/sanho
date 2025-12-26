import React from 'react';

export const WorkspacePickerModal: React.FC = () => {
    return (
        <div className="modal fade" id="workspacePickerModal" tabIndex={-1}>
            <div className="modal-dialog modal-lg">
                <div className="modal-content">
                    <div className="modal-header">
                        <h5 className="modal-title">Open New Console</h5>
                        <button type="button" className="btn-close" data-bs-dismiss="modal"></button>
                    </div>
                    <div className="modal-body">
                        <p>Select a workspace to open a PTY session.</p>
                        {/* Workspace list will go here in CTASK-2 */}
                    </div>
                    <div className="modal-footer">
                        <button type="button" className="btn btn-secondary" data-bs-dismiss="modal">Cancel</button>
                    </div>
                </div>
            </div>
        </div>
    );
};
