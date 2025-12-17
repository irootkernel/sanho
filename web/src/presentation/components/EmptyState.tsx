import type { ReactNode } from 'react';

interface EmptyStateProps {
    /** Icon emoji or element */
    icon?: string | ReactNode;
    /** Main title */
    title: string;
    /** Description text */
    description: string;
    /** Hint or instruction text */
    hint?: ReactNode;
    /** Additional CSS class names */
    className?: string;
}

/**
 * EmptyState component for displaying empty or no-data states.
 * Use for consistent empty state messaging across the application.
 */
export function EmptyState({
    icon = '📁',
    title,
    description,
    hint,
    className = '',
}: EmptyStateProps) {
    return (
        <div className={`empty-state ${className}`.trim()}>
            <div className="empty-icon">{icon}</div>
            <h3>{title}</h3>
            <p>{description}</p>
            {hint && <p className="hint">{hint}</p>}
        </div>
    );
}
