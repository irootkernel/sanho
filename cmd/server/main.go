package main

import (
	"context"
	"log"
	"os"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/infra/git"
	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/docs"
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
