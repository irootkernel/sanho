# Sanho

**Sanho** is a central document coordination system designed to synchronize a specific documentation directory (e.g., `docs/`) across multiple Git repositories (workspaces). It ensures that documentation remains consistent and version-controlled in a dedicated repository (e.g., `sudal_docs`), separate from the application code.

## Language & Communication Guidelines

*   **User Interaction:** All conversations with the user must be conducted in **Korean**.
*   **Project Context:** The primary language for requirements and documentation in this project is Korean.
*   **Implementation Standards:**
    *   **Source Code Comments:** Must be written in **English**.
    *   **UI Text (CLI Output, Logs, Error Messages):** Must be written in **English**.

## Project Overview

The system consists of two main components:

1.  **sanhod**: A central server that:
    *   Manages local clones of documentation repositories.
    *   Provides REST APIs to query the current HEAD, register workspaces, and push document changes.
    *   Maintains the state of all registered workspaces and projects.
    *   Handles the actual Git operations (commit, push) to the central documentation repository.

2.  **Sanho CLI**: A command-line tool used by developers in their local workspaces.
    *   Integrates via Git hooks (`pre-commit`, `pre-push`, etc.) to automate synchronization.
    *   Detects "outdated" documentation states and facilitates 3-way merges.
    *   Communicates with `sanhod` to fetch snapshots and push changes.

### Core Technologies
*   **Language:** Go 1.25.0
*   **Architecture:** Clean Architecture (Domain, Usecase, Interface, Infra)
*   **Persistence:** File-based JSON state storage (for v1).
*   **VCS Interaction:** Wraps the `git` CLI using `os/exec`.

## Architecture & Directory Structure

The project follows a Clean Architecture approach, co-locating server and client code in the same repository.

```text
/
├── cmd/
│   └── server/           # Server entry point
│   └── sanho/            # CLI entry point
├── internal/
│   ├── config/           # Configuration handling
│   ├── domain/           # Core business entities and repository interfaces (No external deps)
│   ├── usecase/          # Application business logic (Depends on Domain)
│   ├── interface/        # Interface adapters (HTTP handlers, CLI commands)
│   │   ├── http/         # Server HTTP handlers
│   │   └── cli/          # CLI command implementations (planned)
│   └── infra/            # Infrastructure implementations (Git, FS, State)
│       ├── git/          # Git client wrapper
│       └── state/        # File-based state repository
├── docs/                 # detailed requirements and roadmaps
└── test/                 # E2E and integration tests
```

## Development Conventions

### 1. Architecture Layers
*   **Domain:** Pure Go structs and interfaces. No imports from `infra` or `interface`.
*   **Usecase:** Implements business logic. Depends only on `domain`.
*   **Interface:** Adapters for external agents (HTTP, CLI). Depends on `usecase`.
*   **Infra:** Concrete implementations of interfaces defined in `domain`.

### 2. Testing
*   **Unit Tests:** Focus on Domain and Usecase logic using mocks/fakes.
*   **Integration Tests:** Verify interactions between adapters and real/fake infrastructure.
*   **E2E Tests:** Located in `test/e2e`, verify the full system flow (Server + Git operations).

### 3. Git & Documentation Management
*   **Server State:** The server maintains a single JSON file (default: `data/kkachi_state.json`) to track projects and workspaces.
*   **Docs Repos:** The server manages local clones of the documentation repositories. It performs `git fetch` on startup and ensures synchronization.
*   **Conflict Resolution:** The system detects conflicts ("outdated" state) but relies on the user to resolve them manually using standard Git conflict markers.

## Building and Running

### Prerequisites
*   Go 1.25+
*   Git installed and available in `PATH`.

### Server

To run the server locally:

```bash
# Set environment variables (optional, defaults shown)
export PORT=5789
export STATE_FILE_PATH=data/kkachi_state.json

# Run the server
go run cmd/sanhod/main.go
```

### Testing

Run all tests:

```bash
go test ./...
```

## Key Documentation

*   **Requirements:** `docs/requirement-v1.md` - Detailed system behavior and protocol specifications.

## Lessons Learned & Agent Self-Correction

This section records mistakes made by the agent during development (specifically during CTASK-5) and the resulting corrections to prevent recurrence.

### 1. Protocol & Control Compliance
*   **Mistake:** Arbitrarily marked phases as "Complete" without seeking explicit user confirmation during the `User Manual Verification` steps of the Conductor framework.
*   **Correction:** Must halt operations at the end of every Phase and request a manual review from the user. Skipping these steps is a violation of the framework's protocol and the user's control.

### 2. Rigorous Technical Validation
*   **Mistake:** Declared implementation complete without running the full build (`make build-web`) and test suite (`make test-web`). This led to late discovery of Lint errors, TypeScript type mismatches, and regressions in existing tests.
*   **Correction:** Never declare a task "Finished" until a 'Zero Error' state is confirmed by running all project-defined validation scripts. Prioritize passing existing tests during refactoring.

### 3. Precision in Requirement Analysis
*   **Mistake:** Overlooked core requirements (e.g., Auth Token support) explicitly mentioned in the roadmap, focusing only on a subset of features (DnD).
*   **Correction:** Create a checklist based on `spec.md` and the roadmap before starting. Cross-reference all implemented features against this checklist before reporting progress.

### 4. Environmental Awareness (Docker vs. Host)
*   **Mistake:** Failed to account for the difference between the host filesystem and the Docker container's volumes (e.g., `web_node_modules`). Installed packages only on the host, leading to runtime resolution errors in the container.
*   **Correction:** Analyze the infrastructure configuration (`docker-compose.dev.yml`) beforehand. Ensure commands are executed in the correct context (Host vs. Container) to maintain environment consistency.

### 5. Adherence to Communication Guidelines
*   **Mistake:** Failed to maintain the user's preferred interaction language (Korean), occasionally reverting to English without permission.
*   **Correction:** Strictly respect the communication guidelines established by the user. Maintain consistent language usage even across context switches.
