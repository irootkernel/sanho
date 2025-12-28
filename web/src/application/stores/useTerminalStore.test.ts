/** @vitest-environment jsdom */
import { describe, it, expect } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useTerminalStore } from './useTerminalStore';
import type { ConsoleRecord } from '@/domain/terminal/types';

describe('useTerminalStore', () => {
    const sampleRecord: ConsoleRecord = {
        consoleId: 'test-id-1',
        title: 'Test Console',
        status: 'CREATED',
        createdAt: Date.now(),
    };

    it('should start with empty consoles', () => {
        const { result } = renderHook(() => useTerminalStore());
        expect(result.current.consoles).toEqual([]);
        expect(result.current.selectedConsoleId).toBeNull();
    });

    it('should add a console and select it', () => {
        const { result } = renderHook(() => useTerminalStore());
        
        act(() => {
            result.current.addConsole(sampleRecord);
        });

        expect(result.current.consoles).toHaveLength(1);
        expect(result.current.consoles[0]).toEqual(sampleRecord);
        expect(result.current.selectedConsoleId).toBe('test-id-1');
    });

    it('should remove a console and deselect if active', () => {
        const { result } = renderHook(() => useTerminalStore());
        
        act(() => {
            result.current.addConsole(sampleRecord);
        });
        expect(result.current.selectedConsoleId).toBe('test-id-1');

        act(() => {
            result.current.removeConsole('test-id-1');
        });

        expect(result.current.consoles).toHaveLength(0);
        expect(result.current.selectedConsoleId).toBeNull();
    });

    it('should select a console', () => {
        const { result } = renderHook(() => useTerminalStore());
        const record2 = { ...sampleRecord, consoleId: 'id-2', title: 'Second' };
        
        act(() => {
            result.current.addConsole(sampleRecord);
            result.current.addConsole(record2);
        });

        act(() => {
            result.current.selectConsole('test-id-1');
        });
        expect(result.current.selectedConsoleId).toBe('test-id-1');

        act(() => {
            result.current.selectConsole('id-2');
        });
        expect(result.current.selectedConsoleId).toBe('id-2');
    });

    it('should update console status', () => {
        const { result } = renderHook(() => useTerminalStore());
        
        act(() => {
            result.current.addConsole(sampleRecord);
        });

        act(() => {
            result.current.updateConsole('test-id-1', { status: 'CONNECTED' });
        });

        expect(result.current.consoles[0].status).toBe('CONNECTED');
    });

    it('should update console with partial data', () => {
        const { result } = renderHook(() => useTerminalStore());
        
        act(() => {
            result.current.addConsole(sampleRecord);
        });

        act(() => {
            result.current.updateConsole('test-id-1', {
                status: 'CONNECTED',
                sessionId: 'server-sess-1',
            });
        });

        expect(result.current.consoles[0].status).toBe('CONNECTED');
        expect(result.current.consoles[0].sessionId).toBe('server-sess-1');
    });

    it('should respect MAX_CONSOLES limit', () => {
        const { result } = renderHook(() => useTerminalStore());
        
        act(() => {
            for (let i = 0; i < 10; i++) {
                result.current.addConsole({
                    ...sampleRecord,
                    consoleId: `id-${i}`,
                    title: `Console ${i}`,
                });
            }
        });

        expect(result.current.consoles).toHaveLength(5); // MAX_CONSOLES is 5
        expect(result.current.consoles[4].consoleId).toBe('id-4');
    });

    it('should reorder consoles', () => {
        const { result } = renderHook(() => useTerminalStore());
        const record2 = { ...sampleRecord, consoleId: 'id-2', title: 'Second' };
        
        act(() => {
            result.current.addConsole(sampleRecord);
            result.current.addConsole(record2);
        });

        expect(result.current.consoles[0].consoleId).toBe('test-id-1');

        act(() => {
            result.current.reorderConsoles(0, 1);
        });

        expect(result.current.consoles[0].consoleId).toBe('id-2');
        expect(result.current.consoles[1].consoleId).toBe('test-id-1');
    });
});
