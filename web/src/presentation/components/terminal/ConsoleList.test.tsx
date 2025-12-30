import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ConsoleList } from './ConsoleList';
import { useTerminal } from '@/application';

// Mock useTerminal
vi.mock('@/application', () => ({
    useTerminal: vi.fn(),
}));

// Mock dnd-kit which uses ResizeObserver
class MockResizeObserver {
    observe = vi.fn();
    unobserve = vi.fn();
    disconnect = vi.fn();
}
vi.stubGlobal('ResizeObserver', MockResizeObserver);

describe('ConsoleList', () => {
    const mockSelectConsole = vi.fn();
    const mockRemoveConsole = vi.fn();
    const mockReorderConsoles = vi.fn();

    const defaultConsoles = [
        {
            consoleId: 'c1',
            workspaceId: 'ws1',
            project: 'Project A',
            title: 'Console 1',
            status: 'CONNECTED',
            createdAt: 1000,
        },
        {
            consoleId: 'c2',
            workspaceId: 'ws2',
            project: 'Project B',
            title: 'Console 2',
            status: 'CLOSED',
            createdAt: 2000,
        },
    ];

    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(useTerminal).mockReturnValue({
            consoles: defaultConsoles,
            selectedConsoleId: 'c1',
            selectConsole: mockSelectConsole,
            removeConsole: mockRemoveConsole,
            reorderConsoles: mockReorderConsoles,
            addConsole: vi.fn(),
            updateConsole: vi.fn(),
        } as unknown as ReturnType<typeof useTerminal>);
    });

    it('renders list of consoles', () => {
        render(<ConsoleList />);
        
        expect(screen.getByText('Console 1')).toBeInTheDocument();
        expect(screen.getByText('Project A')).toBeInTheDocument();
        expect(screen.getByText('Console 2')).toBeInTheDocument();
        expect(screen.getByText('Project B')).toBeInTheDocument();
    });

    it('handles console selection', () => {
        render(<ConsoleList />);
        
        fireEvent.click(screen.getByTestId('console-item-Console 2'));
        expect(mockSelectConsole).toHaveBeenCalledWith('c2');
    });

    it('handles console closing', () => {
        render(<ConsoleList />);
        
        // Find close buttons (they are within the console item)
        const closeButtons = screen.getAllByTestId('close-console-button');
        fireEvent.click(closeButtons[0]);
        
        expect(mockRemoveConsole).toHaveBeenCalledWith('c1');
    });

    it('shows empty state when no consoles', () => {
        vi.mocked(useTerminal).mockReturnValue({
            consoles: [],
            selectedConsoleId: null,
            selectConsole: mockSelectConsole,
            removeConsole: mockRemoveConsole,
            reorderConsoles: mockReorderConsoles,
            addConsole: vi.fn(),
            updateConsole: vi.fn(),
        } as unknown as ReturnType<typeof useTerminal>);

        render(<ConsoleList />);
        expect(screen.getByText('No active sessions')).toBeInTheDocument();
    });
});
