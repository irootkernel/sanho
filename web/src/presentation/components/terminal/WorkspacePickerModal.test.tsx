import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { WorkspacePickerModal } from './WorkspacePickerModal';
import * as StateApi from '@/api/state';
import { useTerminal } from '@/application';

// Mock the API module
vi.mock('@/api/state', () => ({
    fetchWorkspaces: vi.fn(),
}));

// Mock application hooks
vi.mock('@/application', () => ({
    useTerminal: vi.fn(),
}));

const mockWorkspaces: StateApi.Workspace[] = [
// ... (rest of mock data remains same)
    {
        workspace_id: 'ws-1',
        project: 'Project A',
        docs_repo_id: 'repo-1',
        local_path: '/path/to/project-a',
        repo_url: 'http://repo.url',
        docs_hash: 'hash1',
        last_actor_email: 'user@example.com',
    },
    {
        workspace_id: 'ws-2',
        project: 'Project B',
        docs_repo_id: 'repo-2',
        local_path: '/path/to/project-b',
        repo_url: 'http://repo.url',
        docs_hash: 'hash2',
        last_actor_email: 'user@example.com',
    },
];

describe('WorkspacePickerModal', () => {
    const onClose = vi.fn();
    const onSelect = vi.fn();

    beforeEach(() => {
        vi.clearAllMocks();
        onClose.mockClear();
        onSelect.mockClear();
        vi.mocked(StateApi.fetchWorkspaces).mockResolvedValue(mockWorkspaces);
        vi.mocked(useTerminal).mockReturnValue({
            consoles: [],
            selectedConsoleId: null,
            addConsole: vi.fn(),
            removeConsole: vi.fn(),
            selectConsole: vi.fn(),
            updateConsole: vi.fn(),
            reorderConsoles: vi.fn(),
        });
    });

    it('should not render when isOpen is false', () => {
        render(<WorkspacePickerModal isOpen={false} onClose={onClose} onSelect={onSelect} />);
        expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });

    it('should render and load workspaces when isOpen is true', async () => {
        render(<WorkspacePickerModal isOpen={true} onClose={onClose} onSelect={onSelect} />);
        
        expect(screen.getByRole('dialog')).toBeInTheDocument();
        expect(screen.getByText(/Loading/i)).toBeInTheDocument();

        await waitFor(() => {
            expect(screen.getByText(/Project A/i)).toBeInTheDocument();
            expect(screen.getByText(/Project B/i)).toBeInTheDocument();
        });
    });

    it('should filter workspaces by search query', async () => {
        const user = userEvent.setup();
        render(<WorkspacePickerModal isOpen={true} onClose={onClose} onSelect={onSelect} />);

        await waitFor(() => {
            expect(screen.getByText(/Project A/i)).toBeInTheDocument();
        });

        const input = screen.getByPlaceholderText(/Search/i);
        await user.type(input, 'Project B');

        expect(screen.queryByText(/Project A/i)).not.toBeInTheDocument();
        expect(screen.getByText(/Project B/i)).toBeInTheDocument();
    });

    it('should call onSelect when a workspace is clicked', async () => {
        const user = userEvent.setup();
        render(<WorkspacePickerModal isOpen={true} onClose={onClose} onSelect={onSelect} />);

        await waitFor(() => {
            expect(screen.getByText('ws-1')).toBeInTheDocument();
        });

        await user.click(screen.getByText('ws-1'));

        expect(onSelect).toHaveBeenCalledWith(mockWorkspaces[0]);
    });

    it('should call onClose when Cancel button is clicked', async () => {
        const user = userEvent.setup();
        render(<WorkspacePickerModal isOpen={true} onClose={onClose} onSelect={onSelect} />);

        await waitFor(() => {
            expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: 'Cancel' }));

        expect(onClose).toHaveBeenCalled();
    });

    it('should navigate and select using keyboard', async () => {
        const user = userEvent.setup();
        render(<WorkspacePickerModal isOpen={true} onClose={onClose} onSelect={onSelect} />);

        await waitFor(() => {
            expect(screen.getByText('ws-1')).toBeInTheDocument();
        });

        // Navigate down to second item
        await user.keyboard('{ArrowDown}');
        // Press Enter to select
        await user.keyboard('{Enter}');

        expect(onSelect).toHaveBeenCalledWith(mockWorkspaces[1]);
    });

    it('should display error message if fetch fails', async () => {
        vi.mocked(StateApi.fetchWorkspaces).mockRejectedValue(new Error('API Error'));
        
        render(<WorkspacePickerModal isOpen={true} onClose={onClose} onSelect={onSelect} />);

        await waitFor(() => {
            expect(screen.getByText('API Error')).toBeInTheDocument();
            expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
        });
    });
});
