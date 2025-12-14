import { describe, it, expect } from 'vitest'
import {
    ApiError,
    NetworkError,
    isApiError,
    isNetworkError,
} from './errors'

describe('ApiError', () => {
    it('should create an error with message only', () => {
        const error = new ApiError('Something went wrong')

        expect(error.message).toBe('Something went wrong')
        expect(error.name).toBe('ApiError')
        expect(error.statusCode).toBeUndefined()
    })

    it('should create an error with message and status code', () => {
        const error = new ApiError('Not found', 404)

        expect(error.message).toBe('Not found')
        expect(error.name).toBe('ApiError')
        expect(error.statusCode).toBe(404)
    })

    it('should be an instance of Error', () => {
        const error = new ApiError('Test error')

        expect(error).toBeInstanceOf(Error)
        expect(error).toBeInstanceOf(ApiError)
    })

    it('should have correct status codes for common HTTP errors', () => {
        const badRequest = new ApiError('Bad Request', 400)
        const unauthorized = new ApiError('Unauthorized', 401)
        const forbidden = new ApiError('Forbidden', 403)
        const notFound = new ApiError('Not Found', 404)
        const serverError = new ApiError('Internal Server Error', 500)

        expect(badRequest.statusCode).toBe(400)
        expect(unauthorized.statusCode).toBe(401)
        expect(forbidden.statusCode).toBe(403)
        expect(notFound.statusCode).toBe(404)
        expect(serverError.statusCode).toBe(500)
    })
})

describe('NetworkError', () => {
    it('should create an error with message only', () => {
        const error = new NetworkError('Failed to connect')

        expect(error.message).toBe('Failed to connect')
        expect(error.name).toBe('NetworkError')
        expect(error.cause).toBeUndefined()
    })

    it('should create an error with message and cause', () => {
        const cause = new Error('Timeout')
        const error = new NetworkError('Failed to connect', cause)

        expect(error.message).toBe('Failed to connect')
        expect(error.name).toBe('NetworkError')
        expect(error.cause).toBe(cause)
    })

    it('should be an instance of Error', () => {
        const error = new NetworkError('Test error')

        expect(error).toBeInstanceOf(Error)
        expect(error).toBeInstanceOf(NetworkError)
    })
})

describe('isApiError', () => {
    it('should return true for ApiError instances', () => {
        const error = new ApiError('Test', 500)

        expect(isApiError(error)).toBe(true)
    })

    it('should return false for NetworkError instances', () => {
        const error = new NetworkError('Test')

        expect(isApiError(error)).toBe(false)
    })

    it('should return false for regular Error instances', () => {
        const error = new Error('Test')

        expect(isApiError(error)).toBe(false)
    })

    it('should return false for non-Error values', () => {
        expect(isApiError(null)).toBe(false)
        expect(isApiError(undefined)).toBe(false)
        expect(isApiError('error string')).toBe(false)
        expect(isApiError(500)).toBe(false)
        expect(isApiError({ message: 'error' })).toBe(false)
    })
})

describe('isNetworkError', () => {
    it('should return true for NetworkError instances', () => {
        const error = new NetworkError('Test')

        expect(isNetworkError(error)).toBe(true)
    })

    it('should return false for ApiError instances', () => {
        const error = new ApiError('Test', 500)

        expect(isNetworkError(error)).toBe(false)
    })

    it('should return false for regular Error instances', () => {
        const error = new Error('Test')

        expect(isNetworkError(error)).toBe(false)
    })

    it('should return false for non-Error values', () => {
        expect(isNetworkError(null)).toBe(false)
        expect(isNetworkError(undefined)).toBe(false)
        expect(isNetworkError('error string')).toBe(false)
        expect(isNetworkError(500)).toBe(false)
        expect(isNetworkError({ message: 'error' })).toBe(false)
    })
})
