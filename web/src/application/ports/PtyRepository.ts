export interface CreateSessionRequest {
    workspace_id: string;
    cwd_rel?: string;
    title?: string;
    shell?: string;
    cols?: number;
    rows?: number;
}

export interface CreateSessionResponse {
    session_id: string;
    ws_url: string;
    resolved_cwd: string;
}

export interface PtyRepository {
    createSession(request: CreateSessionRequest): Promise<CreateSessionResponse>;
    terminateSession(sessionId: string): Promise<void>;
}
