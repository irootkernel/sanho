import type { ReactNode } from 'react';
import { createRuntime, type Runtime } from './createRuntime';
import { RuntimeContext } from './RuntimeContextValue';

interface RuntimeProviderProps {
    children: ReactNode;
    runtime?: Runtime;
}

/**
 * RuntimeProvider provides the runtime context to the application.
 * If no runtime is provided, it creates the default production runtime.
 */
export function RuntimeProvider({ children, runtime }: RuntimeProviderProps) {
    const value = runtime ?? createRuntime();
    return (
        <RuntimeContext.Provider value={value}>
            {children}
        </RuntimeContext.Provider>
    );
}

