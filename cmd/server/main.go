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
	gitManager := git.NewDocsRepoManager(gitClient)
	docsRepo := git.NewGitDocsRepository(gitClient, stateRepo)

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
	deleteWorkspaceUC := workspace.NewDeleteWorkspaceUseCase(stateRepo)
	registerWorkspaceUC := workspace.NewRegisterWorkspaceUseCase(docsRepo, workspaceRepo, stateRepo, workspace.RealClock{})

	// Handlers
	projectHandler := handler.NewProjectHandler(deleteProjectUC, addProjectUC)
	workspaceHandler := handler.NewWorkspaceHandler(deleteWorkspaceUC, registerWorkspaceUC)
	docsHeadHandler := handler.NewDocsHeadHandler(getDocsHeadUC)

	srv := http.NewHTTPServer(addr, projectHandler, workspaceHandler, docsHeadHandler)
	log.Printf("Starting server on %s", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
