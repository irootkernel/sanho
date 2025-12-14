import { RouterProvider } from 'react-router-dom';
import { RuntimeProvider } from './di/RuntimeContext';
import { router } from '@/presentation/router';

/**
 * App is the root component of the application.
 */
export function App() {
    return (
        <RuntimeProvider>
            <RouterProvider router={router} />
        </RuntimeProvider>
    );
}
