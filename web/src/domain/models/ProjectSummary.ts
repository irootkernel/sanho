/**
 * ProjectSummary represents aggregated information for a project.
 * This model is computed from KkachiState for dashboard display.
 */
export interface ProjectSummary {
    /** Project name (e.g., "sudal", "kkachi") */
    project: string;
    /** Server's docs HEAD commit hash for this project, null if not available */
    docs_head: string | null;
    /** Total number of workspaces registered for this project */
    workspace_count: number;
    /** Number of workspaces with unknown status (docs_head is null) */
    unknown_count: number;
    /** Number of workspaces that are outdated (docs_hash !== docs_head) */
    outdated_count: number;
    /** Most recent last_reported_at among all workspaces in this project */
    last_reported_at_max: string | null;
}
