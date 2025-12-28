import React, { useEffect, useState, useMemo, useCallback } from 'react';
import { fetchWorkspaces, type Workspace } from '@/api/state';
import { useTerminal } from '@/application';
import { WorkspaceGrid } from './WorkspaceGrid';

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
    const { consoles } = useTerminal();
    const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
    const [isLoading, setIsLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [searchQuery, setSearchQuery] = useState('');
    const [focusedIndex, setFocusedIndex] = useState(0);

    const activeWorkspaceIds = useMemo(() => 
        new Set(consoles.map(c => c.workspaceId).filter((id): id is string => !!id)), 
    [consoles]);

    useEffect(() => {
        if (isOpen) {
            loadWorkspaces();
            setSearchQuery(''); 
            setFocusedIndex(0);
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

    useEffect(() => {
        setFocusedIndex(0);
    }, [filteredWorkspaces]);

    const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
        if (filteredWorkspaces.length === 0) return;
        switch (e.key) {
            case 'ArrowDown':
                e.preventDefault();
                setFocusedIndex(prev => (prev + 1) % filteredWorkspaces.length);
                break;
            case 'ArrowUp':
                e.preventDefault();
                setFocusedIndex(prev => (prev - 1 + filteredWorkspaces.length) % filteredWorkspaces.length);
                break;
            case 'Enter':
                e.preventDefault();
                onSelect(filteredWorkspaces[focusedIndex]);
                break;
            case 'Escape':
                onClose();
                break;
        }
    }, [filteredWorkspaces, focusedIndex, onSelect, onClose]);

    if (!isOpen) return null;

    return (
        <div 
            role="dialog"
            aria-modal="true"
            style={{
                position: 'fixed', top: 0, left: 0, right: 0, bottom: 0,
                backgroundColor: 'rgba(0, 0, 0, 0.4)', zIndex: 2000,
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                backdropFilter: 'blur(2px)'
            }} 
            onClick={onClose} 
            onKeyDown={handleKeyDown} 
            tabIndex={-1}
        >
            <div style={{
                width: '90%', maxWidth: '860px', maxHeight: '80vh',
                backgroundColor: '#ffffff', borderRadius: '12px',
                boxShadow: '0 20px 50px rgba(0,0,0,0.15)',
                display: 'flex', flexDirection: 'column', overflow: 'hidden'
            }} onClick={e => e.stopPropagation()}>
                
                {/* Search Header */}
                <div style={{ padding: '24px', borderBottom: '1px solid #eee', backgroundColor: '#fff' }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
                        <span style={{ fontSize: '14px', fontWeight: 'bold', color: '#212529' }}>Select Workspace</span>
                        <button onClick={onClose} style={{ background: 'none', border: 'none', color: '#ccc', cursor: 'pointer', fontSize: '24px', padding: 0 }}>&times;</button>
                    </div>
                    <div style={{ position: 'relative' }}>
                        <input
                            type="text"
                            placeholder="Search by project, ID or path..."
                            value={searchQuery}
                            onChange={e => setSearchQuery(e.target.value)}
                            autoFocus
                            style={{
                                width: '100%', padding: '14px 16px',
                                backgroundColor: '#f8f9fa', border: '1px solid #dee2e6',
                                borderRadius: '8px', color: '#212529', fontSize: '16px',
                                outline: 'none', boxShadow: 'inset 0 1px 2px rgba(0,0,0,0.03)'
                            }}
                        />
                    </div>
                </div>

                {/* Content */}
                <div style={{ padding: '24px', overflowY: 'auto', flex: 1 }}>
                    {isLoading ? (
                        <div style={{ textAlign: 'center', padding: '60px', color: '#adb5bd' }}>
                            <div className="spinner-border text-primary mb-3"></div>
                            <div style={{ fontSize: '14px' }}>Loading workspaces...</div>
                        </div>
                    ) : error ? (
                        <div style={{ padding: '16px', backgroundColor: '#fff5f5', color: '#dc3545', borderRadius: '8px', border: '1px solid #ffe3e3', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                            <span>{error}</span>
                            <button className="btn btn-sm btn-outline-danger" onClick={loadWorkspaces}>Retry</button>
                        </div>
                    ) : (
                        <WorkspaceGrid 
                            workspaces={filteredWorkspaces}
                            activeWorkspaceIds={activeWorkspaceIds}
                            focusedIndex={focusedIndex}
                            onSelect={onSelect}
                        />
                    )}
                </div>

                {/* Footer */}
                <div style={{ padding: '12px 24px', backgroundColor: '#f8f9fa', borderTop: '1px solid #eee', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <div style={{ color: '#6c757d', fontSize: '12px' }}>
                        <span style={{ marginRight: '16px' }}><kbd style={{ background: '#fff', color: '#6c757d', border: '1px solid #dee2e6', padding: '2px 4px', borderRadius: '3px' }}>&uarr;&darr;</kbd> Navigate</span>
                        <span><kbd style={{ background: '#fff', color: '#6c757d', border: '1px solid #dee2e6', padding: '2px 4px', borderRadius: '3px' }}>Enter</kbd> Select</span>
                    </div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
                        <div style={{ color: '#adb5bd', fontSize: '12px' }}>{filteredWorkspaces.length} workspaces</div>
                        <button onClick={onClose} className="btn btn-light btn-sm" style={{ padding: '4px 12px', fontSize: '12px', fontWeight: '600' }}>Cancel</button>
                    </div>
                </div>
            </div>
        </div>
    );
};
