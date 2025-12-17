import type { ReactNode } from 'react';
import type { KkachiStateRepository } from '../ports/KkachiStateRepository';
import { useKkachiStateStore } from './useKkachiStateStore';
import { KkachiStateStoreContext } from './useKkachiState';

interface KkachiStateStoreProviderProps {
    repository: KkachiStateRepository;
    children: ReactNode;
}

/**
 * Provider component that creates and provides the KkachiStateStore.
 */
export function KkachiStateStoreProvider({
    repository,
    children,
}: KkachiStateStoreProviderProps) {
    const store = useKkachiStateStore(repository);
    return (
        <KkachiStateStoreContext.Provider value={store}>
            {children}
        </KkachiStateStoreContext.Provider>
    );
}
