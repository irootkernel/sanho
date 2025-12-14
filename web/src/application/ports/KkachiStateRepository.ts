import type { KkachiState } from '@/domain';

/**
 * KkachiStateRepository is the port (interface) for fetching kkachi state.
 * Implementations can fetch from API, mock data, or other sources.
 */
export interface KkachiStateRepository {
    /**
     * Fetches the current kkachi state from the server.
     * @returns Promise resolving to KkachiState
     * @throws Error on network or server failure
     */
    getState(): Promise<KkachiState>;
}
