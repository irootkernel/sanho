import { describe, it, expect } from 'vitest';
import { GetKkachiState } from '@/application/usecases';
import { UnimplementedError } from '@/domain/errors';
import type { KkachiStateRepository } from '@/application/ports';

describe('GetKkachiState', () => {
    it('should throw UnimplementedError in CTASK-1', async () => {
        // Create a mock repository (won't be used since usecase throws first)
        const mockRepository: KkachiStateRepository = {
            getState: async () => ({ docs_heads: {}, workspaces: [] }),
        };

        const usecase = new GetKkachiState(mockRepository);

        await expect(usecase.execute()).rejects.toThrow(UnimplementedError);
        await expect(usecase.execute()).rejects.toThrow('Not implemented: GetKkachiState.execute');
    });
});
