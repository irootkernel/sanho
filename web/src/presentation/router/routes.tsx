import { createBrowserRouter, type RouteObject } from 'react-router-dom';
import { Layout } from '@/presentation/layout';
import { ProjectsPage, ProjectDetailPage, RawStatePage } from '@/presentation/pages';
import { ErrorBoundary } from '@/presentation/components';

/**
 * Route definitions for the application.
 */
const routes: RouteObject[] = [
    {
        path: '/',
        element: <Layout />,
        errorElement: <ErrorBoundary><div>Route Error</div></ErrorBoundary>,
        children: [
            {
                index: true,
                element: (
                    <ErrorBoundary>
                        <ProjectsPage />
                    </ErrorBoundary>
                ),
            },
            {
                path: 'projects/:projectName',
                element: (
                    <ErrorBoundary>
                        <ProjectDetailPage />
                    </ErrorBoundary>
                ),
            },
            {
                path: 'debug/state',
                element: (
                    <ErrorBoundary>
                        <RawStatePage />
                    </ErrorBoundary>
                ),
            },
        ],
    },
];

/**
 * The application router.
 */
export const router = createBrowserRouter(routes);
