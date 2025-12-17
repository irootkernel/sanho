/**
 * Date/time formatting utilities for display.
 * Pure functions with no external dependencies.
 */

/**
 * Formats an ISO timestamp as a relative time string.
 * Examples: "Just now", "5m ago", "2h ago", "3d ago", "Dec 15"
 *
 * @param isoTimestamp - ISO 8601 timestamp string (e.g., "2024-12-15T10:30:00Z")
 * @param now - Optional current date for testing (defaults to new Date())
 * @returns Formatted relative time string, or "—" if null/undefined
 */
export function formatRelativeTime(
    isoTimestamp: string | null | undefined,
    now: Date = new Date()
): string {
    if (!isoTimestamp) return '—';

    const date = new Date(isoTimestamp);
    if (isNaN(date.getTime())) return '—';

    const diffMs = now.getTime() - date.getTime();
    const diffMinutes = Math.floor(diffMs / (1000 * 60));
    const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

    if (diffMinutes < 1) return 'Just now';
    if (diffMinutes < 60) return `${diffMinutes}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    if (diffDays < 7) return `${diffDays}d ago`;

    return date.toLocaleDateString('en-US', {
        month: 'short',
        day: 'numeric',
        year: date.getFullYear() !== now.getFullYear() ? 'numeric' : undefined,
    });
}

/**
 * Formats an ISO timestamp as an absolute date/time string.
 * Format: "YYYY-MM-DD HH:MM"
 *
 * @param isoTimestamp - ISO 8601 timestamp string
 * @returns Formatted date/time string, or "—" if null/undefined
 */
export function formatAbsoluteTime(
    isoTimestamp: string | null | undefined
): string {
    if (!isoTimestamp) return '—';

    const date = new Date(isoTimestamp);
    if (isNaN(date.getTime())) return '—';

    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const hours = String(date.getHours()).padStart(2, '0');
    const minutes = String(date.getMinutes()).padStart(2, '0');

    return `${year}-${month}-${day} ${hours}:${minutes}`;
}

/**
 * Truncates a hash string for display.
 *
 * @param hash - Full hash string
 * @param length - Maximum length before truncation (default 8)
 * @returns Truncated hash with "..." suffix, or "—" if null/undefined
 */
export function formatHash(
    hash: string | null | undefined,
    length: number = 8
): string {
    if (!hash) return '—';
    return hash.length > length ? `${hash.substring(0, length)}...` : hash;
}
