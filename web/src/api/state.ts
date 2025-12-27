import { buildApiUrl } from '@/data/http/config';
import { ApiError, NetworkError } from '@/data/http/errors';

export interface Workspace {
    workspace_id: string;
    project: string;
    docs_repo_id: string;
    local_path: string;
    repo_url: string;
    docs_hash: string;
    last_reported_at?: string | null;
    last_actor_email: string;
}

interface StateResponseDto {
    docs_heads: Record<string, string>;
    workspaces: Workspace[];
}

export const fetchWorkspaces = async (): Promise<Workspace[]> => {
    const url = buildApiUrl('/state');

    let response: Response;
    try {
        response = await fetch(url);
    } catch (error) {
        throw new NetworkError(
            'Failed to connect to server',
            error instanceof Error ? error : undefined,
        );
    }

    if (!response.ok) {
        throw new ApiError(
            `Server returned ${response.status}: ${response.statusText}`,
            response.status,
        );
    }

    const dto: StateResponseDto = await response.json();
    
    // Basic validation
    if (!dto || !Array.isArray(dto.workspaces)) {
        throw new Error('Invalid response: missing or invalid "workspaces"');
    }

    return dto.workspaces;
};