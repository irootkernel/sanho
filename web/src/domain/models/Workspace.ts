/**
 * Workspace represents a single workspace registered with kkachi-server.
 * This model matches the workspace object in /api/state response.
 */
export interface Workspace {
    /** Unique identifier for the workspace */
    workspace_id: string;
    /** Project name this workspace belongs to */
    project: string;
    /** ID of the docs repository */
    docs_repo_id: string;
    /** Local file system path of the workspace */
    local_path: string;
    /** Git repository URL */
    repo_url: string;
    /** Current docs hash in the workspace (.kkachi_docs_hash) */
    docs_hash: string;
    /** ISO 8601 timestamp of last status report, null if never reported */
    last_reported_at: string | null;
    /** Email of the last actor who reported */
    last_actor_email: string;
}
