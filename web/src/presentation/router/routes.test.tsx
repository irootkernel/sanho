import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { AppProviders } from '@/app';
import { Layout } from '@/presentation/layout';
import {
    ProjectsPage,
    ProjectDetailPage,
    RawStatePage,
    TerminalPage,
} from '@/presentation/pages';
import sampleState from '@/test/fixtures/api-state.sample.json';

// Test helper to render with router and app-level providers
function renderWithProviders(initialRoute: string) {
    return render(
        <AppProviders>
            <MemoryRouter initialEntries={[initialRoute]}>
                <Routes>
                    <Route path="/" element={<Layout />}>
                        <Route index element={<ProjectsPage />} />
                        <Route
                            path="projects/:projectName"
                            element={<ProjectDetailPage />}
                        />
                        <Route path="terminal" element={<TerminalPage />} />
                        <Route path="debug/state" element={<RawStatePage />} />
                    </Route>
                </Routes>
            </MemoryRouter>
        </AppProviders>,
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

        expect(screen.getByText('Kkachi Web')).toBeInTheDocument();
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
        renderWithProviders('/projects/sudal');

        await waitFor(() => {
            expect(
                screen.getByRole('heading', { name: /Project: sudal/i }),
            ).toBeInTheDocument();
        });
    });


    it('should render TerminalPage on "/terminal" route', async () => {
        renderWithProviders('/terminal');

        await waitFor(() => {
            expect(
                screen.getByRole('heading', { name: /Consoles/i }),
            ).toBeInTheDocument();
        });

        // Should show empty state message
        expect(screen.getByText(/No active consoles/i)).toBeInTheDocument();
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
