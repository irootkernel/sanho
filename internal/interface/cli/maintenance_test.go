package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCloneMaintenanceFailureIsVerboseOnly(t *testing.T) {
	previous := verbose
	t.Cleanup(func() { verbose = previous })

	cmd := &cobra.Command{}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	err := errors.New("gc unavailable")

	verbose = false
	reportCloneMaintenance(cmd, err)
	if stderr.Len() != 0 {
		t.Fatalf("non-verbose diagnostic = %q, want silence", stderr.String())
	}

	verbose = true
	reportCloneMaintenance(cmd, err)
	if got := stderr.String(); !strings.Contains(got, "private canonical clone maintenance skipped: gc unavailable") {
		t.Fatalf("verbose diagnostic = %q", got)
	}
}
