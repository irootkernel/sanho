import type { ReactNode } from 'react';
import { useMemo } from 'react';
import type { KkachiStateRepository } from '@/application';
import { KkachiStateStoreProvider } from '@/application';
import { ApiKkachiStateRepository } from '@/data';

interface AppProvidersProps {
    children: ReactNode;
    repository?: KkachiStateRepository;
}

/**
 * AppProviders wires up app-level providers.
 * Currently this only includes KkachiStateStoreProvider for global state caching.
 */
export function AppProviders({ children, repository }: AppProvidersProps) {
    const resolvedRepository = useMemo(
        () => repository ?? new ApiKkachiStateRepository(),
        [repository]
    );

    return (
        <KkachiStateStoreProvider repository={resolvedRepository}>
            {children}
        </KkachiStateStoreProvider>
    );
}

