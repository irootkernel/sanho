import React, { useMemo } from 'react';
import type { Workspace } from '@/api/state';
import { WorkspaceCard } from './WorkspaceCard';

interface WorkspaceGridProps {
    workspaces: Workspace[];
    activeWorkspaceIds: Set<string>;
    focusedIndex: number;
    onSelect: (workspace: Workspace) => void;
}

export const WorkspaceGrid: React.FC<WorkspaceGridProps> = ({
    workspaces,
    activeWorkspaceIds,
    focusedIndex,
    onSelect,
}) => {
    const grouped = useMemo(() => {
        const groups: Record<string, Workspace[]> = {};
        workspaces.forEach((ws) => {
            if (!groups[ws.project]) groups[ws.project] = [];
            groups[ws.project].push(ws);
        });
        return groups;
    }, [workspaces]);

    return (
        <div className="workspace-grid">
            {Object.entries(grouped).map(([project, projectWorkspaces]) => (
                <div key={project} className="project-group mb-5">
                    <h5 style={{ 
                        color: '#495057', 
                        fontWeight: 'bold', 
                        borderBottom: '2px solid #f8f9fa', 
                        paddingBottom: '8px',
                        marginBottom: '16px',
                        fontSize: '14px',
                        textTransform: 'uppercase'
                    }}>
                        {project} ({projectWorkspaces.length})
                    </h5>
                    <div className="row row-cols-1 row-cols-md-2 row-cols-lg-3 g-3">
                        {projectWorkspaces.map((ws) => {
                            const globalIndex = workspaces.indexOf(ws);
                            return (
                                <div key={ws.workspace_id} className="col">
                                    <WorkspaceCard
                                        workspace={ws}
                                        isActive={activeWorkspaceIds.has(ws.workspace_id)}
                                        isFocused={globalIndex === focusedIndex}
                                        onSelect={onSelect}
                                    />
                                </div>
                            );
                        })}
                    </div>
                </div>
            ))}
        </div>
    );
};