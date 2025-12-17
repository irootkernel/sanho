import { RouterProvider } from 'react-router-dom';
import { AppProviders } from './AppProviders';
import { router } from '@/presentation/router';

/**
 * App is the root component of the application.
 */
export function App() {
    return (
        <AppProviders>
            <RouterProvider router={router} />
        </AppProviders>
    );
}
