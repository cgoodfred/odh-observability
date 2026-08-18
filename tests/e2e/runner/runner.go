package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	prefix              = "[e2e-run] "
	defaultTest2JSONBin = "/usr/local/bin/test2json"
	defaultArtifactsDir = "/artifacts"
	defaultResultsDir   = "e2e-results"
	defaultGotestsumBin = "gotestsum"
)

// TestPackages is set at build time via -ldflags.
// Format: "name=binary,name2=binary2".
//
//nolint:gochecknoglobals // set via ldflags at build time
var TestPackages string

type TestPackage struct {
	Name   string
	Binary string
}

func ParsePackages(s string) ([]TestPackage, error) {
	if s == "" {
		return nil, errors.New("testPackages not set (must be set via -ldflags at build time)")
	}
	pairs := strings.Split(s, ",")
	pkgs := make([]TestPackage, 0, len(pairs))
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid package spec %q: expected name=binary", pair)
		}
		pkgs = append(pkgs, TestPackage{Name: parts[0], Binary: parts[1]})
	}
	return pkgs, nil
}

type Result struct {
	JUnitFile string
	JSONFile  string
	ExitCode  int
}

type Runner struct {
	stdout   io.Writer
	stderr   io.Writer
	selfPath string
	packages []TestPackage
}

func New() *Runner {
	return &Runner{stdout: os.Stdout, stderr: os.Stderr}
}

func (r *Runner) WithStdout(w io.Writer) *Runner {
	r.stdout = w
	return r
}

func (r *Runner) WithStderr(w io.Writer) *Runner {
	r.stderr = w
	return r
}

func (r *Runner) WithSelf(path string) *Runner {
	r.selfPath = path
	return r
}

func (r *Runner) WithPackages(pkgs []TestPackage) *Runner {
	r.packages = pkgs
	return r
}

// Run routes between orchestrator and executor modes.
// When args[0] is "exec", it streams test2json output for gotestsum.
// Otherwise it launches gotestsum with --raw-command pointing back at itself.
func (r *Runner) Run(args []string) Result {
	if err := r.initPackages(); err != nil {
		r.logf("invalid testPackages: %v", err)
		return Result{ExitCode: 1}
	}

	if len(args) > 0 && args[0] == "exec" {
		return Result{ExitCode: r.execPackages(args[1:])}
	}

	return r.orchestrate(args)
}

func (r *Runner) initPackages() error {
	if r.packages != nil {
		return nil
	}
	pkgs, err := ParsePackages(TestPackages)
	if err != nil {
		return err
	}
	r.packages = pkgs
	return nil
}

func (r *Runner) orchestrate(args []string) Result {
	artifactsDir := envOr("ARTIFACTS", defaultArtifactsDir)
	resultsDir := filepath.Join(artifactsDir, envOr("E2E_RESULTS_DIR", defaultResultsDir))
	gotestsumBin := envOr("E2E_GOTESTSUM_BIN", defaultGotestsumBin)
	goTestVerbosity := envOr("GO_TEST_VERBOSITY", "testname")
	junitProjectName := envOr("E2E_JUNIT_PROJECT_NAME", "odh-observability")

	junitFile := filepath.Join(resultsDir, "junit.xml")
	jsonFile := filepath.Join(resultsDir, "log.jsonl")

	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		r.logf("failed to create results dir %s: %v", resultsDir, err)
		return Result{ExitCode: 1}
	}

	selfPath := r.selfPath
	if selfPath == "" {
		selfPath = os.Args[0]
	}

	// Translate env vars to test flags for container-friendly invocation.
	envFlags := r.envToFlags()

	cmdArgs := make([]string, 0, 10+len(args)+len(envFlags))
	cmdArgs = append(cmdArgs,
		"--raw-command",
		"--junitfile", junitFile,
		"--junitfile-project-name", junitProjectName,
		"--jsonfile", jsonFile,
		"--format", goTestVerbosity,
		"--",
		selfPath, "exec",
	)
	cmdArgs = append(cmdArgs, envFlags...)
	cmdArgs = append(cmdArgs, args...)

	r.logf("running: %s %s", gotestsumBin, strings.Join(cmdArgs, " "))

	cmd := exec.CommandContext(context.Background(), gotestsumBin, cmdArgs...) //nolint:gosec // args are constructed internally, not from user input
	cmd.Stdout = r.stdout
	cmd.Stderr = r.stderr
	cmd.Stdin = os.Stdin

	result := Result{JUnitFile: junitFile, JSONFile: jsonFile}

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			r.logf("junit: %s", junitFile)
			r.logf("jsonl: %s", jsonFile)
			return result
		}
		r.logf("failed to run gotestsum: %v", err)
		result.ExitCode = 1
		return result
	}

	r.logf("junit: %s", junitFile)
	r.logf("jsonl: %s", jsonFile)
	return result
}

func (r *Runner) execPackages(args []string) int {
	test2jsonBin := envOr("E2E_TEST2JSON_BIN", defaultTest2JSONBin)
	testCount := envOr("E2E_COUNT", "1")

	testArgs := make([]string, 0, 2+len(args))
	testArgs = append(testArgs, "-test.count="+testCount, "-test.v=test2json")
	testArgs = append(testArgs, args...)

	worstCode := 0

	for _, pkg := range r.packages {
		cmdArgs := make([]string, 0, 4+len(testArgs))
		cmdArgs = append(cmdArgs, "-t", "-p", pkg.Name, pkg.Binary)
		cmdArgs = append(cmdArgs, testArgs...)

		r.logf("running: %s %s", test2jsonBin, strings.Join(cmdArgs, " "))

		cmd := exec.CommandContext(context.Background(), test2jsonBin, cmdArgs...)
		cmd.Stdout = r.stdout
		cmd.Stderr = r.stderr

		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				if code := exitErr.ExitCode(); code > worstCode {
					worstCode = code
				}
			} else {
				r.logf("failed to run test2json for package %s: %v", pkg.Name, err)
				if worstCode == 0 {
					worstCode = 1
				}
			}
		}
	}

	return worstCode
}

// envToFlags translates E2E_TEST_* environment variables into test binary flags.
// This preserves the container-friendly env var interface from `podman run -e`.
func (r *Runner) envToFlags() []string {
	envMap := map[string]string{
		"E2E_TEST_MONITORING_NAMESPACE":       "-monitoring-namespace",
		"E2E_TEST_MONITORING_CR_NAME":         "-monitoring-cr-name",
		"E2E_TEST_INSTALL_OPERATORS":          "-install-operators",
		"E2E_TEST_API_MODE":                   "-api-mode",
		"E2E_TEST_DSCI_CR_NAME":               "-dsci-cr-name",
		"E2E_TEST_EVENTUALLY_TIMEOUT":         "-eventually-timeout",
		"E2E_TEST_EVENTUALLY_POLL_INTERVAL":   "-eventually-poll-interval",
		"E2E_TEST_CONSISTENTLY_TIMEOUT":       "-consistently-timeout",
		"E2E_TEST_CONSISTENTLY_POLL_INTERVAL": "-consistently-poll-interval",
		"E2E_TEST_OLM_TIMEOUT":                "-olm-timeout",
	}

	var flags []string
	for env, flag := range envMap {
		if v := os.Getenv(env); v != "" {
			flags = append(flags, flag+"="+v)
		}
	}
	return flags
}

func (r *Runner) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(r.stderr, prefix+format+"\n", args...)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
