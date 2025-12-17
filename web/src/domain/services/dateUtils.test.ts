import { describe, it, expect } from 'vitest';
import {
    formatRelativeTime,
    formatAbsoluteTime,
    formatHash,
} from './dateUtils';

describe('formatRelativeTime', () => {
    const baseDate = new Date('2024-12-15T12:00:00Z');

    it('should return "—" for null input', () => {
        expect(formatRelativeTime(null)).toBe('—');
    });

    it('should return "—" for undefined input', () => {
        expect(formatRelativeTime(undefined)).toBe('—');
    });

    it('should return "—" for invalid date string', () => {
        expect(formatRelativeTime('invalid-date')).toBe('—');
    });

    it('should return "Just now" for less than 1 minute ago', () => {
        const timestamp = '2024-12-15T11:59:30Z';
        expect(formatRelativeTime(timestamp, baseDate)).toBe('Just now');
    });

    it('should return minutes ago for 1-59 minutes', () => {
        const timestamp = '2024-12-15T11:30:00Z';
        expect(formatRelativeTime(timestamp, baseDate)).toBe('30m ago');
    });

    it('should return hours ago for 1-23 hours', () => {
        const timestamp = '2024-12-15T06:00:00Z';
        expect(formatRelativeTime(timestamp, baseDate)).toBe('6h ago');
    });

    it('should return days ago for 1-6 days', () => {
        const timestamp = '2024-12-12T12:00:00Z';
        expect(formatRelativeTime(timestamp, baseDate)).toBe('3d ago');
    });

    it('should return short date for 7+ days in same year', () => {
        const timestamp = '2024-12-01T12:00:00Z';
        const result = formatRelativeTime(timestamp, baseDate);
        expect(result).toMatch(/Dec 1/);
    });

    it('should include year for different year', () => {
        const timestamp = '2023-12-15T12:00:00Z';
        const result = formatRelativeTime(timestamp, baseDate);
        expect(result).toMatch(/2023/);
    });
});

describe('formatAbsoluteTime', () => {
    it('should return "—" for null input', () => {
        expect(formatAbsoluteTime(null)).toBe('—');
    });

    it('should return "—" for undefined input', () => {
        expect(formatAbsoluteTime(undefined)).toBe('—');
    });

    it('should return "—" for invalid date string', () => {
        expect(formatAbsoluteTime('invalid-date')).toBe('—');
    });

    it('should format date as YYYY-MM-DD HH:MM', () => {
        // Note: The output is in local time, so we use a date that we control
        const date = new Date(2024, 11, 15, 14, 30); // Dec 15, 2024 14:30 local
        const timestamp = date.toISOString();
        expect(formatAbsoluteTime(timestamp)).toBe('2024-12-15 14:30');
    });

    it('should pad single digit months and days', () => {
        const date = new Date(2024, 0, 5, 9, 5); // Jan 5, 2024 09:05 local
        const timestamp = date.toISOString();
        expect(formatAbsoluteTime(timestamp)).toBe('2024-01-05 09:05');
    });
});

describe('formatHash', () => {
    it('should return "—" for null input', () => {
        expect(formatHash(null)).toBe('—');
    });

    it('should return "—" for undefined input', () => {
        expect(formatHash(undefined)).toBe('—');
    });

    it('should return full hash if shorter than limit', () => {
        expect(formatHash('abc123')).toBe('abc123');
    });

    it('should return full hash if exactly at limit', () => {
        expect(formatHash('abc12345')).toBe('abc12345');
    });

    it('should truncate hash if longer than limit', () => {
        expect(formatHash('abc123def456')).toBe('abc123de...');
    });

    it('should respect custom length parameter', () => {
        expect(formatHash('abc123def456', 4)).toBe('abc1...');
    });
});
