import { createContext, useContext, type ReactNode } from 'react';
import { type Runtime, createRuntime } from './createRuntime';

const RuntimeContext = createContext<Runtime | null>(null);

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
