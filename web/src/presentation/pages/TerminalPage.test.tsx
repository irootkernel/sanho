import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { TerminalPage } from './TerminalPage';
import { useTerminal } from '@/application';

// Mock application hooks
vi.mock('@/application', () => ({
    useTerminal: vi.fn(),
    // We need to mock other things that might be used by children
    useKkachiState: vi.fn(() => ({ refresh: vi.fn() })),
}));

// Mock child components to isolate TerminalPage
vi.mock('../components/terminal/ConsoleList', () => ({
    ConsoleList: vi.fn(() => <div data-testid="console-list">Console List</div>),
}));

vi.mock('../components/terminal/TerminalPane', () => ({
    TerminalPane: vi.fn(({ consoleId }) => (
        <div data-testid={`terminal-pane-${consoleId || 'empty'}`}>
            Terminal {consoleId}
        </div>
    )),
}));

vi.mock('../components/terminal/WorkspacePickerModal', () => ({
    WorkspacePickerModal: vi.fn(() => <div data-testid="picker-modal">Picker Modal</div>),
}));

describe('TerminalPage', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('should render empty state when no consoles', () => {
        vi.mocked(useTerminal).mockReturnValue({
            consoles: [],
            selectedConsoleId: null,
            addConsole: vi.fn(),
            removeConsole: vi.fn(),
            selectConsole: vi.fn(),
            updateConsole: vi.fn(),
            reorderConsoles: vi.fn(),
        });

        render(<TerminalPage />);
        expect(screen.getByTestId('no-active-consoles')).toBeVisible();
    });

    it('should render multiple terminal panes but only one visible', () => {
        const mockConsoles = [
            { consoleId: 'id-1', title: 'Console 1', status: 'CONNECTED' as const, createdAt: Date.now() },
            { consoleId: 'id-2', title: 'Console 2', status: 'CONNECTED' as const, createdAt: Date.now() },
        ];

        vi.mocked(useTerminal).mockReturnValue({
            consoles: mockConsoles,
            selectedConsoleId: 'id-1',
            addConsole: vi.fn(),
            removeConsole: vi.fn(),
            selectConsole: vi.fn(),
            updateConsole: vi.fn(),
            reorderConsoles: vi.fn(),
        });

        const { getByTestId } = render(<TerminalPage />);

        const pane1 = getByTestId('terminal-pane-id-1').parentElement;
        const pane2 = getByTestId('terminal-pane-id-2').parentElement;

        expect(pane1).toHaveStyle({ display: 'flex' });
        expect(pane2).toHaveStyle({ display: 'none' });
    });
});
