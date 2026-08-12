package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type securityCommand struct {
	name string
	args []string
}

func TestGosec(t *testing.T) {
	t.Helper()

	tool, err := locateGosecCommand()
	if err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	args := append([]string{}, tool.args...)
	args = append(args, "./...")
	cmd := exec.CommandContext(ctx, tool.name, args...)
	cmd.Dir = wd
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("gosec timed out: %s", strings.TrimSpace(string(output)))
	}
	if err != nil {
		t.Fatalf("gosec failed: %v\n%s", err, strings.TrimSpace(string(output)))
	}
}

func TestGovulncheck(t *testing.T) {
	t.Helper()

	tool, err := locateGovulncheckCommand()
	if err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	args := append([]string{}, tool.args...)
	args = append(args, "./...")
	cmd := exec.CommandContext(ctx, tool.name, args...)
	cmd.Dir = wd
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("govulncheck timed out: %s", strings.TrimSpace(string(output)))
	}
	if err != nil {
		t.Fatalf("govulncheck failed: %v\n%s", err, strings.TrimSpace(string(output)))
	}
}

func locateGosecCommand() (securityCommand, error) {
	if configured := strings.TrimSpace(os.Getenv("GOSEC_BIN")); configured != "" {
		return securityCommand{name: configured}, nil
	}
	if bin, err := exec.LookPath("gosec"); err == nil {
		return securityCommand{name: bin}, nil
	}

	if bin, ok := locateGopathBinary("gosec"); ok {
		return securityCommand{name: bin}, nil
	}
	return moduleToolCommand("github.com/securego/gosec/v2/cmd/gosec")
}

func locateGovulncheckCommand() (securityCommand, error) {
	if configured := strings.TrimSpace(os.Getenv("GOVULNCHECK_BIN")); configured != "" {
		return securityCommand{name: configured}, nil
	}
	if bin, err := exec.LookPath("govulncheck"); err == nil {
		return securityCommand{name: bin}, nil
	}

	if bin, ok := locateGopathBinary("govulncheck"); ok {
		return securityCommand{name: bin}, nil
	}
	return moduleToolCommand("golang.org/x/vuln/cmd/govulncheck")
}

func locateGopathBinary(name string) (string, bool) {
	gopathCmd := exec.Command("go", "env", "GOPATH")
	raw, err := gopathCmd.Output()
	if err != nil {
		return "", false
	}
	gopath := strings.TrimSpace(string(raw))
	if gopath == "" {
		return "", false
	}

	bin := filepath.Join(gopath, "bin", name)
	if _, err := os.Stat(bin); err != nil {
		return "", false
	}
	return bin, true
}

func moduleToolCommand(importPath string) (securityCommand, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return securityCommand{}, fmt.Errorf("go command not found; install %s or set the matching *_BIN environment variable", importPath)
	}
	return securityCommand{name: goBin, args: []string{"run", importPath}}, nil
}
