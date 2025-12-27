import React, { useState } from 'react';
import { useTerminal } from '@/application';
import { terminateSession } from '@/api/pty';

export const TerminalPane: React.FC = () => {
    const { consoles, selectedConsoleId, removeConsole } = useTerminal();
    const [isTerminating, setIsTerminating] = useState(false);
    
    const activeConsole = consoles.find((c) => c.consoleId === selectedConsoleId);

    const handleClose = async () => {
        if (!activeConsole) return;

        // If it's already in error state or has no sessionId, just remove from UI
        if (activeConsole.status === 'ERROR' || !activeConsole.sessionId) {
            removeConsole(activeConsole.consoleId);
            return;
        }

        setIsTerminating(true);
        try {
            await terminateSession(activeConsole.sessionId);
        } catch {
            // Silently fail and remove from UI as per spec
        } finally {
            setIsTerminating(false);
            removeConsole(activeConsole.consoleId);
        }
    };

    if (!activeConsole) {
        return (
            <div className="terminal-pane flex-grow-1 d-flex flex-column bg-dark text-white rounded overflow-hidden shadow">
                <div className="terminal-container flex-grow-1 p-3 d-flex align-items-center justify-content-center">
                    <div className="text-center">
                        <i className="bi bi-terminal display-4 mb-3 text-secondary"></i>
                        <p className="text-secondary">Select a console from the list to start</p>
                    </div>
                </div>
            </div>
        );
    }

    return (
        <div className="terminal-pane flex-grow-1 d-flex flex-column bg-dark text-white rounded overflow-hidden shadow">
            <div className="terminal-toolbar d-flex align-items-center justify-content-between px-3 py-2 border-bottom border-secondary">
                <div className="terminal-title small font-monospace text-truncate">
                    {activeConsole.title} ({activeConsole.status})
                </div>
                <div className="terminal-actions">
                    <button 
                        className="btn btn-sm btn-outline-light border-0"
                        onClick={handleClose}
                        disabled={isTerminating}
                    >
                        {isTerminating ? (
                            <span className="spinner-border spinner-border-sm" role="status"></span>
                        ) : (
                            <i className="bi bi-x-lg"></i>
                        )}
                    </button>
                </div>
            </div>
            <div className="terminal-container flex-grow-1 p-0 bg-black">
                {activeConsole.status === 'ERROR' ? (
                    <div className="h-100 d-flex align-items-center justify-content-center p-4">
                        <div className="alert alert-danger mb-0">
                            <h6 className="alert-heading">Session Error</h6>
                            <p className="mb-0 small">{activeConsole.errorMessage}</p>
                        </div>
                    </div>
                ) : (
                    <div className="h-100 d-flex align-items-center justify-content-center">
                        <div className="text-center p-4">
                            <div className="spinner-border spinner-border-sm text-primary mb-3" role="status"></div>
                            <p className="text-muted small">Terminal streaming will be implemented in CTASK-3</p>
                            <p className="text-info small">{activeConsole.workspaceId}</p>
                        </div>
                    </div>
                )}
            </div>
        </div>
    );
};