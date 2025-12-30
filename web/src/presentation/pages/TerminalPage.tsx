import React, { useState, useEffect, useRef } from 'react';
import { ConsoleList } from '../components/terminal/ConsoleList';
import { TerminalPane } from '../components/terminal/TerminalPane';
import { WorkspacePickerModal } from '../components/terminal/WorkspacePickerModal';
import { RenameSessionModal } from '../components/terminal/RenameSessionModal';
import { Toast } from '../components/common/Toast';
import { useTerminal } from '@/application';
import { useToast } from '../hooks/useToast';
import { MAX_CONSOLES } from '@/application/stores/useTerminalStore';
import { createSession } from '@/api/pty';
import type { Workspace } from '@/api/state';
import type { ConsoleRecord } from '@/domain/terminal/types';

export const TerminalPage: React.FC = () => {
    const [isModalOpen, setIsModalOpen] = useState(false);
    const [renamingConsole, setRenamingConsole] = useState<ConsoleRecord | null>(null);
    const { consoles, selectedConsoleId, addConsole, updateConsole } = useTerminal();
    const { toasts, showToast, removeToast } = useToast();
    
    const consolesRef = useRef(consoles);
    useEffect(() => {
        consolesRef.current = consoles;
    }, [consoles]);

    useEffect(() => {
        return () => {
            consolesRef.current.forEach(c => {
                if (c.ws && c.ws.readyState === WebSocket.OPEN) {
                    c.ws.close();
                }
            });
        };
    }, []);

    const handleCreateSession = async (workspace: Workspace) => {
        if (consoles.length >= MAX_CONSOLES) {
            showToast('Maximum number of consoles reached', 'danger');
            return;
        }
        setIsModalOpen(false);
        const consoleId = crypto.randomUUID();

        // Robust title extraction: 
        // 1. If ID contains ':', take the last part (usually path or custom name)
        // 2. If that part looks like a path, take the last segment
        // 3. Fallback to project name if extraction fails
        const rawId = workspace.workspace_id.includes(':') 
            ? workspace.workspace_id.split(':').pop() || "" 
            : workspace.workspace_id;
        const extractedTitle = rawId.split('/').filter(Boolean).pop() || workspace.project;

        addConsole({
            consoleId,
            workspaceId: workspace.workspace_id,
            project: workspace.project,
            title: extractedTitle,
            status: 'CONNECTING',
            createdAt: Date.now(),
        });

        try {
            const response = await createSession({
                workspace_id: workspace.workspace_id,
                cwd_rel: "",
                title: workspace.project,
            });

            updateConsole(consoleId, {
                status: 'CREATED',
                sessionId: response.session_id,
                wsUrl: response.ws_url,
            });
            showToast(`Session created: ${extractedTitle}`, 'success');
        } catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to create session';
            updateConsole(consoleId, {
                status: 'ERROR',
                errorMessage: message,
            });
            showToast(`Failed to create session: ${message}`, 'danger');
        }
    };

    const handleRename = (newTitle: string) => {
        if (renamingConsole) {
            updateConsole(renamingConsole.consoleId, { title: newTitle });
            showToast(`Session renamed to: ${newTitle}`, 'success');
        }
    };

    return (
        <div style={{ 
            height: '100%',
            display: 'flex', 
            backgroundColor: '#fff',
            overflow: 'hidden'
        }}>
            {/* Sidebar */}
            <ConsoleList 
                onNew={() => setIsModalOpen(true)} 
                isNewDisabled={consoles.length >= MAX_CONSOLES}
                onRenameRequest={setRenamingConsole}
            />
            
            {/* Main Content Area */}
            <div style={{ flex: 1, display: 'flex', flexDirection: 'column', position: 'relative', overflow: 'hidden' }}>
                {consoles.length === 0 ? (
                    <div 
                        data-testid="no-active-consoles"
                        style={{ 
                            flex: 1, display: 'flex', flexDirection: 'column', 
                            alignItems: 'center', justifyContent: 'center', color: '#adb5bd',
                            backgroundColor: '#f8f9fa'
                        }}
                    >
                        <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" strokeLinecap="round" strokeLinejoin="round" style={{ marginBottom: '24px', opacity: 0.2 }}><rect x="2" y="3" width="20" height="14" rx="2" ry="2"></rect><line x1="8" y1="21" x2="16" y2="21"></line><line x1="12" y1="17" x2="12" y2="21"></line></svg>
                        <h3 style={{ margin: 0, fontWeight: '600', color: '#6c757d' }}>No active consoles</h3>
                        <p style={{ marginTop: '8px', fontSize: '0.95rem' }}>Select a workspace from the 'New' menu to get started.</p>
                    </div>
                ) : (
                    <div style={{ flex: 1, position: 'relative' }}>
                        {consoles.map((c) => (
                            <div 
                                key={c.consoleId} 
                                style={{ 
                                    position: 'absolute', top: 0, left: 0, width: '100%', height: '100%',
                                    display: c.consoleId === selectedConsoleId ? 'flex' : 'none',
                                    flexDirection: 'column'
                                }}
                            >
                                <TerminalPane 
                                    console={c} 
                                    isSelected={c.consoleId === selectedConsoleId} 
                                />
                            </div>
                        ))}
                    </div>
                )}
            </div>

            <WorkspacePickerModal
                isOpen={isModalOpen}
                onClose={() => setIsModalOpen(false)}
                onSelect={handleCreateSession}
            />

            <RenameSessionModal
                key={renamingConsole?.consoleId || 'none'}
                isOpen={!!renamingConsole}
                initialTitle={renamingConsole?.title || ""}
                onClose={() => setRenamingConsole(null)}
                onRename={handleRename}
            />

            {/* Toasts */}
            {toasts.map((toast, index) => (
                <Toast 
                    key={toast.id}
                    message={toast.message}
                    type={toast.type}
                    index={index}
                    onClose={() => removeToast(toast.id)}
                />
            ))}
        </div>
    );
};

export default TerminalPage;