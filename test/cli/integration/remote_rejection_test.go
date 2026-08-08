package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalServerRejectionBlocksAppPushAndSamePushRetries(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	canonicalBefore := w.canonicalHead()

	hook := filepath.Join(w.origin, "hooks", "pre-receive")
	writeFile(t, hook, "#!/bin/sh\necho 'protected branch rejects this publication' >&2\nexit 1\n")
	if err := os.Chmod(hook, 0755); err != nil {
		t.Fatalf("make canonical pre-receive executable: %v", err)
	}

	w.commitDocs("docs: blocked by canonical policy", map[string]string{"api.md": "candidate\n"})
	blocked := w.push()
	if blocked.exitCode == 0 {
		t.Fatal("application push succeeded despite canonical server rejection")
	}
	requireContains(t, "wrapped rejection", blocked.combined(), "sanho: canonical repository unreachable")
	requireContains(t, "server cause", blocked.combined(), "remote rejected")
	requireContains(t, "server cause", blocked.combined(), "pre-receive hook declined")
	requireContains(t, "atomic app refusal", blocked.combined(), "no remote ref was changed")
	requireNotContains(t, "wrapped rejection", blocked.combined(), "exit status 1")
	if got := w.canonicalHead(); got != canonicalBefore {
		t.Fatalf("canonical head = %s, want unchanged %s", got, canonicalBefore)
	}
	if appRemote := w.gitExit(w.codeOrigin, "rev-parse", "--verify", "refs/heads/main"); appRemote.exitCode == 0 {
		t.Fatalf("application remote main moved to %s", strings.TrimSpace(appRemote.stdout))
	}

	if err := os.Rename(hook, hook+".disabled"); err != nil {
		t.Fatalf("disable canonical pre-receive hook: %v", err)
	}
	retried := w.push()
	if retried.exitCode != 0 {
		t.Fatalf("same push failed after policy removal with exit %d\n%s", retried.exitCode, retried.combined())
	}
	if got := w.canonicalFile(w.canonicalHead(), "api.md"); got != "candidate\n" {
		t.Fatalf("canonical api.md = %q, want candidate", got)
	}
	wantAppHead := strings.TrimSpace(w.git(w.app, "rev-parse", "HEAD").stdout)
	gotAppHead := strings.TrimSpace(w.git(w.codeOrigin, "rev-parse", "refs/heads/main").stdout)
	if gotAppHead != wantAppHead {
		t.Fatalf("application remote main = %s, want %s", gotAppHead, wantAppHead)
	}
}
