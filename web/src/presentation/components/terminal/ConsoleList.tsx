import React from 'react';
import { useTerminal } from '@/application';
import type { ConsoleStatus, ConsoleRecord } from '@/domain/terminal/types';
import {
    DndContext,
    closestCenter,
    DragOverlay,
    type DragEndEvent,
    type DragStartEvent,
} from '@dnd-kit/core';
import {
    SortableContext,
    useSortable,
    verticalListSortingStrategy,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useCommonSensors } from '../../hooks/useCommonSensors';

interface ConsoleListProps {
    onNew?: () => void;
    isNewDisabled?: boolean;
    onRenameRequest?: (console: ConsoleRecord) => void;
}

const getStatusColor = (status: ConsoleStatus) => {
    switch (status) {
        case 'CONNECTED': return '#28a745';
        case 'ERROR': return '#dc3545';
        case 'CLOSED': return '#6c757d';
        default: return '#007fd4';
    }
};

interface SortableConsoleItemProps {
    console: ConsoleRecord;
    isActive: boolean;
    onSelect: (id: string) => void;
    onClose: (id: string) => void;
    onRenameRequest?: (console: ConsoleRecord) => void;
}

const SortableConsoleItem: React.FC<SortableConsoleItemProps> = ({ console, isActive, onSelect, onClose, onRenameRequest }) => {
    const {
        attributes,
        listeners,
        setNodeRef,
        transform,
        transition,
        isDragging,
    } = useSortable({ id: console.consoleId });

    const style = {
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.3 : 1,
        position: 'relative' as const,
        zIndex: isDragging ? 999 : 'auto',
    };

    const statusColor = getStatusColor(console.status);

    return (
        <div
            ref={setNodeRef}
            style={style}
            {...attributes}
            {...listeners}
            onClick={() => onSelect(console.consoleId)}
            onDoubleClick={() => onRenameRequest?.(console)}
            data-testid={`console-item-${console.title}`}
            className="console-item"
            aria-label={`Terminal session ${console.title} for project ${console.project}. Press space to reorder. Double click to rename.`}
        >

                <div style={{

                    padding: '12px',

                    borderRadius: '8px',

                    marginBottom: '6px',

                    backgroundColor: isActive ? '#fff' : 'transparent',

                    border: `1px solid ${isActive ? '#dee2e6' : 'transparent'} `,

                    boxShadow: isDragging 

                        ? '0 8px 24px rgba(13, 110, 253, 0.25)' 

                        : isActive ? '0 2px 4px rgba(0,0,0,0.05)' : 'none',

                    cursor: isDragging ? 'grabbing' : 'grab',

                    display: 'flex',

                    flexDirection: 'column',

                    gap: '4px',

                    transition: 'all 0.2s cubic-bezier(0.4, 0, 0.2, 1)',

                    position: 'relative',

                    overflow: 'hidden',

                    outline: isDragging ? '2px solid #0d6efd' : 'none',

                }}>

                    {isActive && <div style={{ position: 'absolute', left: 0, top: 0, bottom: 0, width: '4px', backgroundColor: '#0d6efd' }} />}
                
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <div style={{ flex: 1, overflow: 'hidden' }}>
                        <div style={{ fontSize: '10px', color: '#adb5bd', fontWeight: 'bold', textTransform: 'uppercase' }}>
                            {console.project}
                        </div>
                        <div style={{ fontSize: '13px', color: '#212529', fontWeight: '600', textOverflow: 'ellipsis', overflow: 'hidden', whiteSpace: 'nowrap' }}>
                            {console.title}
                        </div>
                    </div>
                    <button 
                        onClick={(e) => {
                            e.stopPropagation();
                            onClose(console.consoleId);
                        }}
                        data-testid="close-console-button"
                        style={{ 
                            background: 'none', border: 'none', color: '#adb5bd', 
                            cursor: 'pointer', padding: '4px', borderRadius: '4px',
                            display: 'flex', alignItems: 'center', justifyContent: 'center'
                        }}
                        title="Close"
                        onPointerDown={(e) => e.stopPropagation()} // Prevent drag start on button
                    >
                        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
                    </button>
                </div>
                
                <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                    <div style={{ width: '6px', height: '6px', borderRadius: '50%', backgroundColor: statusColor }}></div>
                    <span style={{ fontSize: '11px', color: '#6c757d' }}>{console.status}</span>
                </div>
            </div>
        </div>
    );
};

export const ConsoleList: React.FC<ConsoleListProps> = ({ onNew, isNewDisabled, onRenameRequest }) => {
    const { consoles, selectedConsoleId, selectConsole, removeConsole, reorderConsoles } = useTerminal();
    const [activeId, setActiveId] = React.useState<string | null>(null);
    const sensors = useCommonSensors();

    const handleDragStart = (event: DragStartEvent) => {
        setActiveId(event.active.id as string);
    };

    const handleDragEnd = (event: DragEndEvent) => {
        const { active, over } = event;

        if (over && active.id !== over.id) {
            const oldIndex = consoles.findIndex((c) => c.consoleId === active.id);
            const newIndex = consoles.findIndex((c) => c.consoleId === over.id);
            reorderConsoles(oldIndex, newIndex);
        }
        setActiveId(null);
    };

    const activeConsole = React.useMemo(
        () => consoles.find((c) => c.consoleId === activeId),
        [activeId, consoles]
    );

    return (
        <div style={{
            width: '260px',
            height: '100%',
            display: 'flex',
            flexDirection: 'column',
            backgroundColor: '#f8f9fa',
            borderRight: '1px solid #dee2e6'
        }}>
            {/* Sidebar Header */}
            <div style={{
                padding: '16px',
                borderBottom: '1px solid #dee2e6',
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                backgroundColor: '#fff'
            }}>
                <span style={{ fontSize: '11px', fontWeight: '700', color: '#6c757d', textTransform: 'uppercase', letterSpacing: '1px' }}>
                    Sessions
                </span>
                <button 
                    onClick={onNew}
                    disabled={isNewDisabled}
                    className="btn-new-console"
                    style={{
                        padding: '4px 12px',
                        borderRadius: '4px',
                        backgroundColor: isNewDisabled ? '#e9ecef' : '#0d6efd',
                        color: isNewDisabled ? '#adb5bd' : '#fff',
                        border: 'none',
                        fontSize: '12px',
                        fontWeight: '600',
                        cursor: isNewDisabled ? 'not-allowed' : 'pointer',
                        display: 'flex',
                        alignItems: 'center',
                        gap: '6px'
                    }}
                >
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>
                    New
                </button>
            </div>

            {/* Session Items */}
            <div style={{ flex: 1, overflowY: 'auto', padding: '8px' }}>
                {consoles.length === 0 ? (
                    <div style={{ padding: '24px', textAlign: 'center', color: '#adb5bd', fontSize: '13px' }}>
                        No active sessions
                    </div>
                ) : (
                    <DndContext
                        sensors={sensors}
                        collisionDetection={closestCenter}
                        onDragStart={handleDragStart}
                        onDragEnd={handleDragEnd}
                    >
                        <SortableContext
                            items={consoles.map(c => c.consoleId)}
                            strategy={verticalListSortingStrategy}
                        >
                            {consoles.map((console) => (
                                <SortableConsoleItem
                                    key={console.consoleId}
                                    console={console}
                                    isActive={selectedConsoleId === console.consoleId}
                                    onSelect={selectConsole}
                                    onClose={removeConsole}
                                    onRenameRequest={onRenameRequest}
                                />
                            ))}
                        </SortableContext>
                        <DragOverlay>
                            {activeConsole ? (
                                <SortableConsoleItem
                                    console={activeConsole}
                                    isActive={selectedConsoleId === activeConsole.consoleId}
                                    onSelect={() => {}}
                                    onClose={() => {}}
                                    onRenameRequest={() => {}}
                                />
                            ) : null}
                        </DragOverlay>
                    </DndContext>
                )}
            </div>
        </div>
    );
};