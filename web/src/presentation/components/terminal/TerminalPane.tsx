import React, { useState, useEffect, useRef } from 'react';
import { useTerminal } from '@/application';
import { terminateSession, connectConsole } from '@/api/pty';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';

interface TerminalPaneProps {
    consoleId?: string;
}

export const TerminalPane: React.FC<TerminalPaneProps> = ({ consoleId }) => {
    const { consoles, selectedConsoleId, removeConsole, updateConsole } = useTerminal();
    const [isTerminating, setIsTerminating] = useState(false);
    const terminalRef = useRef<HTMLDivElement>(null);

    // Instance refs to maintain state across re-renders
    const xtermRef = useRef<Terminal | null>(null);
    const wsRef = useRef<WebSocket | null>(null);
    const fitAddonRef = useRef<FitAddon | null>(null);
    const consoleIdRef = useRef<string | null>(null);

    const activeConsole = consoles.find((c) => c.consoleId === (consoleId || selectedConsoleId));
    const targetId = consoleId || selectedConsoleId;

    // Helper to sync size with server
    const syncSizeWithServer = () => {
        if (fitAddonRef.current && xtermRef.current && wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
            fitAddonRef.current.fit();
            const { cols, rows } = xtermRef.current;
            wsRef.current.send(JSON.stringify({
                type: 'resize',
                cols,
                rows
            }));
        }
    };

    // Effect 1: Initialize Xterm instance
    useEffect(() => {
        if (!targetId || !activeConsole || !terminalRef.current) return;
        if (consoleIdRef.current === targetId) return;
        consoleIdRef.current = targetId;

        const term = new Terminal({
            cursorBlink: true,
            fontSize: 14,
            fontFamily: '"MesloLGS NF", Menlo, Monaco, "Courier New", monospace',
            theme: { background: '#000000', foreground: '#ffffff' },
            allowProposedApi: true
        });

        const fitAddon = new FitAddon();
        term.loadAddon(fitAddon);
        term.loadAddon(new WebLinksAddon());

        term.open(terminalRef.current);

        // Wait for fonts to load before fitting to ensure correct calculation
        if (document.fonts) {
            document.fonts.ready.then(() => {
                fitAddon.fit();
            });
        } else {
            // Fallback for environments without document.fonts (like JSDOM)
            setTimeout(() => fitAddon.fit(), 0);
        }

        xtermRef.current = term;
        fitAddonRef.current = fitAddon;

        term.writeln('\x1b[1;32mWelcome to Kkachi Terminal!\x1b[0m');
        term.writeln(`Connecting to workspace: ${activeConsole.workspaceId}...`);

        const disposable = term.onData((data) => {
            if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
                wsRef.current.send(new TextEncoder().encode(data));
            }
        });

        updateConsole(targetId, { xterm: term, fitAddon: fitAddon });

        return () => {
            disposable.dispose();
            term.dispose();
            if (wsRef.current) wsRef.current.close();
            xtermRef.current = null;
            wsRef.current = null;
            consoleIdRef.current = null;
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [targetId]);

    // Effect 2: Manage WebSocket Connection
    useEffect(() => {
        if (!activeConsole?.wsUrl || wsRef.current) return;
        if (activeConsole.status !== 'CREATED' && activeConsole.status !== 'CONNECTING') return;

        const term = xtermRef.current;
        if (!term) return;

        try {
            const ws = connectConsole(activeConsole.wsUrl);
            wsRef.current = ws;

            ws.onopen = () => {
                updateConsole(activeConsole.consoleId, { status: 'CONNECTED' });
                setTimeout(() => {
                    term.focus();
                    syncSizeWithServer(); // Initial sync after connection
                }, 50);
            };

            ws.onclose = (event) => {
                if (!event.wasClean) {
                    term.writeln(`\r\n\x1b[31mConnection lost (Code: ${event.code}).\x1b[0m`);
                }
                updateConsole(activeConsole.consoleId, { status: 'CLOSED' });
                wsRef.current = null;
            };

            ws.onerror = () => {
                term.writeln(`\r\n\x1b[31mWebSocket connection error.\x1b[0m`);
                updateConsole(activeConsole.consoleId, { status: 'ERROR', errorMessage: 'WebSocket connection failed' });
                wsRef.current = null;
            };

            ws.onmessage = (event) => {
                if (event.data instanceof ArrayBuffer) {
                    term.write(new Uint8Array(event.data));
                } else if (typeof event.data === 'string') {
                    try {
                        const msg = JSON.parse(event.data);
                        if (msg.type === 'exit') {
                            term.writeln(`\r\n\x1b[33mProcess exited with code ${msg.exit_code}\x1b[0m`);
                            updateConsole(activeConsole.consoleId, { status: 'CLOSED' });
                        } else if (msg.type === 'error') {
                            term.writeln(`\r\n\x1b[31mError: ${msg.error}\x1b[0m`);
                        }
                    } catch { /* ignore */ }
                }
            };

            updateConsole(activeConsole.consoleId, { ws });

            return () => {
                if (wsRef.current) {
                    wsRef.current.close();
                    wsRef.current = null;
                }
            };
        } catch {
            updateConsole(activeConsole.consoleId, { status: 'ERROR', errorMessage: 'Failed to connect WebSocket' });
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [activeConsole?.wsUrl]);

    // Effect 3: Robust Resize Handling with ResizeObserver (Debounced)
    useEffect(() => {
        if (!terminalRef.current) return;

        let debounceTimer: ReturnType<typeof setTimeout>;
        const observer = new ResizeObserver(() => {
            clearTimeout(debounceTimer);
            debounceTimer = setTimeout(() => {
                syncSizeWithServer();
            }, 100);
        });

        observer.observe(terminalRef.current);

        return () => {
            observer.disconnect();
            clearTimeout(debounceTimer);
        };
    }, [targetId]);

    // Effect 4: Focus when active
    useEffect(() => {
        if (selectedConsoleId === targetId && xtermRef.current) {
            xtermRef.current.focus();
            setTimeout(syncSizeWithServer, 100); // Re-sync size when switching back
        }
    }, [selectedConsoleId, targetId]);

    const handleClose = async () => {
        if (!activeConsole || !targetId) return;
        setIsTerminating(true);
        try {
            if (activeConsole.sessionId) await terminateSession(activeConsole.sessionId);
        } catch {
            // ignore
        } finally {
            if (wsRef.current) wsRef.current.close();
            if (xtermRef.current) xtermRef.current.dispose();
            setIsTerminating(false);
            removeConsole(targetId);
        }
    };

    if (!activeConsole) return null;

    return (
        <div className="terminal-pane" style={{
            height: '100%', display: 'flex', flexDirection: 'column',
            backgroundColor: '#000000', overflow: 'hidden'
        }}>
            {/* Minimal Toolbar */}
            <div style={{
                padding: '10px 16px', display: 'flex', justifyContent: 'space-between',
                alignItems: 'center', backgroundColor: '#1e1e1e', borderBottom: '1px solid #333'
            }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                    <div style={{
                        width: '8px', height: '8px', borderRadius: '50%',
                        backgroundColor: activeConsole.status === 'CONNECTED' ? '#28a745' : '#ffc107',
                        boxShadow: activeConsole.status === 'CONNECTED' ? '0 0 6px #28a745' : 'none'
                    }}></div>
                    <span style={{ color: '#fff', fontSize: '12px', fontWeight: '600', fontFamily: 'monospace' }}>
                        {activeConsole.title}
                    </span>
                    <span style={{ color: '#666', fontSize: '11px', fontFamily: 'monospace' }}>
                        {activeConsole.project}
                    </span>
                </div>
                <button
                    onClick={handleClose}
                    disabled={isTerminating}
                    style={{
                        background: 'none', border: 'none', color: '#666',
                        cursor: 'pointer', display: 'flex', alignItems: 'center',
                        padding: '4px'
                    }}
                >
                    {isTerminating ? (
                        <div className="spinner-border spinner-border-sm" style={{ width: '12px', height: '14px' }}></div>
                    ) : (
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>
                    )}
                </button>
            </div>

            {/* Terminal Viewport */}
            <div
                style={{ flex: 1, padding: '4px', backgroundColor: '#000000', position: 'relative' }}
                onClick={() => xtermRef.current?.focus()}
            >
                <div ref={terminalRef} style={{ width: '100%', height: '100%' }}></div>
                {activeConsole.status === 'ERROR' && (
                    <div style={{
                        position: 'absolute', top: '50%', left: '50%', transform: 'translate(-50%, -50%)',
                        backgroundColor: 'rgba(220, 53, 69, 0.95)', color: '#fff',
                        padding: '16px 24px', borderRadius: '6px', fontSize: '13px', zIndex: 100,
                        boxShadow: '0 4px 12px rgba(0,0,0,0.2)', textAlign: 'center'
                    }}>
                        <div style={{ fontWeight: 'bold', marginBottom: '4px' }}>Session Error</div>
                        <div>{activeConsole.errorMessage}</div>
                    </div>
                )}
            </div>
        </div >
    );
};