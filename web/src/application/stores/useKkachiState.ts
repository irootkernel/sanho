import { createContext, useContext } from 'react';
import type { KkachiStateStore } from './useKkachiStateStore';

// Context for providing the store globally
export const KkachiStateStoreContext = createContext<KkachiStateStore | null>(null);

/**
 * Hook to access the KkachiStateStore from any component.
 * Must be used within a KkachiStateStoreProvider.
 */
export function useKkachiState(): KkachiStateStore {
    const store = useContext(KkachiStateStoreContext);
    if (!store) {
        throw new Error(
            'useKkachiState must be used within a KkachiStateStoreProvider'
        );
    }
    return store;
}
