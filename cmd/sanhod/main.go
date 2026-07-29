package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/irootkernel/sanho/internal/buildinfo"
	"github.com/irootkernel/sanho/internal/config"
	"github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/state"
	sanhohttp "github.com/irootkernel/sanho/internal/interface/http"
	"github.com/irootkernel/sanho/internal/interface/http/handler"
	"github.com/irootkernel/sanho/internal/usecase/docs"
	"github.com/irootkernel/sanho/internal/usecase/project"
	stateuc "github.com/irootkernel/sanho/internal/usecase/state"
	"github.com/irootkernel/sanho/internal/usecase/workspace"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("sanhod", flag.ContinueOnError)
	flags.SetOutput(stdout)
	homeDir := flags.String("home", os.Getenv("SANHO_HOME"), "Sanho runtime home directory")
	socketPath := flags.String("socket", os.Getenv("SANHO_SOCKET"), "Unix socket path")
	showVersion := flags.Bool("version", false, "Print sanhod version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		_, err := fmt.Fprintf(stdout, "sanhod version %s\n", buildinfo.ResolveVersion(version))
		return err
	}

	paths, err := config.ResolveRuntimePaths(*homeDir, *socketPath)
	if err != nil {
		return fmt.Errorf("resolve runtime paths: %w", err)
	}
	if err := config.PrepareRuntime(paths); err != nil {
		return err
	}

	// Infra
	stateRepo, err := state.NewFileStateRepository(paths.StatePath)
	if err != nil {
		return fmt.Errorf("init state repository: %w", err)
	}

	gitClient := git.NewClient()
	repoCoordinator := git.NewRepoCoordinator()
	gitManager := git.NewDocsRepoManager(gitClient, repoCoordinator)
	docsRepo := git.NewGitDocsRepository(gitClient, stateRepo, repoCoordinator)

	// Initial Sync (P0-3)
	log.Println("Syncing docs repos...")
	if err := gitManager.Sync(context.Background(), stateRepo.ListDocsRepos()); err != nil {
		return fmt.Errorf("initial docs sync failed: %w", err)
	}

	// Repositories
	workspaceRepo := state.NewFileWorkspaceRepository(stateRepo)

	// Usecases
	deleteProjectUC := project.NewDeleteProjectUseCase(stateRepo, gitManager)
	addProjectUC := project.NewAddProjectUseCase(stateRepo, gitManager, paths.DocsReposDir)
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

	srv := sanhohttp.NewHTTPServer(projectHandler, workspaceHandler, docsHeadHandler, docsSnapshotHandler, docsPushHandler, stateHandler, projectStatusHandler)
	listener, err := sanhohttp.ListenUnix(paths.SocketPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = listener.Close()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(listener)
	}()

	log.Printf("sanhod listening on %s", paths.SocketPath)
	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
