package awake

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner makes all external system probes replaceable in unit tests.
type Runner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}
type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return b, fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(b)))
	}
	return b, nil
}
