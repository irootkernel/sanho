import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { TerminalPane } from './TerminalPane';
import { useTerminal } from '@/application';

// Mock application hooks and API
vi.mock('@/application', () => ({
    useTerminal: vi.fn(),
}));

vi.mock('@/api/pty', () => ({
    terminateSession: vi.fn(),
    connectConsole: vi.fn(() => ({
        send: vi.fn(),
        close: vi.fn(),
        readyState: 1, // OPEN
    })),
}));

// Mock xterm
vi.mock('@xterm/xterm', () => {
    class Terminal {
        loadAddon = vi.fn();
        open = vi.fn();
        write = vi.fn();
        writeln = vi.fn();
        onData = vi.fn(() => ({ dispose: vi.fn() }));
        dispose = vi.fn();
        focus = vi.fn();
        cols = 80;
        rows = 24;
    }
    return { Terminal };
});

// Mock ResizeObserver properly using a class
class MockResizeObserver {
    observe = vi.fn();
    unobserve = vi.fn();
    disconnect = vi.fn();
}
vi.stubGlobal('ResizeObserver', MockResizeObserver);

vi.mock('@xterm/addon-fit', () => {
    class FitAddon {
        fit = vi.fn();
    }
    return { FitAddon };
});

vi.mock('@xterm/addon-web-links', () => {
    class WebLinksAddon { }
    return { WebLinksAddon };
});

describe('TerminalPane', () => {
    const mockUpdateConsole = vi.fn();
    const mockRemoveConsole = vi.fn();

    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(useTerminal).mockReturnValue({
            consoles: [],
            selectedConsoleId: null,
            addConsole: vi.fn(),
            removeConsole: mockRemoveConsole,
            selectConsole: vi.fn(),
            updateConsole: mockUpdateConsole,
            reorderConsoles: vi.fn(),
        });
    });

    it('should render nothing when no console is provided', () => {
        // @ts-expect-error - testing missing console
        const { container } = render(<TerminalPane console={undefined} isSelected={false} />);
        expect(container.firstChild).toBeNull();
    });

    it('should render terminal toolbar when a console is selected', () => {
        const mockConsole = {
            consoleId: 'console-1',
            title: 'Test Console',
            status: 'CONNECTED' as const,
            createdAt: Date.now(),
            workspaceId: 'ws-1',
            project: 'Test Project',
        };

        vi.mocked(useTerminal).mockReturnValue({
            consoles: [mockConsole],
            selectedConsoleId: 'console-1',
            addConsole: vi.fn(),
            removeConsole: mockRemoveConsole,
            selectConsole: vi.fn(),
            updateConsole: mockUpdateConsole,
            reorderConsoles: vi.fn(),
        } as unknown as ReturnType<typeof useTerminal>);

        render(<TerminalPane console={mockConsole} isSelected={true} />);
        expect(screen.getByText(/Test Console/i)).toBeInTheDocument();
    });
});
