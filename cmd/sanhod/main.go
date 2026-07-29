package main

import (
	"context"
	"log"
	"os"

	"github.com/irootkernel/sanho/internal/config"
	"github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/state"
	"github.com/irootkernel/sanho/internal/interface/http"
	"github.com/irootkernel/sanho/internal/interface/http/handler"
	"github.com/irootkernel/sanho/internal/usecase/docs"
	"github.com/irootkernel/sanho/internal/usecase/project"
	stateuc "github.com/irootkernel/sanho/internal/usecase/state"
	"github.com/irootkernel/sanho/internal/usecase/workspace"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "5789"
	}
	addr := ":" + port

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
	repoCoordinator := git.NewRepoCoordinator()
	gitManager := git.NewDocsRepoManager(gitClient, repoCoordinator)
	docsRepo := git.NewGitDocsRepository(gitClient, stateRepo, repoCoordinator)

	// Initial Sync (P0-3)
	log.Println("Syncing docs repos...")
	if err := gitManager.Sync(context.Background(), stateRepo.ListDocsRepos()); err != nil {
		log.Fatalf("Error: Initial sync failed: %v", err)
	}

	// Repositories
	workspaceRepo := state.NewFileWorkspaceRepository(stateRepo)

	// Usecases
	deleteProjectUC := project.NewDeleteProjectUseCase(stateRepo, gitManager)
	addProjectUC := project.NewAddProjectUseCase(stateRepo, gitManager)
	getDocsHeadUC := docs.NewGetDocsHeadUseCase(docsRepo)
	getDocsSnapshotUC := docs.NewGetDocsSnapshotUseCase(docsRepo)
	deleteWorkspaceUC := workspace.NewDeleteWorkspaceUseCase(stateRepo)
	registerWorkspaceUC := workspace.NewRegisterWorkspaceUseCase(docsRepo, workspaceRepo, stateRepo, workspace.RealClock{})
	reportDocsHashUC := workspace.NewReportDocsHashUseCase(workspaceRepo, docsRepo)
	pushDocsUC := docs.NewPushDocsUseCase(workspaceRepo, docsRepo, repoCoordinator)
	getStateUC := stateuc.NewGetStateUseCase(docsRepo, workspaceRepo, stateRepo)
	getProjectStatusUC := project.NewGetProjectStatusUseCase(workspaceRepo, docsRepo)

	// Handlers
	projectHandler := handler.NewProjectHandler(deleteProjectUC, addProjectUC)
	workspaceHandler := handler.NewWorkspaceHandler(deleteWorkspaceUC, registerWorkspaceUC, reportDocsHashUC)
	docsHeadHandler := handler.NewDocsHeadHandler(getDocsHeadUC)
	docsSnapshotHandler := handler.NewDocsSnapshotHandler(getDocsSnapshotUC)
	docsPushHandler := handler.NewDocsPushHandler(pushDocsUC)
	stateHandler := handler.NewStateHandler(getStateUC)
	projectStatusHandler := handler.NewProjectStatusHandler(getProjectStatusUC)

	serverCfg := http.ServerConfig{
		Addr: addr,
	}

	srv := http.NewHTTPServer(serverCfg, projectHandler, workspaceHandler, docsHeadHandler, docsSnapshotHandler, docsPushHandler, stateHandler, projectStatusHandler)
	log.Printf("Starting server on %s", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
