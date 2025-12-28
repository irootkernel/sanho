import { buildApiUrl } from '@/data/http/config';
import { ApiError, NetworkError } from '@/data/http/errors';

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

export const createSession = async (request: CreateSessionRequest): Promise<CreateSessionResponse> => {
    const url = buildApiUrl('/pty/sessions');

    let response: Response;
    try {
        response = await fetch(url, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(request),
        });
    } catch (error) {
        throw new NetworkError(
            'Failed to connect to server',
            error instanceof Error ? error : undefined,
        );
    }

    if (!response.ok) {
        let message = `Server returned ${response.status}: ${response.statusText}`;
        try {
            const errorData = await response.json();
            if (errorData.message) {
                message = errorData.message;
            } else if (errorData.error) {
                message = errorData.error;
            }
        } catch {
            // Ignore parse error
        }
        throw new ApiError(message, response.status);
    }

    const data: CreateSessionResponse = await response.json();
    return data;
};

export const connectConsole = (url: string): WebSocket => {
    const ws = new WebSocket(url);
    ws.binaryType = 'arraybuffer';
    return ws;
};

export const terminateSession = async (sessionId: string): Promise<void> => {
    const url = buildApiUrl(`/pty/sessions/${sessionId}`);

    let response: Response;
    try {
        response = await fetch(url, {
            method: 'DELETE',
        });
    } catch (error) {
        throw new NetworkError(
            'Failed to connect to server',
            error instanceof Error ? error : undefined,
        );
    }

    if (!response.ok) {
        let message = `Server returned ${response.status}: ${response.statusText}`;
        try {
            const errorData = await response.json();
            if (errorData.message) {
                message = errorData.message;
            } else if (errorData.error) {
                message = errorData.error;
            }
        } catch {
            // Ignore parse error
        }
        throw new ApiError(message, response.status);
    }
};
