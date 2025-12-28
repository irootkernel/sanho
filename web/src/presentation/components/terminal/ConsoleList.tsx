import React from 'react';
import { useTerminal } from '@/application';
import type { ConsoleStatus } from '@/domain/terminal/types';

interface ConsoleListProps {
    onNew?: () => void;
    isNewDisabled?: boolean;
}

export const ConsoleList: React.FC<ConsoleListProps> = ({ onNew, isNewDisabled }) => {
    const { consoles, selectedConsoleId, selectConsole, removeConsole } = useTerminal();

    const getStatusColor = (status: ConsoleStatus) => {
        switch (status) {
            case 'CONNECTED': return '#28a745';
            case 'ERROR': return '#dc3545';
            case 'CLOSED': return '#6c757d';
            default: return '#007fd4';
        }
    };

    const handleClose = (e: React.MouseEvent, consoleId: string) => {
        e.stopPropagation();
        removeConsole(consoleId);
    };

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
                    consoles.map((console) => {
                        const active = selectedConsoleId === console.consoleId;
                        const statusColor = getStatusColor(console.status);
                        
                        return (
                            <div
                                key={console.consoleId}
                                onClick={() => selectConsole(console.consoleId)}
                                data-testid={`console-item-${console.title}`}
                                style={{
                                    padding: '12px',
                                    borderRadius: '6px',
                                    marginBottom: '4px',
                                    backgroundColor: active ? '#fff' : 'transparent',
                                    border: `1px solid ${active ? '#dee2e6' : 'transparent'}`,
                                    boxShadow: active ? '0 2px 4px rgba(0,0,0,0.05)' : 'none',
                                    cursor: 'pointer',
                                    display: 'flex',
                                    flexDirection: 'column',
                                    gap: '4px',
                                    transition: 'all 0.1s ease',
                                    position: 'relative',
                                    overflow: 'hidden'
                                }}
                            >
                                {active && <div style={{ position: 'absolute', left: 0, top: 0, bottom: 0, width: '3px', backgroundColor: '#0d6efd' }} />}
                                
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
                                        onClick={(e) => handleClose(e, console.consoleId)}
                                        data-testid="close-console-button"
                                        style={{ 
                                            background: 'none', border: 'none', color: '#adb5bd', 
                                            cursor: 'pointer', padding: '4px', borderRadius: '4px',
                                            display: 'flex', alignItems: 'center', justifyContent: 'center'
                                        }}
                                        title="Close"
                                    >
                                        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
                                    </button>
                                </div>
                                
                                <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                                    <div style={{ width: '6px', height: '6px', borderRadius: '50%', backgroundColor: statusColor }}></div>
                                    <span style={{ fontSize: '11px', color: '#6c757d' }}>{console.status}</span>
                                </div>
                            </div>
                        );
                    })
                )}
            </div>
        </div>
    );
};