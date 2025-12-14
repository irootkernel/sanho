import type { KkachiState } from '@/domain';
import type { KkachiStateRepository } from '@/application';
import { buildApiUrl } from '../http/config';
import { ApiError, NetworkError } from '../http/errors';

/**
 * Response DTO from /api/state endpoint.
 * Matches server's ServerStateResponse structure.
 */
interface StateResponseDto {
    docs_heads: Record<string, string>;
    workspaces: WorkspaceDto[];
}

interface WorkspaceDto {
    workspace_id: string;
    project: string;
    docs_repo_id: string;
    local_path: string;
    repo_url: string;
    docs_hash: string;
    last_reported_at?: string | null;
    last_actor_email: string;
}

/**
 * ApiKkachiStateRepository is the concrete implementation of KkachiStateRepository.
 * It fetches state from the kkachi-server's /api/state endpoint.
 */
export class ApiKkachiStateRepository implements KkachiStateRepository {
    /**
     * Fetches state from /api/state endpoint.
     * @returns Promise resolving to KkachiState
     * @throws NetworkError on connection failure
     * @throws ApiError on non-OK response
     */
    async getState(): Promise<KkachiState> {
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
        return this.mapToKkachiState(dto);
    }

    private mapToKkachiState(dto: StateResponseDto): KkachiState {
        return {
            docs_heads: dto.docs_heads,
            workspaces: dto.workspaces.map((ws) => ({
                workspace_id: ws.workspace_id,
                project: ws.project,
                docs_repo_id: ws.docs_repo_id,
                local_path: ws.local_path,
                repo_url: ws.repo_url,
                docs_hash: ws.docs_hash,
                last_reported_at: ws.last_reported_at ?? null,
                last_actor_email: ws.last_actor_email,
            })),
        };
    }
}
