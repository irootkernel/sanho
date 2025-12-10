# Kkachi

**Kkachi** is a central document coordination system designed to synchronize a specific documentation directory (e.g., `docs/`) across multiple Git repositories (workspaces). It ensures that documentation remains consistent and version-controlled in a dedicated repository (e.g., `sudal_docs`), separate from the application code.

## Language & Communication Guidelines

*   **User Interaction:** All conversations with the user must be conducted in **Korean**.
*   **Project Context:** The primary language for requirements and documentation in this project is Korean.
*   **Implementation Standards:**
    *   **Source Code Comments:** Must be written in **English**.
    *   **UI Text (CLI Output, Logs, Error Messages):** Must be written in **English**.

## Project Overview

The system consists of two main components:

1.  **kkachi-server**: A central server that:
    *   Manages local clones of documentation repositories.
    *   Provides REST APIs to query the current HEAD, register workspaces, and push document changes.
    *   Maintains the state of all registered workspaces and projects.
    *   Handles the actual Git operations (commit, push) to the central documentation repository.

2.  **kkachi CLI**: A command-line tool used by developers in their local workspaces.
    *   Integrates via Git hooks (`pre-commit`, `pre-push`, etc.) to automate synchronization.
    *   Detects "outdated" documentation states and facilitates 3-way merges.
    *   Communicates with `kkachi-server` to fetch snapshots and push changes.

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
│   └── kkachi/           # CLI entry point (planned)
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
go run cmd/server/main.go
```

### Testing

Run all tests:

```bash
go test ./...
```

## Key Documentation

*   **Requirements:** `docs/requirement-v1.md` - Detailed system behavior and protocol specifications.
