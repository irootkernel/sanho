import { describe, it, expect } from 'vitest';
import { UnimplementedError, isUnimplementedError } from '@/domain/errors';

describe('UnimplementedError', () => {
    it('should create an error with correct message', () => {
        const error = new UnimplementedError('TestFeature');

        expect(error.message).toBe('Not implemented: TestFeature');
        expect(error.name).toBe('UnimplementedError');
        expect(error.featureName).toBe('TestFeature');
    });

    it('should be an instance of Error', () => {
        const error = new UnimplementedError('TestFeature');

        expect(error).toBeInstanceOf(Error);
        expect(error).toBeInstanceOf(UnimplementedError);
    });
});

describe('isUnimplementedError', () => {
    it('should return true for UnimplementedError', () => {
        const error = new UnimplementedError('TestFeature');

        expect(isUnimplementedError(error)).toBe(true);
    });

    it('should return false for regular Error', () => {
        const error = new Error('Regular error');

        expect(isUnimplementedError(error)).toBe(false);
    });

    it('should return false for non-Error values', () => {
        expect(isUnimplementedError(null)).toBe(false);
        expect(isUnimplementedError(undefined)).toBe(false);
        expect(isUnimplementedError('string')).toBe(false);
        expect(isUnimplementedError(123)).toBe(false);
    });
});
