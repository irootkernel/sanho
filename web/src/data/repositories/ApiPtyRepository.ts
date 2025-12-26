import type { PtyRepository, CreateSessionRequest, CreateSessionResponse } from '@/application/ports/PtyRepository';
import { buildApiUrl } from '../http/config';

export class ApiPtyRepository implements PtyRepository {
    async createSession(request: CreateSessionRequest): Promise<CreateSessionResponse> {
        const url = buildApiUrl('/pty/sessions');
        console.log('PTY Session Create requested (STUB):', request, 'to', url);
        
        // This will be implemented in CTASK-2
        throw new Error('Not implemented');
    }

    async terminateSession(sessionId: string): Promise<void> {
        const url = buildApiUrl(`/pty/sessions/${sessionId}`);
        console.log('PTY Session Terminate requested (STUB):', sessionId, 'to', url);

        // This will be implemented in CTASK-2
        throw new Error('Not implemented');
    }
}
