import { describe, it, expect } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { RuntimeProvider } from '@/app/di/RuntimeContext';
import { Layout } from '@/presentation/layout';
import { ProjectsPage, ProjectDetailPage, RawStatePage } from '@/presentation/pages';

// Test helper to render with router and runtime
function renderWithProviders(initialRoute: string) {
    return render(
        <RuntimeProvider>
            <MemoryRouter initialEntries={[initialRoute]}>
                <Routes>
                    <Route path="/" element={<Layout />}>
                        <Route index element={<ProjectsPage />} />
                        <Route path="projects/:projectName" element={<ProjectDetailPage />} />
                        <Route path="debug/state" element={<RawStatePage />} />
                    </Route>
                </Routes>
            </MemoryRouter>
        </RuntimeProvider>
    );
}

describe('Router', () => {
    it('should render Layout with header on all routes', async () => {
        renderWithProviders('/');

        expect(screen.getByText('Kkachi Web v2')).toBeInTheDocument();
        expect(screen.getByText('🔄 Refresh')).toBeInTheDocument();

        // Flush async state updates triggered by page effects.
        await waitFor(() => {
            expect(screen.getByText('Feature in Development')).toBeInTheDocument();
        });
    });

    it('should render ProjectsPage and show unimplemented message on "/" route', async () => {
        renderWithProviders('/');

        // Initially shows loading, then shows unimplemented error
        await waitFor(() => {
            expect(screen.getByText('Feature in Development')).toBeInTheDocument();
        });
        expect(screen.getByText(/GetKkachiState\.execute/)).toBeInTheDocument();
    });

    it('should render ProjectDetailPage and show unimplemented message on "/projects/:projectName" route', async () => {
        renderWithProviders('/projects/test-project');

        await waitFor(() => {
            expect(screen.getByText('Feature in Development')).toBeInTheDocument();
        });
    });

    it('should render RawStatePage and show unimplemented message on "/debug/state" route', async () => {
        renderWithProviders('/debug/state');

        await waitFor(() => {
            expect(screen.getByText('Feature in Development')).toBeInTheDocument();
        });
    });
});
