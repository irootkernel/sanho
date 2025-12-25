package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/domain/guardrail"
	"github.com/SeventeenthEarth/kkachi/internal/infra/fs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/git"
	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
	"github.com/SeventeenthEarth/kkachi/internal/pty"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/docs"
	guardrailuc "github.com/SeventeenthEarth/kkachi/internal/usecase/guardrail"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/project"
	stateuc "github.com/SeventeenthEarth/kkachi/internal/usecase/state"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/workspace"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "5789"
	}
	addr := ":" + port

	// Resolve web distribution directory
	webDistDir := config.ResolveWebDistDir(os.Getenv("WEB_DIST_DIR"))

	statePath, err := config.ResolveStatePath(os.Getenv("STATE_FILE_PATH"))
	if err != nil {
		log.Fatalf("Failed to resolve state path: %v", err)
	}

	// Infra
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		log.Fatalf("Failed to init state repo: %v", err)
	}

	gitClient := git.NewClient()
	gitManager := git.NewDocsRepoManager(gitClient)
	docsRepo := git.NewGitDocsRepository(gitClient, stateRepo)

	// Initial Sync (P0-3)
	log.Println("Syncing docs repos...")
	if err := gitManager.Sync(context.Background(), stateRepo.ListDocsRepos()); err != nil {
		log.Fatalf("Error: Initial sync failed: %v", err)
	}

	// Repositories
	workspaceRepo := state.NewFileWorkspaceRepository(stateRepo)

	// Mutex Manager (Phase 4)
	mutexManager := docs.NewInMemoryMutexManager()

	// Usecases
	deleteProjectUC := project.NewDeleteProjectUseCase(stateRepo, gitManager)
	addProjectUC := project.NewAddProjectUseCase(stateRepo, gitManager)
	getDocsHeadUC := docs.NewGetDocsHeadUseCase(docsRepo)
	getDocsSnapshotUC := docs.NewGetDocsSnapshotUseCase(docsRepo)
	deleteWorkspaceUC := workspace.NewDeleteWorkspaceUseCase(stateRepo)
	registerWorkspaceUC := workspace.NewRegisterWorkspaceUseCase(docsRepo, workspaceRepo, stateRepo, workspace.RealClock{})
	pushDocsUC := docs.NewPushDocsUseCase(workspaceRepo, docsRepo, mutexManager)
	getStateUC := stateuc.NewGetStateUseCase(docsRepo, workspaceRepo, stateRepo)

	// Handlers
	projectHandler := handler.NewProjectHandler(deleteProjectUC, addProjectUC)
	workspaceHandler := handler.NewWorkspaceHandler(deleteWorkspaceUC, registerWorkspaceUC)
	docsHeadHandler := handler.NewDocsHeadHandler(getDocsHeadUC)
	docsSnapshotHandler := handler.NewDocsSnapshotHandler(getDocsSnapshotUC)
	docsPushHandler := handler.NewDocsPushHandler(pushDocsUC)
	stateHandler := handler.NewStateHandler(getStateUC)

	// PTY (v3)
	ptyConfig := pty.LoadConfigFromEnv()

	// Load Security Rules for Guardrail (STASK-4)
	securityLoader := fs.NewFileSecurityLoader("config/security_rules.yaml")
	var guardrailInstance guardrail.Guardrail
	securityCfg, err := securityLoader.Load()
	if err != nil {
		log.Printf("Warning: Failed to load security rules: %v. Guardrail will be disabled.", err)
	} else {
		matcher, err := guardrailuc.NewRegexMatcher(securityCfg.Blacklist)
		if err != nil {
			log.Printf("Warning: Failed to initialize guardrail: %v. Guardrail will be disabled.", err)
		} else {
			guardrailInstance = matcher
		}
	}

	// Auth Config (STASK-5)
	authConfig := config.LoadAuthConfigFromEnv()
	if authConfig.AuthEnabled {
		if authConfig.AuthToken == "" {
			slog.Error("Critical: AUTH_TOKEN must be set when AUTH_ENABLED is true")
			os.Exit(1)
		}
		slog.Info("Authentication enabled")
	} else {
		slog.Info("Authentication disabled")
	}

	sessionManager := pty.NewSessionManager(guardrailInstance)
	ptyHandler := handler.NewPTYHandler(sessionManager, workspaceRepo, ptyConfig, authConfig)

	// Server configuration
	serverCfg := http.ServerConfig{
		Addr:       addr,
		WebDistDir: webDistDir,
		AuthConfig: authConfig,
	}

	srv := http.NewHTTPServer(serverCfg, projectHandler, workspaceHandler, docsHeadHandler, docsSnapshotHandler, docsPushHandler, stateHandler, ptyHandler)
	log.Printf("Starting server on %s (web dist: %s)", addr, webDistDir)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
