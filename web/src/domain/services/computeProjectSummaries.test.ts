import { describe, it, expect } from 'vitest';
import {
    computeProjectSummaries,
    sortByOutdatedFirst,
} from './computeProjectSummaries';
import type { KkachiState } from '../models/KkachiState';

describe('computeProjectSummaries', () => {
    describe('Empty state handling', () => {
        it('should return empty array for empty state', () => {
            const state: KkachiState = {
                docs_heads: {},
                workspaces: [],
            };

            const result = computeProjectSummaries(state);

            expect(result).toEqual([]);
        });
    });

    describe('docs_heads only (no workspaces)', () => {
        it('should create project summary with zero counts when only docs_heads exist', () => {
            const state: KkachiState = {
                docs_heads: {
                    sudal: 'abc123',
                },
                workspaces: [],
            };

            const result = computeProjectSummaries(state);

            expect(result).toHaveLength(1);
            expect(result[0]).toEqual({
                project: 'sudal',
                docs_head: 'abc123',
                workspace_count: 0,
                unknown_count: 0,
                outdated_count: 0,
                last_reported_at_max: null,
            });
        });
    });

    describe('workspaces only (no docs_heads)', () => {
        it('should mark all workspaces as unknown when docs_head is missing', () => {
            const state: KkachiState = {
                docs_heads: {},
                workspaces: [
                    {
                        workspace_id: 'ws-001',
                        project: 'sudal',
                        docs_repo_id: 'docs-sudal',
                        local_path: '/path/to/sudal',
                        repo_url: 'https://github.com/example/sudal',
                        docs_hash: 'abc123',
                        last_reported_at: '2024-12-14T10:00:00Z',
                        last_actor_email: 'dev@example.com',
                    },
                    {
                        workspace_id: 'ws-002',
                        project: 'sudal',
                        docs_repo_id: 'docs-sudal',
                        local_path: '/path/to/sudal2',
                        repo_url: 'https://github.com/example/sudal2',
                        docs_hash: 'def456',
                        last_reported_at: '2024-12-13T15:00:00Z',
                        last_actor_email: 'dev2@example.com',
                    },
                ],
            };

            const result = computeProjectSummaries(state);

            expect(result).toHaveLength(1);
            expect(result[0]).toEqual({
                project: 'sudal',
                docs_head: null,
                workspace_count: 2,
                unknown_count: 2,
                outdated_count: 0,
                last_reported_at_max: '2024-12-14T10:00:00Z',
            });
        });
    });

    describe('outdated count calculation', () => {
        it('should count workspaces where docs_hash differs from docs_head', () => {
            const state: KkachiState = {
                docs_heads: {
                    sudal: 'latest-hash',
                },
                workspaces: [
                    {
                        workspace_id: 'ws-001',
                        project: 'sudal',
                        docs_repo_id: 'docs-sudal',
                        local_path: '/path/to/sudal1',
                        repo_url: 'https://github.com/example/sudal',
                        docs_hash: 'latest-hash', // up-to-date
                        last_reported_at: '2024-12-14T10:00:00Z',
                        last_actor_email: 'dev@example.com',
                    },
                    {
                        workspace_id: 'ws-002',
                        project: 'sudal',
                        docs_repo_id: 'docs-sudal',
                        local_path: '/path/to/sudal2',
                        repo_url: 'https://github.com/example/sudal2',
                        docs_hash: 'old-hash', // outdated
                        last_reported_at: '2024-12-13T15:00:00Z',
                        last_actor_email: 'dev2@example.com',
                    },
                    {
                        workspace_id: 'ws-003',
                        project: 'sudal',
                        docs_repo_id: 'docs-sudal',
                        local_path: '/path/to/sudal3',
                        repo_url: 'https://github.com/example/sudal3',
                        docs_hash: 'another-old-hash', // outdated
                        last_reported_at: '2024-12-12T10:00:00Z',
                        last_actor_email: 'dev3@example.com',
                    },
                ],
            };

            const result = computeProjectSummaries(state);

            expect(result).toHaveLength(1);
            expect(result[0].outdated_count).toBe(2);
            expect(result[0].unknown_count).toBe(0);
        });

        it('should have zero outdated when all workspaces match docs_head', () => {
            const state: KkachiState = {
                docs_heads: {
                    kkachi: 'abc123',
                },
                workspaces: [
                    {
                        workspace_id: 'ws-001',
                        project: 'kkachi',
                        docs_repo_id: 'docs-kkachi',
                        local_path: '/path/to/kkachi',
                        repo_url: 'https://github.com/example/kkachi',
                        docs_hash: 'abc123',
                        last_reported_at: '2024-12-14T10:00:00Z',
                        last_actor_email: 'dev@example.com',
                    },
                ],
            };

            const result = computeProjectSummaries(state);

            expect(result[0].outdated_count).toBe(0);
        });
    });

    describe('last_reported_at_max calculation', () => {
        it('should return the most recent last_reported_at', () => {
            const state: KkachiState = {
                docs_heads: {
                    sudal: 'abc123',
                },
                workspaces: [
                    {
                        workspace_id: 'ws-001',
                        project: 'sudal',
                        docs_repo_id: 'docs-sudal',
                        local_path: '/path/1',
                        repo_url: 'https://github.com/example/sudal',
                        docs_hash: 'abc123',
                        last_reported_at: '2024-12-10T10:00:00Z',
                        last_actor_email: 'dev@example.com',
                    },
                    {
                        workspace_id: 'ws-002',
                        project: 'sudal',
                        docs_repo_id: 'docs-sudal',
                        local_path: '/path/2',
                        repo_url: 'https://github.com/example/sudal',
                        docs_hash: 'abc123',
                        last_reported_at: '2024-12-14T15:30:00Z', // most recent
                        last_actor_email: 'dev2@example.com',
                    },
                    {
                        workspace_id: 'ws-003',
                        project: 'sudal',
                        docs_repo_id: 'docs-sudal',
                        local_path: '/path/3',
                        repo_url: 'https://github.com/example/sudal',
                        docs_hash: 'abc123',
                        last_reported_at: '2024-12-12T08:00:00Z',
                        last_actor_email: 'dev3@example.com',
                    },
                ],
            };

            const result = computeProjectSummaries(state);

            expect(result[0].last_reported_at_max).toBe('2024-12-14T15:30:00Z');
        });

        it('should handle null last_reported_at values', () => {
            const state: KkachiState = {
                docs_heads: {
                    sudal: 'abc123',
                },
                workspaces: [
                    {
                        workspace_id: 'ws-001',
                        project: 'sudal',
                        docs_repo_id: 'docs-sudal',
                        local_path: '/path/1',
                        repo_url: 'https://github.com/example/sudal',
                        docs_hash: 'abc123',
                        last_reported_at: null,
                        last_actor_email: 'dev@example.com',
                    },
                    {
                        workspace_id: 'ws-002',
                        project: 'sudal',
                        docs_repo_id: 'docs-sudal',
                        local_path: '/path/2',
                        repo_url: 'https://github.com/example/sudal',
                        docs_hash: 'abc123',
                        last_reported_at: '2024-12-14T10:00:00Z',
                        last_actor_email: 'dev2@example.com',
                    },
                ],
            };

            const result = computeProjectSummaries(state);

            expect(result[0].last_reported_at_max).toBe('2024-12-14T10:00:00Z');
        });

        it('should return null when all workspaces have null last_reported_at', () => {
            const state: KkachiState = {
                docs_heads: {
                    sudal: 'abc123',
                },
                workspaces: [
                    {
                        workspace_id: 'ws-001',
                        project: 'sudal',
                        docs_repo_id: 'docs-sudal',
                        local_path: '/path/1',
                        repo_url: 'https://github.com/example/sudal',
                        docs_hash: 'abc123',
                        last_reported_at: null,
                        last_actor_email: 'dev@example.com',
                    },
                ],
            };

            const result = computeProjectSummaries(state);

            expect(result[0].last_reported_at_max).toBeNull();
        });
    });

    describe('multiple projects', () => {
        it('should handle multiple projects correctly', () => {
            const state: KkachiState = {
                docs_heads: {
                    sudal: 'sudal-head',
                    kkachi: 'kkachi-head',
                },
                workspaces: [
                    {
                        workspace_id: 'ws-001',
                        project: 'sudal',
                        docs_repo_id: 'docs-sudal',
                        local_path: '/path/sudal',
                        repo_url: 'https://github.com/example/sudal',
                        docs_hash: 'sudal-head',
                        last_reported_at: '2024-12-14T10:00:00Z',
                        last_actor_email: 'dev@example.com',
                    },
                    {
                        workspace_id: 'ws-002',
                        project: 'kkachi',
                        docs_repo_id: 'docs-kkachi',
                        local_path: '/path/kkachi',
                        repo_url: 'https://github.com/example/kkachi',
                        docs_hash: 'old-hash', // outdated
                        last_reported_at: '2024-12-13T15:00:00Z',
                        last_actor_email: 'dev2@example.com',
                    },
                ],
            };

            const result = computeProjectSummaries(state);

            expect(result).toHaveLength(2);

            // Should be sorted by project name
            expect(result[0].project).toBe('kkachi');
            expect(result[0].outdated_count).toBe(1);

            expect(result[1].project).toBe('sudal');
            expect(result[1].outdated_count).toBe(0);
        });

        it('should sort results by project name ascending', () => {
            const state: KkachiState = {
                docs_heads: {
                    zebra: 'z-head',
                    alpha: 'a-head',
                    middle: 'm-head',
                },
                workspaces: [],
            };

            const result = computeProjectSummaries(state);

            expect(result.map((s) => s.project)).toEqual([
                'alpha',
                'middle',
                'zebra',
            ]);
        });
    });

    describe('mixed projects (some in docs_heads only, some in workspaces only)', () => {
        it('should include projects from both sources', () => {
            const state: KkachiState = {
                docs_heads: {
                    // Only in docs_heads
                    'docs-only-project': 'abc123',
                },
                workspaces: [
                    {
                        // Only in workspaces
                        workspace_id: 'ws-001',
                        project: 'workspace-only-project',
                        docs_repo_id: 'docs-wop',
                        local_path: '/path/wop',
                        repo_url: 'https://github.com/example/wop',
                        docs_hash: 'xyz789',
                        last_reported_at: '2024-12-14T10:00:00Z',
                        last_actor_email: 'dev@example.com',
                    },
                ],
            };

            const result = computeProjectSummaries(state);

            expect(result).toHaveLength(2);

            const docsOnlyProject = result.find(
                (s) => s.project === 'docs-only-project'
            );
            const workspaceOnlyProject = result.find(
                (s) => s.project === 'workspace-only-project'
            );

            expect(docsOnlyProject).toBeDefined();
            expect(docsOnlyProject?.workspace_count).toBe(0);
            expect(docsOnlyProject?.docs_head).toBe('abc123');

            expect(workspaceOnlyProject).toBeDefined();
            expect(workspaceOnlyProject?.workspace_count).toBe(1);
            expect(workspaceOnlyProject?.docs_head).toBeNull();
            expect(workspaceOnlyProject?.unknown_count).toBe(1);
        });
    });
});

describe('sortByOutdatedFirst', () => {
    it('should sort projects with outdated workspaces first', () => {
        const summaries = [
            {
                project: 'clean-project',
                docs_head: 'abc',
                workspace_count: 5,
                unknown_count: 0,
                outdated_count: 0,
                last_reported_at_max: null,
            },
            {
                project: 'dirty-project',
                docs_head: 'def',
                workspace_count: 3,
                unknown_count: 0,
                outdated_count: 2,
                last_reported_at_max: null,
            },
        ];

        const result = sortByOutdatedFirst(summaries);

        expect(result[0].project).toBe('dirty-project');
        expect(result[1].project).toBe('clean-project');
    });

    it('should sort by outdated count descending within outdated projects', () => {
        const summaries = [
            {
                project: 'moderate-outdated',
                docs_head: 'abc',
                workspace_count: 5,
                unknown_count: 0,
                outdated_count: 2,
                last_reported_at_max: null,
            },
            {
                project: 'highly-outdated',
                docs_head: 'def',
                workspace_count: 10,
                unknown_count: 0,
                outdated_count: 8,
                last_reported_at_max: null,
            },
            {
                project: 'slightly-outdated',
                docs_head: 'ghi',
                workspace_count: 3,
                unknown_count: 0,
                outdated_count: 1,
                last_reported_at_max: null,
            },
        ];

        const result = sortByOutdatedFirst(summaries);

        expect(result[0].project).toBe('highly-outdated');
        expect(result[1].project).toBe('moderate-outdated');
        expect(result[2].project).toBe('slightly-outdated');
    });

    it('should sort by project name when outdated counts are equal', () => {
        const summaries = [
            {
                project: 'zebra',
                docs_head: 'abc',
                workspace_count: 5,
                unknown_count: 0,
                outdated_count: 2,
                last_reported_at_max: null,
            },
            {
                project: 'alpha',
                docs_head: 'def',
                workspace_count: 3,
                unknown_count: 0,
                outdated_count: 2,
                last_reported_at_max: null,
            },
        ];

        const result = sortByOutdatedFirst(summaries);

        expect(result[0].project).toBe('alpha');
        expect(result[1].project).toBe('zebra');
    });

    it('should not mutate original array', () => {
        const summaries = [
            {
                project: 'b',
                docs_head: 'abc',
                workspace_count: 1,
                unknown_count: 0,
                outdated_count: 0,
                last_reported_at_max: null,
            },
            {
                project: 'a',
                docs_head: 'def',
                workspace_count: 1,
                unknown_count: 0,
                outdated_count: 1,
                last_reported_at_max: null,
            },
        ];

        const original = [...summaries];
        sortByOutdatedFirst(summaries);

        expect(summaries).toEqual(original);
    });
});
