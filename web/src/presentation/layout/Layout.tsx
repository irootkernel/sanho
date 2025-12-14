import { Outlet, Link } from 'react-router-dom';
import { useRuntime } from '@/app/di/RuntimeContext';

/**
 * Layout is the main layout component for the application.
 * It includes a header with title and refresh button.
 */
export function Layout() {
    const { refreshState } = useRuntime();

    const handleRefresh = async (): Promise<void> => {
        try {
            await refreshState();
        } catch (error) {
            // Error will be caught by ErrorBoundary
            console.error('Failed to refresh state:', error);
        }
    };

    return (
        <div className="layout">
            <header className="header">
                <div className="header-content">
                    <Link to="/" className="header-title">
                        <h1>Kkachi Web v2</h1>
                    </Link>
                    <nav className="header-nav">
                        <Link to="/" className="nav-link">Dashboard</Link>
                        <Link to="/debug/state" className="nav-link">Debug</Link>
                    </nav>
                    <button onClick={handleRefresh} className="refresh-button">
                        🔄 Refresh
                    </button>
                </div>
            </header>
            <main className="main-content">
                <Outlet />
            </main>
        </div>
    );
}
