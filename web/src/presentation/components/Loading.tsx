import type { ReactNode } from 'react';

interface LoadingProps {
    /** Optional loading message */
    message?: string;
    /** Additional CSS class names */
    className?: string;
    /** Child elements (alternative to message) */
    children?: ReactNode;
}

/**
 * Loading component displaying a spinner with optional message.
 * Use for consistent loading states across the application.
 */
export function Loading({ message, className = '', children }: LoadingProps) {
    return (
        <div className={`loading-container ${className}`.trim()}>
            <div className="loading-spinner"></div>
            {message && <p>{message}</p>}
            {children}
        </div>
    );
}
