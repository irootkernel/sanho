import { Outlet, Link } from 'react-router-dom';
import { useKkachiState } from '@/application';

/**
 * Layout is the main layout component for the application.
 * It includes a header with title and refresh button.
 */
export function Layout() {
    const { refresh } = useKkachiState();

    const handleRefresh = async (): Promise<void> => {
        await refresh();
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
