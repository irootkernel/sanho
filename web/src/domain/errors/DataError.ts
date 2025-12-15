/**
 * Error thrown when data validation fails or invalid data is encountered.
 */
export class DataError extends Error {
    constructor(message: string) {
        super(message);
        this.name = 'DataError';
    }
}
