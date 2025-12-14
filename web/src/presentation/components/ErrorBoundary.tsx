import { Component, type ReactNode } from 'react';
import { isUnimplementedError } from '@/domain';

interface Props {
    children: ReactNode;
}

interface State {
    error: Error | null;
}

/**
 * ErrorBoundary catches errors thrown by child components.
 * - UnimplementedError: Shows "Feature in development" message
 * - Other errors: Shows "An error occurred" message
 */
export class ErrorBoundary extends Component<Props, State> {
    constructor(props: Props) {
        super(props);
        this.state = { error: null };
    }

    static getDerivedStateFromError(error: Error): State {
        return { error };
    }

    componentDidCatch(error: Error, errorInfo: React.ErrorInfo): void {
        console.error('ErrorBoundary caught an error:', error, errorInfo);
    }

    handleRetry = (): void => {
        this.setState({ error: null });
    };

    render(): ReactNode {
        const { error } = this.state;
        const { children } = this.props;

        if (error) {
            if (isUnimplementedError(error)) {
                return (
                    <div className="error-container unimplemented">
                        <div className="error-icon">🚧</div>
                        <h2>Feature in Development</h2>
                        <p>
                            The feature <code>{error.featureName}</code> is not yet implemented.
                        </p>
                        <p className="hint">This feature will be available in a future update.</p>
                    </div>
                );
            }

            return (
                <div className="error-container error">
                    <div className="error-icon">⚠️</div>
                    <h2>An Error Occurred</h2>
                    <p>{error.message}</p>
                    <button onClick={this.handleRetry} className="retry-button">
                        Retry
                    </button>
                </div>
            );
        }

        return children;
    }
}
