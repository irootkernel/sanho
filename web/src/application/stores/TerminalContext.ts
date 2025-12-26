import { createContext } from 'react';
import type { TerminalStore } from './useTerminalStore';

export const TerminalContext = createContext<TerminalStore | null>(null);
