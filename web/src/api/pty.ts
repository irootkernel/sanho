// web/src/api/pty.ts
// Stub for PTY session management APIs

export interface CreateSessionRequest {
    workspace_id: string;
    cwd_rel?: string;
    title?: string;
    shell?: string;
    cols?: number;
    rows?: number;
}

export const createSession = async (request: CreateSessionRequest) => {
    console.log('API: createSession (STUB)', request);
    // Will be implemented in CTASK-2
    throw new Error('Not implemented');
};

export const terminateSession = async (sessionId: string) => {
    console.log('API: terminateSession (STUB)', sessionId);
    // Will be implemented in CTASK-2
    throw new Error('Not implemented');
};
