import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Loading } from './Loading';

describe('Loading', () => {
    it('should render loading spinner', () => {
        render(<Loading />);
        expect(document.querySelector('.loading-spinner')).toBeInTheDocument();
    });

    it('should render with message', () => {
        render(<Loading message="Loading data..." />);
        expect(screen.getByText('Loading data...')).toBeInTheDocument();
    });

    it('should render children instead of message', () => {
        render(
            <Loading>
                <span data-testid="custom">Custom loading content</span>
            </Loading>
        );
        expect(screen.getByTestId('custom')).toBeInTheDocument();
    });

    it('should apply custom className', () => {
        const { container } = render(<Loading className="custom-class" />);
        expect(container.querySelector('.loading-container.custom-class')).toBeInTheDocument();
    });

    it('should render without message or children', () => {
        const { container } = render(<Loading />);
        expect(container.querySelector('.loading-container')).toBeInTheDocument();
        expect(container.querySelector('.loading-spinner')).toBeInTheDocument();
    });
});
