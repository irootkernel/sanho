import React from 'react';
import type { Workspace } from '@/api/state';

interface WorkspaceCardProps {
    workspace: Workspace;
    isActive: boolean;
    isFocused: boolean;
    onSelect: (workspace: Workspace) => void;
}

export const WorkspaceCard: React.FC<WorkspaceCardProps> = ({
    workspace,
    isActive,
    isFocused,
    onSelect,
}) => {
    // 1. Get the clean name: from "project:path/to/name" to "name"
    const rawId = workspace.workspace_id.includes(':') 
        ? workspace.workspace_id.split(':').slice(1).join(':') 
        : workspace.workspace_id;
    
    // 2. Further extract only the last part of the path if it's a path
    const displayTitle = rawId.split('/').filter(Boolean).pop() || rawId;

    return (
        <div 
            className="workspace-card"
            onClick={() => onSelect(workspace)}
            style={{ 
                cursor: 'pointer',
                padding: '20px',
                borderRadius: '8px',
                backgroundColor: isFocused ? '#f0f7ff' : '#ffffff',
                border: `2px solid ${isFocused ? '#007fd4' : isActive ? '#28a745' : '#dee2e6'}`,
                transition: 'all 0.15s ease',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                textAlign: 'center',
                height: '100%',
                boxShadow: isFocused ? '0 8px 16px rgba(0,0,0,0.1)' : '0 2px 4px rgba(0,0,0,0.05)',
            }}
            title={workspace.local_path} // Show full path only on hover tooltip
        >
            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '8px' }}>
                <span style={{ 
                    color: isFocused ? '#007fd4' : '#212529', 
                    fontSize: '18px', 
                    fontWeight: 'bold',
                    wordBreak: 'break-all'
                }}>
                    {displayTitle}
                </span>
                {isActive && (
                    <span style={{ 
                        fontSize: '10px',
                        color: '#28a745',
                        backgroundColor: '#e6f4ea',
                        padding: '2px 8px',
                        borderRadius: '12px',
                        fontWeight: 'bold',
                        letterSpacing: '0.5px'
                    }}>ACTIVE</span>
                )}
            </div>
        </div>
    );
};