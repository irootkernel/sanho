import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { WorkspacePickerModal } from './WorkspacePickerModal';
import * as StateApi from '@/api/state';

// Mock the API module
vi.mock('@/api/state', () => ({
    fetchWorkspaces: vi.fn(),
}));

const mockWorkspaces: StateApi.Workspace[] = [
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
    });

    it('should not render when isOpen is false', () => {
        render(<WorkspacePickerModal isOpen={false} onClose={onClose} onSelect={onSelect} />);
        expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });

    it('should render and load workspaces when isOpen is true', async () => {
        render(<WorkspacePickerModal isOpen={true} onClose={onClose} onSelect={onSelect} />);
        
        expect(screen.getByRole('dialog')).toBeInTheDocument();
        expect(screen.getByText('Loading...')).toBeInTheDocument();

        await waitFor(() => {
            expect(screen.getByText('Project A')).toBeInTheDocument();
            expect(screen.getByText('Project B')).toBeInTheDocument();
        });
    });

    it('should filter workspaces by search query', async () => {
        const user = userEvent.setup();
        render(<WorkspacePickerModal isOpen={true} onClose={onClose} onSelect={onSelect} />);

        await waitFor(() => {
            expect(screen.getByText('Project A')).toBeInTheDocument();
        });

        const input = screen.getByPlaceholderText(/Search/i);
        await user.type(input, 'Project B');

        expect(screen.queryByText('Project A')).not.toBeInTheDocument();
        expect(screen.getByText('Project B')).toBeInTheDocument();
    });

    it('should call onSelect when a workspace is clicked', async () => {
        const user = userEvent.setup();
        render(<WorkspacePickerModal isOpen={true} onClose={onClose} onSelect={onSelect} />);

        await waitFor(() => {
            expect(screen.getByText('Project A')).toBeInTheDocument();
        });

        await user.click(screen.getByText('Project A'));

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

    it('should display error message if fetch fails', async () => {
        vi.mocked(StateApi.fetchWorkspaces).mockRejectedValue(new Error('API Error'));
        
        render(<WorkspacePickerModal isOpen={true} onClose={onClose} onSelect={onSelect} />);

        await waitFor(() => {
            expect(screen.getByText('API Error')).toBeInTheDocument();
            expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
        });
    });
});
