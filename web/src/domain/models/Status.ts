/**
 * Status represents the synchronization state of a workspace.
 * - up_to_date: Workspace docs_hash matches the server's docs HEAD
 * - outdated: Workspace docs_hash differs from the server's docs HEAD
 * - unknown: Server does not have docs HEAD for this project
 */
export type Status = 'up_to_date' | 'outdated' | 'unknown';

/**
 * Status display information
 */
export interface StatusDisplay {
    text: string;
    color: 'green' | 'orange' | 'gray';
}

/**
 * Maps Status to display information
 */
export function getStatusDisplay(status: Status): StatusDisplay {
    switch (status) {
        case 'up_to_date':
            return { text: 'Up-to-date', color: 'green' };
        case 'outdated':
            return { text: 'Outdated', color: 'orange' };
        case 'unknown':
            return { text: 'Unknown', color: 'gray' };
    }
}
