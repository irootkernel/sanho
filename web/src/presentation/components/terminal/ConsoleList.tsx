import React from 'react';
import { useTerminal } from '@/application';
import type { ConsoleStatus } from '@/domain/terminal/types';

interface ConsoleListProps {
    onNew?: () => void;
}

export const ConsoleList: React.FC<ConsoleListProps> = ({ onNew }) => {
    const { consoles, selectedConsoleId, selectConsole } = useTerminal();

    const getBadgeClass = (status: ConsoleStatus) => {
        switch (status) {
            case 'CONNECTED':
                return 'bg-success';
            case 'ERROR':
                return 'bg-danger';
            case 'CLOSED':
                return 'bg-dark';
            default:
                return 'bg-secondary';
        }
    };

    return (
        <div className="console-list">
            <div className="d-flex justify-content-between align-items-center mb-3">
                <h5 className="mb-0">Consoles</h5>
                <button className="btn btn-sm btn-primary" onClick={onNew}>
                    <i className="bi bi-plus-lg me-1"></i> New
                </button>
            </div>
            <div className="list-group list-group-flush">
                {consoles.length === 0 ? (
                    <div className="text-muted small p-3 text-center">
                        No active consoles. Click 'New' to open one.
                    </div>
                ) : (
                    consoles.map((console) => (
                        <button
                            key={console.consoleId}
                            className={`list-group-item list-group-item-action border-0 rounded mb-1 ${
                                selectedConsoleId === console.consoleId ? 'active' : ''
                            }`}
                            onClick={() => selectConsole(console.consoleId)}
                        >
                            <div className="d-flex justify-content-between align-items-center">
                                <span className="text-truncate me-2">{console.title}</span>
                                <span
                                    className={`badge rounded-pill ${getBadgeClass(console.status)}`}
                                    style={{ fontSize: '0.65rem' }}
                                >
                                    {console.status}
                                </span>
                            </div>
                        </button>
                    ))
                )}
            </div>
        </div>
    );
};
