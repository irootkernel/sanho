import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { ProjectsPage } from './ProjectsPage';
import { RuntimeProvider } from '@/app/di/RuntimeContext';
import type { KkachiState } from '@/domain';

// Helper to render ProjectsPage with necessary providers
function renderProjectsPage() {
    return render(
        <MemoryRouter initialEntries={['/']}>
            <RuntimeProvider>
                <Routes>
                    <Route path="/" element={<ProjectsPage />} />
                    <Route
                        path="/projects/:projectName"
                        element={<div data-testid="detail-page">Detail Page</div>}
                    />
                </Routes>
            </RuntimeProvider>
        </MemoryRouter>
    );
}

// Sample state with multiple projects
const sampleState: KkachiState = {
    docs_heads: {
        sudal: 'abc123def456',
        kkachi: '789ghi012jkl',
    },
    workspaces: [
        {
            workspace_id: 'ws-001',
            project: 'sudal',
            docs_repo_id: 'docs-sudal',
            local_path: '/Users/dev/projects/sudal',
            repo_url: 'https://github.com/example/sudal',
            docs_hash: 'abc123def456', // up-to-date
            last_reported_at: '2024-12-14T10:00:00Z',
            last_actor_email: 'dev@example.com',
        },
        {
            workspace_id: 'ws-002',
            project: 'sudal',
            docs_repo_id: 'docs-sudal',
            local_path: '/Users/dev2/projects/sudal',
            repo_url: 'https://github.com/example/sudal-fork',
            docs_hash: 'old-hash-123', // outdated
            last_reported_at: '2024-12-13T15:30:00Z',
            last_actor_email: 'dev2@example.com',
        },
        {
            workspace_id: 'ws-003',
            project: 'kkachi',
            docs_repo_id: 'docs-kkachi',
            local_path: '/Users/dev/projects/kkachi',
            repo_url: 'https://github.com/example/kkachi',
            docs_hash: '789ghi012jkl', // up-to-date
            last_reported_at: null,
            last_actor_email: 'dev@example.com',
        },
    ],
};

describe('ProjectsPage', () => {
    beforeEach(() => {
        vi.resetAllMocks();
    });

    afterEach(() => {
        vi.unstubAllGlobals();
    });

    describe('Loading state', () => {
        it('should show loading spinner initially', () => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockImplementation(() => new Promise(() => { }))
            );

            renderProjectsPage();

            expect(screen.getByText(/Loading projects.../i)).toBeInTheDocument();
        });

        it('should show Projects Dashboard heading while loading', () => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockImplementation(() => new Promise(() => { }))
            );

            renderProjectsPage();

            expect(
                screen.getByRole('heading', { name: /Projects Dashboard/i })
            ).toBeInTheDocument();
        });
    });

    describe('Success state', () => {
        beforeEach(() => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockResolvedValue({
                    ok: true,
                    json: () => Promise.resolve(sampleState),
                })
            );
        });

        it('should display project table with summaries', async () => {
            renderProjectsPage();

            await waitFor(() => {
                expect(screen.getByText('sudal')).toBeInTheDocument();
                expect(screen.getByText('kkachi')).toBeInTheDocument();
            });
        });

        it('should display docs HEAD hash', async () => {
            renderProjectsPage();

            await waitFor(() => {
                expect(screen.getByText('abc123de...')).toBeInTheDocument();
            });
        });

        it('should display workspace count', async () => {
            renderProjectsPage();

            await waitFor(() => {
                // sudal has 2 workspaces
                const cells = screen.getAllByRole('cell');
                const workspaceCountCells = cells.filter(
                    (cell) => cell.textContent === '2' || cell.textContent === '1'
                );
                expect(workspaceCountCells.length).toBeGreaterThanOrEqual(2);
            });
        });

        it('should display outdated status for projects with outdated workspaces', async () => {
            renderProjectsPage();

            await waitFor(() => {
                expect(screen.getByText(/1 Outdated/i)).toBeInTheDocument();
            });
        });

        it('should display up-to-date status for projects without outdated workspaces', async () => {
            renderProjectsPage();

            await waitFor(() => {
                expect(screen.getByText(/✓ Up-to-date/i)).toBeInTheDocument();
            });
        });

        it('should display sort toggle button', async () => {
            renderProjectsPage();

            await waitFor(() => {
                expect(
                    screen.getByRole('button', { name: /Sort by Name/i })
                ).toBeInTheDocument();
            });
        });

        it('should display refresh button', async () => {
            renderProjectsPage();

            await waitFor(() => {
                expect(
                    screen.getByRole('button', { name: /🔄 Refresh/i })
                ).toBeInTheDocument();
            });
        });
    });

    describe('Sort toggle', () => {
        beforeEach(() => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockResolvedValue({
                    ok: true,
                    json: () => Promise.resolve(sampleState),
                })
            );
        });

        it('should toggle to outdated first when clicked', async () => {
            const user = userEvent.setup();
            renderProjectsPage();

            await waitFor(() => {
                expect(screen.getByText('sudal')).toBeInTheDocument();
            });

            // Initially sorted by name (kkachi first)
            const rows = screen.getAllByRole('row');
            expect(rows[1].textContent).toContain('kkachi');

            // Click sort toggle
            const sortButton = screen.getByRole('button', {
                name: /Sort by Name/i,
            });
            await user.click(sortButton);

            // Now should be sorted by outdated first (sudal has 1 outdated)
            await waitFor(() => {
                const updatedRows = screen.getAllByRole('row');
                expect(updatedRows[1].textContent).toContain('sudal');
            });

            // Button text should change
            expect(
                screen.getByRole('button', { name: /Outdated First/i })
            ).toBeInTheDocument();
        });
    });

    describe('Row click navigation', () => {
        beforeEach(() => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockResolvedValue({
                    ok: true,
                    json: () => Promise.resolve(sampleState),
                })
            );
        });

        it('should navigate to project detail page when row is clicked', async () => {
            const user = userEvent.setup();
            renderProjectsPage();

            await waitFor(() => {
                expect(screen.getByText('sudal')).toBeInTheDocument();
            });

            // Click on sudal row
            const sudalRow = screen.getByText('sudal').closest('tr')!;
            await user.click(sudalRow);

            // Should navigate to detail page
            await waitFor(() => {
                expect(screen.getByTestId('detail-page')).toBeInTheDocument();
            });
        });

        it('should navigate when Enter key is pressed on row', async () => {
            const user = userEvent.setup();
            renderProjectsPage();

            await waitFor(() => {
                expect(screen.getByText('sudal')).toBeInTheDocument();
            });

            // Focus and press Enter on sudal row
            const sudalRow = screen.getByText('sudal').closest('tr')!;
            sudalRow.focus();
            await user.keyboard('{Enter}');

            // Should navigate to detail page
            await waitFor(() => {
                expect(screen.getByTestId('detail-page')).toBeInTheDocument();
            });
        });
    });

    describe('Refresh functionality', () => {
        it('should refetch data when refresh button is clicked', async () => {
            const user = userEvent.setup();
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve(sampleState),
            });
            vi.stubGlobal('fetch', mockFetch);

            renderProjectsPage();

            await waitFor(() => {
                expect(screen.getByText('sudal')).toBeInTheDocument();
            });

            // Click refresh button
            const refreshButton = screen.getByRole('button', {
                name: /🔄 Refresh/i,
            });
            await user.click(refreshButton);

            // Fetch should be called again
            await waitFor(() => {
                expect(mockFetch).toHaveBeenCalledTimes(2);
            });
        });
    });

    describe('Empty state', () => {
        it('should display empty state when no projects or workspaces', async () => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockResolvedValue({
                    ok: true,
                    json: () => Promise.resolve({ docs_heads: {}, workspaces: [] }),
                })
            );

            renderProjectsPage();

            await waitFor(() => {
                expect(screen.getByText(/No Projects Yet/i)).toBeInTheDocument();
                expect(
                    screen.getByText(/kkachi-server has no registered/i)
                ).toBeInTheDocument();
            });
        });

        it('should display CLI hint in empty state', async () => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockResolvedValue({
                    ok: true,
                    json: () => Promise.resolve({ docs_heads: {}, workspaces: [] }),
                })
            );

            renderProjectsPage();

            await waitFor(() => {
                expect(screen.getByText(/kkachi init/i)).toBeInTheDocument();
            });
        });
    });

    describe('Warning banner for docs_head only projects', () => {
        it('should display warning when project has docs_head but no workspaces', async () => {
            const stateWithDocsOnly: KkachiState = {
                docs_heads: {
                    orphan: 'orphan-hash',
                },
                workspaces: [
                    {
                        workspace_id: 'ws-001',
                        project: 'active',
                        docs_repo_id: 'docs-active',
                        local_path: '/path/active',
                        repo_url: 'https://github.com/example/active',
                        docs_hash: 'some-hash',
                        last_reported_at: '2024-12-14T10:00:00Z',
                        last_actor_email: 'dev@example.com',
                    },
                ],
            };

            vi.stubGlobal(
                'fetch',
                vi.fn().mockResolvedValue({
                    ok: true,
                    json: () => Promise.resolve(stateWithDocsOnly),
                })
            );

            renderProjectsPage();

            await waitFor(() => {
                expect(
                    screen.getByText(/1 project\(s\) have docs HEAD but no registered workspaces/i)
                ).toBeInTheDocument();
            });
        });
    });

    describe('Error state - Network Error', () => {
        beforeEach(() => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockRejectedValue(new Error('Network error'))
            );
        });

        it('should display error message', async () => {
            renderProjectsPage();

            await waitFor(() => {
                expect(
                    screen.getByText(/Failed to Load Projects/i)
                ).toBeInTheDocument();
            });
        });

        it('should display retry button', async () => {
            renderProjectsPage();

            await waitFor(() => {
                expect(
                    screen.getByRole('button', { name: /🔄 Retry/i })
                ).toBeInTheDocument();
            });
        });

        it('should refetch when retry button is clicked', async () => {
            const user = userEvent.setup();
            const mockFetch = vi
                .fn()
                .mockRejectedValueOnce(new Error('Network error'))
                .mockResolvedValueOnce({
                    ok: true,
                    json: () => Promise.resolve(sampleState),
                });
            vi.stubGlobal('fetch', mockFetch);

            renderProjectsPage();

            // Wait for error state
            await waitFor(() => {
                expect(
                    screen.getByText(/Failed to Load Projects/i)
                ).toBeInTheDocument();
            });

            // Click retry button
            const retryButton = screen.getByRole('button', { name: /🔄 Retry/i });
            await user.click(retryButton);

            // Should now show success state
            await waitFor(() => {
                expect(screen.getByText('sudal')).toBeInTheDocument();
            });
        });
    });

    describe('Accessibility', () => {
        beforeEach(() => {
            vi.stubGlobal(
                'fetch',
                vi.fn().mockResolvedValue({
                    ok: true,
                    json: () => Promise.resolve(sampleState),
                })
            );
        });

        it('should have proper heading hierarchy', async () => {
            renderProjectsPage();

            await waitFor(() => {
                const heading = screen.getByRole('heading', { level: 2 });
                expect(heading).toHaveTextContent('Projects Dashboard');
            });
        });

        it('should have accessible table structure', async () => {
            renderProjectsPage();

            await waitFor(() => {
                expect(screen.getByRole('table')).toBeInTheDocument();
                // Check for column headers
                expect(
                    screen.getByRole('columnheader', { name: /Project/i })
                ).toBeInTheDocument();
                expect(
                    screen.getByRole('columnheader', { name: /Docs HEAD/i })
                ).toBeInTheDocument();
                expect(
                    screen.getByRole('columnheader', { name: /Status/i })
                ).toBeInTheDocument();
            });
        });

        it('should have aria-pressed on sort toggle button', async () => {
            const user = userEvent.setup();
            renderProjectsPage();

            await waitFor(() => {
                expect(screen.getByText('sudal')).toBeInTheDocument();
            });

            const sortButton = screen.getByRole('button', {
                name: /Sort by Name/i,
            });
            expect(sortButton).toHaveAttribute('aria-pressed', 'false');

            await user.click(sortButton);

            expect(sortButton).toHaveAttribute('aria-pressed', 'true');
        });
    });
});
