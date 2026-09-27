package gitx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectionDerivedRunnerDrainsAndCountsBoundedOutput(t *testing.T) {
	dir := newFixtureRepo(t)
	content := strings.Repeat("content\n", 10000)
	written, err := New(dir).RunWithStdin(context.Background(), strings.NewReader(content), "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewInspection(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.WithOptions(WithStdoutLimit(8)).Run(context.Background(), "cat-file", "blob", strings.TrimSpace(string(written.Stdout)))
	if err != nil || string(result.Stdout) != content[:8] || !result.StdoutTruncated || result.StdoutBytes != int64(len(content)) {
		t.Fatalf("bounded capture=%d/%d truncated=%t error=%v", len(result.Stdout), result.StdoutBytes, result.StdoutTruncated, err)
	}
}

func TestInspectionUnavailableWhenGitRejectsRequiredControl(t *testing.T) {
	bin := t.TempDir()
	// Inject a process capability failure, without replacing Git semantics in
	// any repository-content test.
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexit 129\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if _, err := NewInspection(context.Background(), t.TempDir()); !errors.Is(err, ErrInspectionPolicyUnavailable) {
		t.Fatalf("unsupported policy=%v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := NewInspection(context.Background(), t.TempDir()); err == nil || errors.Is(err, ErrInspectionPolicyUnavailable) {
		t.Fatalf("launch failure must remain an execution error: %v", err)
	}
}

func TestInspectionDerivedRunnerOverridesExecutionConfiguration(t *testing.T) {
	dir := newFixtureRepo(t)
	ctx := context.Background()
	settings := map[string]string{
		"core.fsmonitor": "false", "core.untrackedCache": "false",
		"core.splitIndex": "false", "maintenance.auto": "false",
		"gc.auto": "0", "submodule.recurse": "false", "core.hooksPath": os.DevNull,
	}
	for key := range settings {
		if _, err := New(dir).Run(ctx, "config", key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	runner, err := NewInspection(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	derived := runner.WithOptions(WithEnv("GIT_INDEX_FILE=" + filepath.Join(t.TempDir(), "index")))
	for key, want := range settings {
		if got, err := derived.Line(ctx, "config", "--get", key); err != nil || got != want {
			t.Fatalf("effective %s=%q, want %q: %v", key, got, want, err)
		}
		if got, err := New(dir).Line(ctx, "config", "--get", key); err != nil || got != "true" {
			t.Fatalf("persisted %s changed to %q: %v", key, got, err)
		}
	}
}
