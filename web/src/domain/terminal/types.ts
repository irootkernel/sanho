export type ConsoleStatus = 'CREATED' | 'CONNECTING' | 'CONNECTED' | 'CLOSED' | 'ERROR';

export interface ConsoleRecord {
    consoleId: string;      // Client-side unique UUID
    sessionId?: string;     // Server-side session ID
    workspaceId?: string;
    project?: string;
    title: string;
    status: ConsoleStatus;
    createdAt: number;
    errorMessage?: string;

    // runtime objects
    // xterm?: any; (We will add these later when implementing CTASK-3)
    // ws?: WebSocket;
}
