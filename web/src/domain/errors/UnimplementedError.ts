/**
 * UnimplementedError represents a feature that is not yet implemented.
 * This is used during development to clearly indicate which features
 * are pending implementation.
 */
export class UnimplementedError extends Error {
    public readonly featureName: string;

    constructor(featureName: string) {
        super(`Not implemented: ${featureName}`);
        this.name = 'UnimplementedError';
        this.featureName = featureName;
    }
}

/**
 * Type guard to check if an error is an UnimplementedError
 */
export function isUnimplementedError(error: unknown): error is UnimplementedError {
    return error instanceof UnimplementedError;
}
