import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RawStatePage } from './RawStatePage'
import { AppProviders } from '@/app'
import sampleState from '@/test/fixtures/api-state.sample.json'

// Helper to render RawStatePage with AppProviders
function renderRawStatePage() {
    return render(
        <AppProviders>
            <RawStatePage />
        </AppProviders>,
    )
}

describe('RawStatePage', () => {
    beforeEach(() => {
        vi.resetAllMocks()
    })

    afterEach(() => {
        vi.unstubAllGlobals()
    })

    describe('Loading state', () => {
        it('should show loading spinner initially', () => {
            // Never resolve the fetch to keep loading state
            vi.stubGlobal('fetch', vi.fn().mockImplementation(() => new Promise(() => { })))

            renderRawStatePage()

            expect(screen.getByText(/Loading state.../i)).toBeInTheDocument()
        })

        it('should show Debug: Raw State heading while loading', () => {
            vi.stubGlobal('fetch', vi.fn().mockImplementation(() => new Promise(() => { })))

            renderRawStatePage()

            expect(screen.getByRole('heading', { name: /Debug: Raw State/i })).toBeInTheDocument()
        })
    })

    describe('Success state', () => {
        beforeEach(() => {
            vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve(sampleState),
            }))
        })

        it('should display project count badge', async () => {
            renderRawStatePage()

            await waitFor(() => {
                expect(screen.getByText(/2 projects/i)).toBeInTheDocument()
            })
        })

        it('should display workspace count badge', async () => {
            renderRawStatePage()

            await waitFor(() => {
                expect(screen.getByText(/3 workspaces/i)).toBeInTheDocument()
            })
        })

        it('should display prettified JSON', async () => {
            renderRawStatePage()

            await waitFor(() => {
                // Check for JSON structure elements
                expect(screen.getByText(/"docs_heads"/)).toBeInTheDocument()
                expect(screen.getByText(/"workspaces"/)).toBeInTheDocument()
            })
        })
    })

    describe('Empty state', () => {
        it('should handle empty docs_heads and workspaces', async () => {
            vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ docs_heads: {}, workspaces: [] }),
            }))

            renderRawStatePage()

            await waitFor(() => {
                expect(screen.getByText(/0 projects/i)).toBeInTheDocument()
                expect(screen.getByText(/0 workspaces/i)).toBeInTheDocument()
            })
        })
    })

    describe('Error state - Network Error', () => {
        beforeEach(() => {
            vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Network error')))
        })

        it('should display error message', async () => {
            renderRawStatePage()

            await waitFor(() => {
                expect(screen.getByText(/Failed to Load State/i)).toBeInTheDocument()
            })
        })

        it('should display error details', async () => {
            renderRawStatePage()

            await waitFor(() => {
                expect(screen.getByText(/Failed to connect to server/i)).toBeInTheDocument()
            })
        })

        it('should display retry button', async () => {
            renderRawStatePage()

            await waitFor(() => {
                expect(screen.getByRole('button', { name: /🔄 Retry/i })).toBeInTheDocument()
            })
        })

        it('should refetch when retry button is clicked', async () => {
            const user = userEvent.setup()
            const mockFetch = vi.fn()
                .mockRejectedValueOnce(new Error('Network error'))
                .mockResolvedValueOnce({
                    ok: true,
                    json: () => Promise.resolve(sampleState),
                })
            vi.stubGlobal('fetch', mockFetch)

            renderRawStatePage()

            // Wait for error state
            await waitFor(() => {
                expect(screen.getByText(/Failed to Load State/i)).toBeInTheDocument()
            })

            // Click retry button
            const retryButton = screen.getByRole('button', { name: /🔄 Retry/i })
            await user.click(retryButton)

            // Should now show success state
            await waitFor(() => {
                expect(screen.getByText(/2 projects/i)).toBeInTheDocument()
            })
        })
    })

    describe('Error state - API Error', () => {
        it('should display 500 error message', async () => {
            vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
                ok: false,
                status: 500,
                statusText: 'Internal Server Error',
            }))

            renderRawStatePage()

            await waitFor(() => {
                expect(screen.getByText(/Failed to Load State/i)).toBeInTheDocument()
                expect(screen.getByText(/Server returned 500/i)).toBeInTheDocument()
            })
        })

        it('should display 404 error message', async () => {
            vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
                ok: false,
                status: 404,
                statusText: 'Not Found',
            }))

            renderRawStatePage()

            await waitFor(() => {
                expect(screen.getByText(/Server returned 404/i)).toBeInTheDocument()
            })
        })
    })

    describe('Accessibility', () => {
        it('should have proper heading hierarchy', async () => {
            vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve(sampleState),
            }))

            renderRawStatePage()

            await waitFor(() => {
                const heading = screen.getByRole('heading', { level: 2 })
                expect(heading).toHaveTextContent('Debug: Raw State')
            })
        })
    })
})
