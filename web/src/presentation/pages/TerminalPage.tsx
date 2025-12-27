import React, { useState } from 'react';
import { ConsoleList } from '../components/terminal/ConsoleList';
import { TerminalPane } from '../components/terminal/TerminalPane';
import { WorkspacePickerModal } from '../components/terminal/WorkspacePickerModal';
import { useTerminal } from '@/application';
import { createSession } from '@/api/pty';
import type { Workspace } from '@/api/state';

export const TerminalPage: React.FC = () => {
    const [isModalOpen, setIsModalOpen] = useState(false);
    const { addConsole, updateConsole } = useTerminal();

    const handleCreateSession = async (workspace: Workspace) => {
        setIsModalOpen(false);

        // 1. Create a client-side ID
        const consoleId = crypto.randomUUID();

        // 2. Add to list with CONNECTING status
        addConsole({
            consoleId,
            workspaceId: workspace.workspace_id,
            project: workspace.project,
            title: workspace.project, // Initial title
            status: 'CONNECTING',
            createdAt: Date.now(),
        });

        try {
            // 3. Call server API
            const response = await createSession({
                workspace_id: workspace.workspace_id,
                cwd_rel: "", // Explicitly send empty string as root
                title: workspace.project,
            });

            // 4. Update status and store sessionId
            updateConsole(consoleId, {
                status: 'CREATED',
                sessionId: response.session_id,
            });
        } catch (err) {
            // 5. Handle error
            const message = err instanceof Error ? err.message : 'Failed to create session';
            updateConsole(consoleId, {
                status: 'ERROR',
                errorMessage: message,
            });
        }
    };

    return (
        <div className="container-fluid py-4" style={{ height: 'calc(100vh - 100px)' }}>
            <div className="row h-100">
                <div className="col-md-3 h-100 border-end overflow-auto">
                    <ConsoleList onNew={() => setIsModalOpen(true)} />
                </div>
                <div className="col-md-9 h-100 d-flex flex-column">
                    <TerminalPane />
                </div>
            </div>

            <WorkspacePickerModal
                isOpen={isModalOpen}
                onClose={() => setIsModalOpen(false)}
                onSelect={handleCreateSession}
            />
        </div>
    );
};

export default TerminalPage;