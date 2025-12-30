import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createSession, terminateSession } from './pty';
import { buildApiUrl } from '@/data/http/config';

describe('pty API', () => {
    beforeEach(() => {
        vi.resetAllMocks();
    });

    afterEach(() => {
        vi.unstubAllGlobals();
    });

    describe('createSession', () => {
        it('should call POST /api/pty/sessions and return response data', async () => {
            const mockResponse = {
                session_id: 'sess-123',
                ws_url: '/api/pty/sessions/sess-123/ws',
                resolved_cwd: '/tmp/ws',
            };

            const fetchMock = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve(mockResponse),
            });
            vi.stubGlobal('fetch', fetchMock);

            const request = {
                workspace_id: 'ws-1',
                cwd_rel: 'src',
            };

            const result = await createSession(request);

            expect(fetchMock).toHaveBeenCalledWith(buildApiUrl('/pty/sessions'), {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(request),
            });
            expect(result).toEqual(mockResponse);
        });

        it('should throw ApiError on non-OK response', async () => {
            const fetchMock = vi.fn().mockResolvedValue({
                ok: false,
                status: 400,
                statusText: 'Bad Request',
            });
            vi.stubGlobal('fetch', fetchMock);

            await expect(createSession({ workspace_id: 'ws-1' })).rejects.toThrow('Server returned 400: Bad Request');
        });
    });

    describe('terminateSession', () => {
        it('should call DELETE /api/pty/sessions/:id', async () => {
            const fetchMock = vi.fn().mockResolvedValue({
                ok: true,
            });
            vi.stubGlobal('fetch', fetchMock);

            const sessionId = 'sess-123';
            await terminateSession(sessionId);

            expect(fetchMock).toHaveBeenCalledWith(buildApiUrl(`/pty/sessions/${sessionId}`), {
                method: 'DELETE',
                headers: {
                    'Content-Type': 'application/json',
                },
            });
        });

        it('should throw ApiError on non-OK response', async () => {
            const fetchMock = vi.fn().mockResolvedValue({
                ok: false,
                status: 404,
                statusText: 'Not Found',
            });
            vi.stubGlobal('fetch', fetchMock);
            
            await expect(terminateSession('sess-123')).rejects.toThrow('Server returned 404: Not Found');
        });
    });
});
