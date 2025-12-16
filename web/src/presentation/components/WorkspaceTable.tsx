import type { WorkspaceWithStatus } from '@/domain';
import { StatusBadge } from './StatusBadge';

interface WorkspaceTableProps {
    workspaces: WorkspaceWithStatus[];
}

function formatRelativeTime(isoTimestamp: string | null): string {
    if (!isoTimestamp) return '—';
    const date = new Date(isoTimestamp);
    return date.toLocaleString();
}

/**
 * Truncates hash for display
 */
function formatHash(hash: string | null): string {
    if (!hash) return '—';
    return hash.length > 8 ? `${hash.substring(0, 8)}...` : hash;
}

/**
 * Validates repo URL to prevent XSS attacks (e.g., javascript: scheme)
 * Only allows http://, https://, and git@github.com: URLs
 */
function isValidRepoUrl(url: string): boolean {
    return /^(https?:\/\/|git@github\.com:)/i.test(url);
}

export function WorkspaceTable({ workspaces }: WorkspaceTableProps) {
    return (
        <table className="workspace-table">
            <thead>
                <tr>
                    <th>Local Path</th>
                    <th>Status</th>
                    <th>Repo URL</th>
                    <th>Docs Hash</th>
                    <th>Last Reported</th>
                    <th>Last Actor</th>
                </tr>
            </thead>
            <tbody>
                {workspaces.map((ws) => (
                    <tr
                        key={ws.workspace_id}
                        className={
                            ws.status === 'outdated' ? 'row-outdated' : ''
                        }
                    >
                        <td className="ws-path">
                            <code>{ws.local_path}</code>
                        </td>
                        <td className="ws-status">
                            <StatusBadge status={ws.status} />
                        </td>
                        <td className="ws-repo">
                            {ws.repo_url && isValidRepoUrl(ws.repo_url) ? (
                                <a
                                    href={ws.repo_url
                                        .replace('git@github.com:', 'https://github.com/')
                                        .replace('.git', '')}
                                    target="_blank"
                                    rel="noreferrer"
                                >
                                    Repo ↗
                                </a>
                            ) : (
                                '—'
                            )}
                        </td>
                        <td className="ws-hash">
                            <code>{formatHash(ws.docs_hash)}</code>
                        </td>
                        <td className="ws-updated">
                            {formatRelativeTime(ws.last_reported_at)}
                        </td>
                        <td className="ws-actor">
                            {ws.last_actor_email || '—'}
                        </td>
                    </tr>
                ))}
            </tbody>
        </table>
    );
}
