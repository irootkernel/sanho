import { describe, it, expect } from 'vitest';
import { filterAndSortWorkspaces, sortWorkspacesByPath } from './workspaceUtils';
import type { Workspace } from '../models/Workspace';

describe('filterAndSortWorkspaces', () => {
    const mockWorkspaces: Workspace[] = [
        {
            workspace_id: 'ws-1',
            project: 'proj-a',
            docs_repo_id: 'repo-1',
            docs_hash: 'hash-old',
            last_reported_at: '2025-01-01T10:00:00Z',
            last_actor_email: '',
            local_path: '/users/dev/ws-1',
            repo_url: 'git@github.com:org/repo1.git',
        },
        {
            workspace_id: 'ws-2',
            project: 'proj-a',
            docs_repo_id: 'repo-1',
            docs_hash: 'hash-new',
            last_reported_at: '2025-01-02T10:00:00Z',
            last_actor_email: 'dev@example.com',
            local_path: '/users/dev/ws-2',
            repo_url: 'git@github.com:org/repo2.git',
        },
        {
            workspace_id: 'ws-3',
            project: 'proj-a',
            docs_repo_id: 'repo-1',
            docs_hash: 'hash-old',
            last_reported_at: null, // No report yet
            last_actor_email: '',
            local_path: '/users/dev/ws-3',
            repo_url: '',
        },
    ];


    const DOCS_HEAD = 'hash-new';

    it('computes status correctly for all items', () => {
        const result = filterAndSortWorkspaces(
            mockWorkspaces,
            DOCS_HEAD,
            'all',
            '',
            { field: 'local_path', direction: 'asc' }
        );

        expect(result).toHaveLength(3);
        const ws1 = result.find((w) => w.workspace_id === 'ws-1');
        const ws2 = result.find((w) => w.workspace_id === 'ws-2');

        expect(ws1?.status).toBe('outdated'); // hash-old != hash-new
        expect(ws2?.status).toBe('up_to_date'); // hash-new == hash-new
    });

    it('filters by status', () => {
        const outdated = filterAndSortWorkspaces(
            mockWorkspaces,
            DOCS_HEAD,
            'outdated',
            '',
            { field: 'local_path', direction: 'asc' }
        );
        expect(outdated.map((w) => w.workspace_id)).toEqual(['ws-1', 'ws-3']);

        const upToDate = filterAndSortWorkspaces(
            mockWorkspaces,
            DOCS_HEAD,
            'up_to_date',
            '',
            { field: 'local_path', direction: 'asc' }
        );
        expect(upToDate.map((w) => w.workspace_id)).toEqual(['ws-2']);
    });

    it('filters by search query (id, path, url)', () => {
        // Search by ID
        const byId = filterAndSortWorkspaces(
            mockWorkspaces,
            DOCS_HEAD,
            'all',
            'ws-2',
            { field: 'local_path', direction: 'asc' }
        );
        expect(byId).toHaveLength(1);
        expect(byId[0].workspace_id).toBe('ws-2');

        // Search by path substring
        const byPath = filterAndSortWorkspaces(
            mockWorkspaces,
            DOCS_HEAD,
            'all',
            'ws-3',
            { field: 'local_path', direction: 'asc' }
        );
        expect(byPath).toHaveLength(1);
        expect(byPath[0].workspace_id).toBe('ws-3');

        // Search by repo url
        const byRepo = filterAndSortWorkspaces(
            mockWorkspaces,
            DOCS_HEAD,
            'all',
            'repo2.git',
            { field: 'local_path', direction: 'asc' }
        );
        expect(byRepo).toHaveLength(1);
        expect(byRepo[0].workspace_id).toBe('ws-2');
    });

    it('sorts by last_reported_at desc', () => {
        const result = filterAndSortWorkspaces(
            mockWorkspaces,
            DOCS_HEAD,
            'all',
            '',
            { field: 'last_reported_at', direction: 'desc' }
        );
        // ws-2 (Jan 2) > ws-1 (Jan 1) > ws-3 (null)
        expect(result.map((w) => w.workspace_id)).toEqual(['ws-2', 'ws-1', 'ws-3']);
    });

    it('sorts by local_path asc', () => {
        const result = filterAndSortWorkspaces(
            mockWorkspaces,
            DOCS_HEAD,
            'all',
            '',
            { field: 'local_path', direction: 'asc' }
        );
        // /users/dev/ws-1 < /users/dev/ws-2 < /users/dev/ws-3
        expect(result.map((w) => w.workspace_id)).toEqual(['ws-1', 'ws-2', 'ws-3']);
    });
});

describe('sortWorkspacesByPath', () => {
    const mockWorkspaces: Workspace[] = [
        {
            workspace_id: 'ws-b',
            project: 'proj',
            local_path: '/path/b',
            repo_url: '',
            docs_repo_id: '',
            docs_hash: '',
            last_reported_at: '',
            last_actor_email: '',
        },
        {
            workspace_id: 'ws-a',
            project: 'proj',
            local_path: '/path/a',
            repo_url: '',
            docs_repo_id: '',
            docs_hash: '',
            last_reported_at: '',
            last_actor_email: '',
        },
        {
            workspace_id: 'ws-c',
            project: 'proj',
            local_path: '/path/c',
            repo_url: '',
            docs_repo_id: '',
            docs_hash: '',
            last_reported_at: '',
            last_actor_email: '',
        },
    ];

    it('should sort workspaces by local_path ascending', () => {
        const sorted = sortWorkspacesByPath(mockWorkspaces);
        expect(sorted).toHaveLength(3);
        expect(sorted[0].local_path).toBe('/path/a');
        expect(sorted[1].local_path).toBe('/path/b');
        expect(sorted[2].local_path).toBe('/path/c');
    });

    it('should not mutate the original array', () => {
        const original = [...mockWorkspaces];
        const sorted = sortWorkspacesByPath(original);
        expect(original).toEqual(mockWorkspaces); // Should be unchanged
        expect(sorted).not.toBe(original); // Should return a new array
    });
});

