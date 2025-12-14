import { describe, it, expect, vi } from 'vitest';
import { GetKkachiState } from '@/application/usecases';
import type { KkachiStateRepository } from '@/application/ports';
import type { KkachiState } from '@/domain';

describe('GetKkachiState', () => {
    const mockState: KkachiState = {
        docs_heads: { sudal: 'abc123' },
        workspaces: [
            {
                workspace_id: 'ws-001',
                project: 'sudal',
                docs_repo_id: 'docs-sudal',
                local_path: '/path/to/workspace',
                repo_url: 'https://github.com/example/sudal',
                docs_hash: 'abc123',
                last_reported_at: '2024-12-14T10:00:00Z',
                last_actor_email: 'dev@example.com',
            },
        ],
    };

    it('should return state from repository', async () => {
        const mockRepository: KkachiStateRepository = {
            getState: vi.fn().mockResolvedValue(mockState),
        };

        const usecase = new GetKkachiState(mockRepository);
        const result = await usecase.execute();

        expect(result).toEqual(mockState);
        expect(mockRepository.getState).toHaveBeenCalledOnce();
    });

    it('should propagate repository errors', async () => {
        const mockError = new Error('Network error');
        const mockRepository: KkachiStateRepository = {
            getState: vi.fn().mockRejectedValue(mockError),
        };

        const usecase = new GetKkachiState(mockRepository);

        await expect(usecase.execute()).rejects.toThrow('Network error');
    });
});
