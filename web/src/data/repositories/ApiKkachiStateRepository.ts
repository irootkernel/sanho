import type { KkachiState } from '@/domain';
import { UnimplementedError } from '@/domain';
import type { KkachiStateRepository } from '@/application';

/**
 * ApiKkachiStateRepository is the concrete implementation of KkachiStateRepository.
 * It fetches state from the kkachi-server's /api/state endpoint.
 *
 * In CTASK-1, this throws UnimplementedError.
 * In CTASK-2, this will be implemented with actual fetch calls.
 */
export class ApiKkachiStateRepository implements KkachiStateRepository {
    /**
     * Fetches state from /api/state endpoint.
     * @returns Promise resolving to KkachiState
     * @throws UnimplementedError in CTASK-1
     */
    async getState(): Promise<KkachiState> {
        // CTASK-1: Throw UnimplementedError
        // CTASK-2: Implement actual API call
        throw new UnimplementedError('ApiKkachiStateRepository.getState');
    }
}
