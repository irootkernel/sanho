import React from 'react';
import type { ReactNode } from 'react';
import { useTerminalStore } from './useTerminalStore';
import { TerminalContext } from './TerminalContext';

export const TerminalStoreProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
    const store = useTerminalStore();
    return (
        <TerminalContext.Provider value={store}>
            {children}
        </TerminalContext.Provider>
    );
};
