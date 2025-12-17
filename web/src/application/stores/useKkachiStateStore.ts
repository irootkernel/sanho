import { useState, useCallback, useMemo, useEffect } from 'react';
import type { KkachiState } from '@/domain';
import type { KkachiStateRepository } from '../ports/KkachiStateRepository';

/**
 * KkachiStateStore manages the global state fetched from /api/state.
 * It provides caching so that route changes don't trigger new API calls.
 */
export interface KkachiStateStore {
    /** Current state data (null if not yet loaded or error) */
    data: KkachiState | null;
    /** Whether a fetch is in progress */
    isLoading: boolean;
    /** Error from the last fetch attempt */
    error: Error | null;
    /** Whether fetch has been attempted at least once */
    isInitialized: boolean;
    /** Force refresh the data (ignores cache) */
    refresh: () => Promise<void>;
}

/**
 * Creates a KkachiStateStore instance.
 * This is a custom hook that manages state fetching and caching.
 */
export function useKkachiStateStore(
    repository: KkachiStateRepository
): KkachiStateStore {
    const [data, setData] = useState<KkachiState | null>(null);
    const [isLoading, setIsLoading] = useState(false);
    const [error, setError] = useState<Error | null>(null);
    const [isInitialized, setIsInitialized] = useState(false);

    const fetchInternal = useCallback(async () => {
        setIsLoading(true);
        setError(null);
        try {
            const result = await repository.getState();
            setData(result);
        } catch (err) {
            setError(err instanceof Error ? err : new Error('Unknown error'));
        } finally {
            setIsLoading(false);
            setIsInitialized(true);
        }
    }, [repository]);

    const refresh = useCallback(async () => {
        await fetchInternal();
    }, [fetchInternal]);

    // Initial fetch on mount
    useEffect(() => {
        if (!isInitialized && !isLoading) {
            fetchInternal();
        }
    }, [isInitialized, isLoading, fetchInternal]);

    return useMemo(
        () => ({
            data,
            isLoading,
            error,
            isInitialized,
            refresh,
        }),
        [data, isLoading, error, isInitialized, refresh]
    );
}
