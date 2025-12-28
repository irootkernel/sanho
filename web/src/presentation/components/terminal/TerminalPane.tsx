import React, { useState, useEffect, useRef } from 'react';
import { useTerminal } from '@/application';
import { terminateSession, connectConsole } from '@/api/pty';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';

export const TerminalPane: React.FC = () => {
    const { consoles, selectedConsoleId, removeConsole, updateConsole } = useTerminal();
    const [isTerminating, setIsTerminating] = useState(false);
    const terminalRef = useRef<HTMLDivElement>(null);
    
    const activeConsole = consoles.find((c) => c.consoleId === selectedConsoleId);

    useEffect(() => {
        if (!activeConsole || !terminalRef.current) return;

        let term = activeConsole.xterm;

        // 1. Initialize xterm if needed
        if (!term) {
            // Initialize xterm
            term = new Terminal({
                cursorBlink: true,
                fontSize: 14,
                fontFamily: 'Menlo, Monaco, "Courier New", monospace',
                theme: {
                    background: '#000000',
                    foreground: '#ffffff',
                },
            });

            const fitAddon = new FitAddon();
            const webLinksAddon = new WebLinksAddon();

            term.loadAddon(fitAddon);
            term.loadAddon(webLinksAddon);

            term.open(terminalRef.current);
            fitAddon.fit();

            // Store in state
            updateConsole(activeConsole.consoleId, {
                xterm: term,
                fitAddon: fitAddon
            });
            
            // Initial greeting (Local only)
            term.writeln('\x1b[1;32mWelcome to Kkachi Terminal!\x1b[0m');
            term.writeln(`Connecting to workspace: ${activeConsole.workspaceId}...`);
        } else {
            // Re-attach existing terminal if needed
            if (terminalRef.current.childElementCount === 0) {
                 term.open(terminalRef.current);
                 activeConsole.fitAddon?.fit();
            }
        }

        // 2. Initialize WebSocket if needed
        if (!activeConsole.ws && activeConsole.wsUrl && (activeConsole.status === 'CREATED' || activeConsole.status === 'CONNECTING')) {
            // Check if we are already connecting/connected to avoid double init if status update lags
            // But we rely on activeConsole.ws check.
            
            try {
                const ws = connectConsole(activeConsole.wsUrl);
                
                ws.onopen = () => {
                    updateConsole(activeConsole.consoleId, { status: 'CONNECTED' });
                    term?.focus();
                };
                
                ws.onclose = (event) => {
                    term?.writeln(`\r\n\x1b[31mConnection closed (Code: ${event.code}). Please check if the server is running.\x1b[0m`);
                    updateConsole(activeConsole.consoleId, { status: 'CLOSED' });
                };
                
                ws.onerror = () => {
                    // onerror event doesn't contain error details in browser
                    term?.writeln(`\r\n\x1b[31mWebSocket Connection Error.\x1b[0m`);
                    updateConsole(activeConsole.consoleId, { status: 'ERROR', errorMessage: 'WebSocket connection error' });
                };
                
                ws.onmessage = (event) => {
                    if (event.data instanceof ArrayBuffer) {
                        // Binary output from PTY
                        term?.write(new Uint8Array(event.data));
                    } else if (typeof event.data === 'string') {
                        // Control message
                        try {
                            const msg = JSON.parse(event.data);
                            if (msg.type === 'exit') {
                                term?.writeln(`\r\n\x1b[33mProcess exited with code ${msg.exit_code}\x1b[0m`);
                                updateConsole(activeConsole.consoleId, { status: 'CLOSED' });
                            } else if (msg.type === 'error') {
                                term?.writeln(`\r\n\x1b[31mError: ${msg.error}\x1b[0m`);
                            }
                        } catch (e) {
                            console.error('Failed to parse control message', e);
                        }
                    }
                };
                
                // Attach input listener
                term.onData((data: string) => {
                    if (ws.readyState === WebSocket.OPEN) {
                        ws.send(new TextEncoder().encode(data));
                    }
                });
                
                
                updateConsole(activeConsole.consoleId, { ws });
            } catch {
                 updateConsole(activeConsole.consoleId, { status: 'ERROR', errorMessage: 'Failed to create WebSocket' });
            }
        }
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [activeConsole?.consoleId, activeConsole?.fitAddon, activeConsole?.xterm, activeConsole?.ws, activeConsole?.wsUrl, activeConsole?.status]);

    // 3. Resize Handling
    useEffect(() => {
        if (!activeConsole || !terminalRef.current || !activeConsole.fitAddon || !activeConsole.xterm) return;

        const observer = new ResizeObserver(() => {
            if (activeConsole.fitAddon && activeConsole.xterm) {
                activeConsole.fitAddon.fit();
                const cols = activeConsole.xterm.cols;
                const rows = activeConsole.xterm.rows;
                
                if (activeConsole.ws && activeConsole.ws.readyState === WebSocket.OPEN) {
                    activeConsole.ws.send(JSON.stringify({
                        type: 'resize',
                        cols,
                        rows
                    }));
                }
            }
        });

        observer.observe(terminalRef.current);
        return () => {
            observer.disconnect();
        };
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [activeConsole?.consoleId, activeConsole?.fitAddon, activeConsole?.xterm, activeConsole?.ws]);

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
            // Dispose resources
            activeConsole.ws?.close();
            activeConsole.xterm?.dispose();
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
                <div className="terminal-title small font-monospace text-truncate d-flex align-items-center">
                    <span className={`badge rounded-circle p-1 me-2 ${
                        activeConsole.status === 'CONNECTED' ? 'bg-success' : 
                        activeConsole.status === 'CONNECTING' ? 'bg-warning' : 
                        activeConsole.status === 'ERROR' ? 'bg-danger' : 'bg-secondary'
                    }`} style={{ width: '10px', height: '10px' }} title={activeConsole.status}></span>
                    {activeConsole.title}
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
            <div className="terminal-container flex-grow-1 p-0 bg-black position-relative">
                {activeConsole.status === 'ERROR' ? (
                    <div className="h-100 d-flex align-items-center justify-content-center p-4">
                        <div className="alert alert-danger mb-0">
                            <h6 className="alert-heading">Session Error</h6>
                            <p className="mb-0 small">{activeConsole.errorMessage}</p>
                        </div>
                    </div>
                ) : (
                    <div ref={terminalRef} className="h-100 w-100" style={{ overflow: 'hidden' }} />
                )}
            </div>
        </div>
    );
};