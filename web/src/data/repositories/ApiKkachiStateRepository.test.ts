import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { ApiKkachiStateRepository } from './ApiKkachiStateRepository';
import sampleState from '@/test/fixtures/api-state.sample.json';

describe('ApiKkachiStateRepository', () => {
    beforeEach(() => {
        vi.resetAllMocks();
    });

    afterEach(() => {
        vi.unstubAllGlobals();
    });

    describe('getState', () => {
        it('should parse sample fixture correctly', async () => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockResolvedValue({
                    ok: true,
                    json: () => Promise.resolve(sampleState),
                }),
            );

            const repository = new ApiKkachiStateRepository();
            const result = await repository.getState();

            expect(result.docs_heads).toEqual(sampleState.docs_heads);
            expect(result.workspaces).toHaveLength(sampleState.workspaces.length);
            expect(result.workspaces[0].workspace_id).toBe('ws-001');
            expect(result.workspaces[0].project).toBe('sudal');
        });

        it('should preserve null last_reported_at', async () => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockResolvedValue({
                    ok: true,
                    json: () => Promise.resolve(sampleState),
                }),
            );

            const repository = new ApiKkachiStateRepository();
            const result = await repository.getState();

            // Third workspace has null last_reported_at
            expect(result.workspaces[2].last_reported_at).toBeNull();
        });

        it('should throw NetworkError on fetch failure', async () => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockRejectedValue(new Error('Network error')),
            );

            const repository = new ApiKkachiStateRepository();

            await expect(repository.getState()).rejects.toThrow(
                'Failed to connect to server',
            );
        });

        it('should throw ApiError on non-OK response', async () => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockResolvedValue({
                    ok: false,
                    status: 500,
                    statusText: 'Internal Server Error',
                }),
            );

            const repository = new ApiKkachiStateRepository();

            await expect(repository.getState()).rejects.toThrow(
                'Server returned 500',
            );
        });

        it('should call correct API endpoint', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () =>
                    Promise.resolve({ docs_heads: {}, workspaces: [] }),
            });
            vi.stubGlobal('fetch', mockFetch);

            const repository = new ApiKkachiStateRepository();
            await repository.getState();

            expect(mockFetch).toHaveBeenCalledWith('/api/state');
        });
    });
});
