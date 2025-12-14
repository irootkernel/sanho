import type { KkachiState } from '@/domain';
import { UnimplementedError } from '@/domain';
import type { KkachiStateRepository } from '../ports/KkachiStateRepository';

/**
 * GetKkachiState is the use case for fetching kkachi state.
 * In CTASK-1, this throws UnimplementedError.
 * In CTASK-2, this will be implemented with actual repository calls.
 */
export class GetKkachiState {
    private readonly repository: KkachiStateRepository;

    constructor(repository: KkachiStateRepository) {
        this.repository = repository;
    }

    /**
     * Executes the use case to fetch kkachi state.
     * @returns Promise resolving to KkachiState
     * @throws UnimplementedError in CTASK-1
     */
    async execute(): Promise<KkachiState> {
        // CTASK-1: Throw UnimplementedError
        // CTASK-2: Uncomment the line below and remove the throw
        // return this.repository.getState();
        void this.repository; // Suppress unused warning in CTASK-1
        throw new UnimplementedError('GetKkachiState.execute');
    }
}
