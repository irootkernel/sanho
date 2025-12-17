import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ErrorBanner } from './ErrorBanner';
import { UnimplementedError } from '@/domain';

describe('ErrorBanner', () => {
    describe('UnimplementedError handling', () => {
        it('should show "Feature in Development" for UnimplementedError', () => {
            const error = new UnimplementedError('TestFeature');
            render(<ErrorBanner error={error} />);

            expect(screen.getByText(/Feature in Development/i)).toBeInTheDocument();
            expect(screen.getByText(/TestFeature/i)).toBeInTheDocument();
        });

        it('should show hint message for UnimplementedError', () => {
            const error = new UnimplementedError('SomeFeature');
            render(<ErrorBanner error={error} />);

            expect(screen.getByText(/will be available in a future update/i)).toBeInTheDocument();
        });

        it('should not show retry button for UnimplementedError', () => {
            const error = new UnimplementedError('Feature');
            const onRetry = vi.fn();
            render(<ErrorBanner error={error} onRetry={onRetry} />);

            expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument();
        });
    });

    describe('General error handling', () => {
        it('should show error message', () => {
            const error = new Error('Something went wrong');
            render(<ErrorBanner error={error} />);

            expect(screen.getByText('Something went wrong')).toBeInTheDocument();
        });

        it('should show default title', () => {
            const error = new Error('Test error');
            render(<ErrorBanner error={error} />);

            expect(screen.getByText('An Error Occurred')).toBeInTheDocument();
        });

        it('should show custom title', () => {
            const error = new Error('Test error');
            render(<ErrorBanner error={error} title="Custom Error Title" />);

            expect(screen.getByText('Custom Error Title')).toBeInTheDocument();
        });

        it('should show retry button when onRetry is provided', () => {
            const error = new Error('Test error');
            const onRetry = vi.fn();
            render(<ErrorBanner error={error} onRetry={onRetry} />);

            expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
        });

        it('should call onRetry when retry button is clicked', () => {
            const error = new Error('Test error');
            const onRetry = vi.fn();
            render(<ErrorBanner error={error} onRetry={onRetry} />);

            fireEvent.click(screen.getByRole('button', { name: /retry/i }));
            expect(onRetry).toHaveBeenCalledTimes(1);
        });

        it('should not show retry button when onRetry is not provided', () => {
            const error = new Error('Test error');
            render(<ErrorBanner error={error} />);

            expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument();
        });
    });
});
