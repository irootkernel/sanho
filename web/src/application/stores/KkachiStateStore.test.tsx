import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import { renderHook } from '@testing-library/react';
import { useKkachiState } from './useKkachiState';
import { KkachiStateStoreProvider } from './KkachiStateStoreProvider';
import { useKkachiStateStore } from './useKkachiStateStore';
import type { KkachiState } from '@/domain';
import type { KkachiStateRepository } from '../ports/KkachiStateRepository';

const mockState: KkachiState = {
    docs_heads: { project1: 'abc123' },
    workspaces: [
        {
            workspace_id: 'ws-1',
            project: 'project1',
            docs_repo_id: 'docs-1',
            local_path: '/path/to/ws',
            repo_url: 'https://example.com/repo',
            docs_hash: 'abc123',
            last_reported_at: '2024-12-15T10:00:00Z',
            last_actor_email: 'user@example.com',
        },
    ],
};

function createMockRepository(
    getStateImpl: () => Promise<KkachiState> = async () => mockState
): KkachiStateRepository {
    return {
        getState: vi.fn(getStateImpl),
    };
}

describe('useKkachiStateStore', () => {
    it('should start with loading state', () => {
        const repository = createMockRepository(
            () => new Promise(() => { }) // Never resolves
        );

        const { result } = renderHook(() => useKkachiStateStore(repository));

        expect(result.current.isLoading).toBe(true);
        expect(result.current.data).toBeNull();
        expect(result.current.error).toBeNull();
        expect(result.current.isInitialized).toBe(false);
    });

    it('should fetch data on mount', async () => {
        const repository = createMockRepository();

        const { result } = renderHook(() => useKkachiStateStore(repository));

        await waitFor(() => {
            expect(result.current.isInitialized).toBe(true);
        });

        expect(result.current.data).toEqual(mockState);
        expect(result.current.isLoading).toBe(false);
        expect(result.current.error).toBeNull();
        expect(repository.getState).toHaveBeenCalledTimes(1);
    });

    it('should set error on fetch failure', async () => {
        const testError = new Error('Network error');
        const repository = createMockRepository(async () => {
            throw testError;
        });

        const { result } = renderHook(() => useKkachiStateStore(repository));

        await waitFor(() => {
            expect(result.current.isInitialized).toBe(true);
        });

        expect(result.current.error).toEqual(testError);
        expect(result.current.data).toBeNull();
        expect(result.current.isLoading).toBe(false);
    });

    it('should refetch on refresh call', async () => {
        const repository = createMockRepository();

        const { result } = renderHook(() => useKkachiStateStore(repository));

        await waitFor(() => {
            expect(result.current.isInitialized).toBe(true);
        });

        expect(repository.getState).toHaveBeenCalledTimes(1);

        // Call refresh
        await act(async () => {
            await result.current.refresh();
        });

        expect(repository.getState).toHaveBeenCalledTimes(2);
    });
});

describe('KkachiStateStoreProvider', () => {
    it('should provide store to children', async () => {
        const repository = createMockRepository();

        function TestComponent() {
            const store = useKkachiState();
            return <div data-testid="data">{store.data ? 'loaded' : 'empty'}</div>;
        }

        render(
            <KkachiStateStoreProvider repository={repository}>
                <TestComponent />
            </KkachiStateStoreProvider>
        );

        await waitFor(() => {
            expect(screen.getByTestId('data')).toHaveTextContent('loaded');
        });
    });
});

describe('useKkachiState', () => {
    it('should throw error when used outside provider', () => {
        // Suppress console.error for this test
        const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => { });

        function TestComponent() {
            useKkachiState();
            return null;
        }

        expect(() => render(<TestComponent />)).toThrow(
            'useKkachiState must be used within a KkachiStateStoreProvider'
        );

        consoleSpy.mockRestore();
    });
});
