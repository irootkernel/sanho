import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';

export type ConsoleStatus = 'CREATED' | 'CONNECTING' | 'CONNECTED' | 'CLOSED' | 'ERROR';

export interface ConsoleRecord {
    consoleId: string;      // Client-side unique UUID
    sessionId?: string;     // Server-side session ID
    wsUrl?: string;         // WebSocket URL for streaming
    workspaceId?: string;
    project?: string;
    title: string;
    status: ConsoleStatus;
    createdAt: number;
    errorMessage?: string;

    // runtime objects
    xterm?: Terminal;
    fitAddon?: FitAddon;
    ws?: WebSocket;
}
