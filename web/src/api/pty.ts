import { buildApiUrl, getApiHeaders, getApiConfig } from '@/data/http/config';
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
            headers: getApiHeaders(),
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
    const { authToken } = getApiConfig();
    
    // Set auth_token cookie for WebSocket authentication if token is present
    if (authToken) {
        // Set cookie with SameSite=Strict and path=/ for security
        document.cookie = `auth_token=${authToken}; Path=/; SameSite=Strict`;
    }

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
            headers: getApiHeaders(),
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
