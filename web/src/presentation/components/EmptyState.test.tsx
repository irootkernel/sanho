import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { EmptyState } from './EmptyState';

describe('EmptyState', () => {
    it('should render title', () => {
        render(
            <EmptyState
                title="No Items"
                description="There are no items to display."
            />
        );

        expect(screen.getByRole('heading', { name: 'No Items' })).toBeInTheDocument();
    });

    it('should render description', () => {
        render(
            <EmptyState
                title="Empty"
                description="This is the description text."
            />
        );

        expect(screen.getByText('This is the description text.')).toBeInTheDocument();
    });

    it('should render default icon', () => {
        const { container } = render(
            <EmptyState title="Empty" description="No data" />
        );

        expect(container.querySelector('.empty-icon')).toHaveTextContent('📁');
    });

    it('should render custom icon', () => {
        const { container } = render(
            <EmptyState title="Empty" description="No data" icon="🎉" />
        );

        expect(container.querySelector('.empty-icon')).toHaveTextContent('🎉');
    });

    it('should render hint when provided', () => {
        render(
            <EmptyState
                title="Empty"
                description="No data"
                hint="Try adding some items."
            />
        );

        expect(screen.getByText('Try adding some items.')).toBeInTheDocument();
    });

    it('should render hint as ReactNode', () => {
        render(
            <EmptyState
                title="Empty"
                description="No data"
                hint={
                    <>
                        Use <code>kkachi init</code> to start.
                    </>
                }
            />
        );

        expect(screen.getByText(/kkachi init/)).toBeInTheDocument();
    });

    it('should not render hint when not provided', () => {
        const { container } = render(
            <EmptyState title="Empty" description="No data" />
        );

        // Only the description paragraph should exist, not the hint
        const paragraphs = container.querySelectorAll('p');
        expect(paragraphs).toHaveLength(1);
    });

    it('should apply custom className', () => {
        const { container } = render(
            <EmptyState
                title="Empty"
                description="No data"
                className="custom-empty"
            />
        );

        expect(container.querySelector('.empty-state.custom-empty')).toBeInTheDocument();
    });
});
