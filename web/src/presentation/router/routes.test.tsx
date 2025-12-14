import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { RuntimeProvider } from '@/app/di/RuntimeContext';
import { Layout } from '@/presentation/layout';
import {
    ProjectsPage,
    ProjectDetailPage,
    RawStatePage,
} from '@/presentation/pages';
import sampleState from '@/test/fixtures/api-state.sample.json';

// Test helper to render with router and runtime
function renderWithProviders(initialRoute: string) {
    return render(
        <RuntimeProvider>
            <MemoryRouter initialEntries={[initialRoute]}>
                <Routes>
                    <Route path="/" element={<Layout />}>
                        <Route index element={<ProjectsPage />} />
                        <Route
                            path="projects/:projectName"
                            element={<ProjectDetailPage />}
                        />
                        <Route path="debug/state" element={<RawStatePage />} />
                    </Route>
                </Routes>
            </MemoryRouter>
        </RuntimeProvider>,
    );
}

describe('Router', () => {
    beforeEach(() => {
        vi.resetAllMocks();
        // Mock successful API response
        vi.stubGlobal(
            'fetch',
            vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve(sampleState),
            }),
        );
    });

    afterEach(() => {
        vi.unstubAllGlobals();
    });

    it('should render Layout with header on all routes', async () => {
        renderWithProviders('/');

        expect(screen.getByText('Kkachi Web v2')).toBeInTheDocument();
        expect(screen.getByText('🔄 Refresh')).toBeInTheDocument();

        // Wait for async state to settle
        await waitFor(() => {
            expect(
                screen.getByRole('heading', { name: /Projects Dashboard/i }),
            ).toBeInTheDocument();
        });
    });

    it('should render ProjectsPage on "/" route', async () => {
        renderWithProviders('/');

        await waitFor(() => {
            expect(
                screen.getByRole('heading', { name: /Projects Dashboard/i }),
            ).toBeInTheDocument();
        });
    });

    it('should render ProjectDetailPage on "/projects/:projectName" route', async () => {
        renderWithProviders('/projects/test-project');

        await waitFor(() => {
            // ProjectDetailPage still shows unimplemented in CTASK-2
            // It will be implemented in CTASK-4
            expect(
                screen.getByText(/test-project|Feature in Development/i),
            ).toBeInTheDocument();
        });
    });

    it('should render RawStatePage and show JSON state on "/debug/state" route', async () => {
        renderWithProviders('/debug/state');

        await waitFor(() => {
            expect(screen.getByText(/Debug: Raw State/)).toBeInTheDocument();
        });

        await waitFor(() => {
            expect(screen.getByText(/2 projects/)).toBeInTheDocument();
            expect(screen.getByText(/3 workspaces/)).toBeInTheDocument();
        });
    });

    it('should show error state when API fails on RawStatePage', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn().mockResolvedValue({
                ok: false,
                status: 500,
                statusText: 'Internal Server Error',
            }),
        );

        renderWithProviders('/debug/state');

        await waitFor(() => {
            expect(
                screen.getByText(/Failed to Load State/),
            ).toBeInTheDocument();
            expect(screen.getByText(/Server returned 500/)).toBeInTheDocument();
        });

        // Retry button should be visible
        expect(screen.getByText(/🔄 Retry/)).toBeInTheDocument();
    });
});
