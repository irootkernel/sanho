import { describe, it, expect } from 'vitest';
import { computeWorkspaceStatus } from './computeWorkspaceStatus';
import type { Workspace } from '../models/Workspace';

describe('computeWorkspaceStatus', () => {
    const baseWorkspace: Workspace = {
        workspace_id: 'ws-1',
        project: 'project-a',
        docs_repo_id: 'docs-repo-1',
        docs_hash: 'hash-v1',
        last_reported_at: '2025-01-01T00:00:00Z',
        last_actor_email: 'actor@example.com',
        local_path: '/tmp/ws-1',
        repo_url: 'git@github.com:org/repo.git',
    };


    it('returns "unknown" if docsHead is null', () => {
        const result = computeWorkspaceStatus(baseWorkspace, null);
        expect(result).toBe('unknown');
    });

    it('returns "up_to_date" if docs_hash matches docsHead', () => {
        const result = computeWorkspaceStatus(baseWorkspace, 'hash-v1');
        expect(result).toBe('up_to_date');
    });

    it('returns "outdated" if docs_hash does not match docsHead', () => {
        const result = computeWorkspaceStatus(baseWorkspace, 'hash-v2');
        expect(result).toBe('outdated');
    });
});
