import React, { useMemo } from 'react';
import type { Workspace } from '@/api/state';
import { WorkspaceCard } from './WorkspaceCard';
import {
    useSortable,
    SortableContext,
    rectSortingStrategy,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';

interface WorkspaceGridProps {
    workspaces: Workspace[];
    activeWorkspaceIds: Set<string>;
    focusedIndex: number;
    onSelect: (workspace: Workspace) => void;
}

interface SortableWorkspaceCardProps {
    workspace: Workspace;
    isActive: boolean;
    isFocused: boolean;
    onSelect: (workspace: Workspace) => void;
}

const SortableWorkspaceCard: React.FC<SortableWorkspaceCardProps> = ({
    workspace,
    isActive,
    isFocused,
    onSelect,
}) => {
    const {
        attributes,
        listeners,
        setNodeRef,
        transform,
        transition,
        isDragging,
    } = useSortable({ id: workspace.workspace_id });

    const style = {
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.3 : 1,
        position: 'relative' as const,
        zIndex: isDragging ? 999 : 'auto',
        height: '100%',
    };

    return (
        <div 
            ref={setNodeRef} 
            style={style} 
            {...attributes} 
            {...listeners}
            aria-label={`Workspace ${workspace.workspace_id} in project ${workspace.project}. Path: ${workspace.local_path}. Press space to reorder.`}
        >
            <div style={{
                height: '100%',
                borderRadius: '12px',
                transition: 'all 0.2s cubic-bezier(0.4, 0, 0.2, 1)',
                boxShadow: isDragging ? '0 12px 30px rgba(13, 110, 253, 0.3)' : 'none',
                transform: isDragging ? 'scale(1.02)' : 'scale(1)',
                zIndex: isDragging ? 1000 : 1,
                position: 'relative',
                outline: isDragging ? '2px solid #0d6efd' : 'none',
            }}>
                <WorkspaceCard
                    workspace={workspace}
                    isActive={isActive}
                    isFocused={isFocused}
                    onSelect={onSelect}
                />
            </div>
        </div>
    );
};

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
        <SortableContext
            items={workspaces.map((w) => w.workspace_id)}
            strategy={rectSortingStrategy}
        >
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
                                        <SortableWorkspaceCard
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
        </SortableContext>
    );
};