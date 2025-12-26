import { useContext } from 'react';
import { TerminalContext } from './TerminalContext';
import type { TerminalStore } from './useTerminalStore';

export const useTerminal = (): TerminalStore => {
    const context = useContext(TerminalContext);
    if (!context) {
        throw new Error('useTerminal must be used within a TerminalStoreProvider');
    }
    return context;
};
