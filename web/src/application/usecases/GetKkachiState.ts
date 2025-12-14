import type { KkachiState } from '@/domain';
import type { KkachiStateRepository } from '../ports/KkachiStateRepository';

/**
 * GetKkachiState is the use case for fetching kkachi state.
 * It delegates to the repository to fetch the current server state.
 */
export class GetKkachiState {
    private readonly repository: KkachiStateRepository;

    constructor(repository: KkachiStateRepository) {
        this.repository = repository;
    }

    /**
     * Executes the use case to fetch kkachi state.
     * @returns Promise resolving to KkachiState
     * @throws Error on network or server failure
     */
    async execute(): Promise<KkachiState> {
        return this.repository.getState();
    }
}
