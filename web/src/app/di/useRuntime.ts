import { useContext } from 'react';
import { RuntimeContext } from './RuntimeContextValue';
import type { Runtime } from './createRuntime';

/**
 * Hook to access the runtime context.
 * @throws Error if used outside of RuntimeProvider
 */
export function useRuntime(): Runtime {
    const runtime = useContext(RuntimeContext);
    if (!runtime) {
        throw new Error('useRuntime must be used within a RuntimeProvider');
    }
    return runtime;
}
