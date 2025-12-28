import { useState, useCallback, useMemo } from 'react';
import type { ConsoleRecord } from '@/domain/terminal/types';

export const MAX_CONSOLES = 5;

export interface TerminalStore {
    consoles: ConsoleRecord[];
    selectedConsoleId: string | null;
    addConsole: (record: ConsoleRecord) => void;
    removeConsole: (consoleId: string) => void;
    selectConsole: (consoleId: string) => void;
    updateConsole: (consoleId: string, updates: Partial<ConsoleRecord>) => void;
    reorderConsoles: (startIndex: number, endIndex: number) => void;
}

export function useTerminalStore(): TerminalStore {
    const [consoles, setConsoles] = useState<ConsoleRecord[]>([]);
    const [selectedConsoleId, setSelectedConsoleId] = useState<string | null>(null);

    const addConsole = useCallback((record: ConsoleRecord) => {
        setConsoles((prev) => {
            if (prev.length >= MAX_CONSOLES) {
                return prev;
            }
            // Use setTimeout to defer setSelectedConsoleId to ensure it happens after state update if needed,
            // or just set it here since it's a separate state.
            setSelectedConsoleId(record.consoleId);
            return [...prev, record];
        });
    }, []);

    const removeConsole = useCallback((consoleId: string) => {
        setConsoles((prev) => prev.filter((c) => c.consoleId !== consoleId));
        if (selectedConsoleId === consoleId) {
            setSelectedConsoleId(null);
        }
    }, [selectedConsoleId]);

    const selectConsole = useCallback((consoleId: string) => {
        setSelectedConsoleId(consoleId);
    }, []);

    const updateConsole = useCallback((consoleId: string, updates: Partial<ConsoleRecord>) => {
        setConsoles((prev) =>
            prev.map((c) =>
                c.consoleId === consoleId ? { ...c, ...updates } : c
            )
        );
    }, []);

    const reorderConsoles = useCallback((startIndex: number, endIndex: number) => {
        setConsoles((prev) => {
            const result = Array.from(prev);
            const [removed] = result.splice(startIndex, 1);
            result.splice(endIndex, 0, removed);
            return result;
        });
    }, []);

    return useMemo(
        () => ({
            consoles,
            selectedConsoleId,
            addConsole,
            removeConsole,
            selectConsole,
            updateConsole,
            reorderConsoles,
        }),
        [consoles, selectedConsoleId, addConsole, removeConsole, selectConsole, updateConsole, reorderConsoles]
    );
}
