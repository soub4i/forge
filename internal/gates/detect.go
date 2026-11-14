package gates

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/util"
)

// CommandSpec is a resolved command plus where it came from.
type CommandSpec struct {
	Command   string `json:"command"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
	Optional  bool   `json:"optional,omitempty"`
	Source    string `json:"source,omitempty"`
}

// Detected is the result of language and command auto-detection.
type Detected struct {
	Language       string
	PackageManager string
	PackageJSON    map[string]any
	Scripts        map[string]any
	Commands       map[string]*CommandSpec
}

// Resolved is the final command set for a run.
type Resolved struct {
	Commands map[string]*CommandSpec
	Sources  map[string]string
	Detected Detected
}

var commandKeys = []string{"install", "test", "coverage", "lint", "typecheck", "build", "benchmark"}

var makeTargetPattern = regexp.MustCompile(`^([A-Za-z0-9_.-]+):`)

func existsAt(cwd, name string) bool {
	return util.FileExists(filepath.Join(cwd, name))
}

func readPackage(cwd string) map[string]any {
	var pkg map[string]any
	if err := util.ReadJSON(filepath.Join(cwd, "package.json"), &pkg); err != nil {
		return nil
	}
	return pkg
}

func packageManagerFor(cwd string) string {
	if existsAt(cwd, "pnpm-lock.yaml") {
		return "pnpm"
	}
	if existsAt(cwd, "yarn.lock") {
		return "yarn"
	}
	if existsAt(cwd, "bun.lockb") || existsAt(cwd, "bun.lock") {
		return "bun"
	}
	return "npm"
}

func installCommand(cwd string, pkg map[string]any) *CommandSpec {
	manager := packageManagerFor(cwd)
	hasDeps := false
	for _, key := range []string{"dependencies", "devDependencies"} {
		if deps, ok := pkg[key].(map[string]any); ok && len(deps) > 0 {
			hasDeps = true
		}
	}
	switch {
	case manager == "pnpm":
		return &CommandSpec{Command: "pnpm install --frozen-lockfile", Source: "lockfile"}
	case manager == "yarn":
		return &CommandSpec{Command: "yarn install --frozen-lockfile", Source: "lockfile"}
	case manager == "bun":
		return &CommandSpec{Command: "bun install --frozen-lockfile", Source: "lockfile"}
	case existsAt(cwd, "package-lock.json"):
		return &CommandSpec{Command: "npm ci", Source: "lockfile"}
	case hasDeps:
		return &CommandSpec{Command: "npm install --no-audit --no-fund", Source: "package.json"}
	}
	return nil
}

func detectNode(cwd string, pkg map[string]any) map[string]*CommandSpec {
	commands := map[string]*CommandSpec{}
	scripts, _ := pkg["scripts"].(map[string]any)
	manager := packageManagerFor(cwd)
	run := func(script string) string {
		switch manager {
		case "pnpm":
			return "pnpm run " + script
		case "yarn":
			return "yarn " + script
		case "bun":
			return "bun run " + script
		}
		return "npm run " + script
	}
	scriptValue := func(name string) string {
		value, _ := scripts[name].(string)
		return strings.TrimSpace(value)
	}

	if install := installCommand(cwd, pkg); install != nil {
		commands["install"] = install
	}
	if scriptValue("test") != "" {
		commands["test"] = &CommandSpec{Command: run("test"), Source: "package.json#scripts.test"}
	}
	if scriptValue("test:coverage") != "" {
		commands["coverage"] = &CommandSpec{Command: run("test:coverage"), Source: "package.json#scripts.test:coverage"}
	} else if scriptValue("test") == "" && util.DirExists(filepath.Join(cwd, "test")) {
		commands["test"] = &CommandSpec{Command: "node --test", Source: "node:test"}
		commands["coverage"] = &CommandSpec{Command: "node --test --experimental-test-coverage", Source: "node:test"}
	}
	if scriptValue("lint") != "" {
		commands["lint"] = &CommandSpec{Command: run("lint"), Source: "package.json#scripts.lint"}
	}
	if scriptValue("typecheck") != "" {
		commands["typecheck"] = &CommandSpec{Command: run("typecheck"), Source: "package.json#scripts.typecheck"}
	}
	if scriptValue("type-check") != "" {
		commands["typecheck"] = &CommandSpec{Command: run("type-check"), Source: "package.json#scripts.type-check"}
	}
	if scriptValue("build") != "" {
		commands["build"] = &CommandSpec{Command: run("build"), Source: "package.json#scripts.build"}
	}
	if scriptValue("bench") != "" {
		commands["benchmark"] = &CommandSpec{Command: run("bench"), Source: "package.json#scripts.bench"}
	}
	if scriptValue("benchmark") != "" {
		commands["benchmark"] = &CommandSpec{Command: run("benchmark"), Source: "package.json#scripts.benchmark"}
	}
	return commands
}

func detectPython(cwd string) map[string]*CommandSpec {
	commands := map[string]*CommandSpec{}
	hasPyproject := existsAt(cwd, "pyproject.toml")
	hasRequirements := existsAt(cwd, "requirements.txt")
	hasTests := util.DirExists(filepath.Join(cwd, "tests")) || util.DirExists(filepath.Join(cwd, "test"))
	pyproject := strings.ToLower(util.ReadText(filepath.Join(cwd, "pyproject.toml")))
	usesPytest := hasTests || strings.Contains(pyproject, "pytest")
	if usesPytest {
		commands["test"] = &CommandSpec{Command: "python3 -m pytest -q", Source: "pytest"}
	}
	if usesPytest && util.Which("pytest") {
		if util.Which("coverage") || strings.Contains(pyproject, "pytest-cov") {
			commands["coverage"] = &CommandSpec{Command: "python3 -m pytest --cov --cov-report=term-missing -q", Source: "pytest-cov"}
		}
	}
	if util.Which("ruff") {
		commands["lint"] = &CommandSpec{Command: "ruff check .", Source: "ruff"}
	}
	if util.Which("mypy") {
		commands["typecheck"] = &CommandSpec{Command: "mypy .", Source: "mypy"}
	}
	if hasPyproject && util.Which("python3") {
		commands["build"] = &CommandSpec{Command: "python3 -m build", Source: "pyproject.toml"}
	}
	if hasRequirements {
		commands["install"] = &CommandSpec{Command: "python3 -m pip install -r requirements.txt", Source: "requirements.txt"}
	} else if hasPyproject {
		commands["install"] = &CommandSpec{Command: "python3 -m pip install -e .", Source: "pyproject.toml"}
	}
	return commands
}

func detectGo() map[string]*CommandSpec {
	return map[string]*CommandSpec{
		"test":      {Command: "go test ./...", Source: "go.mod"},
		"coverage":  {Command: "go test -cover ./...", Source: "go.mod"},
		"build":     {Command: "go build ./...", Source: "go.mod"},
		"lint":      {Command: "go vet ./...", Source: "go.mod"},
		"install":   {Command: "go mod download", Source: "go.mod"},
		"benchmark": {Command: `go test -bench . -run "^$" ./...`, Source: "go.mod"},
	}
}

func detectRust() map[string]*CommandSpec {
	commands := map[string]*CommandSpec{
		"test":      {Command: "cargo test", Source: "Cargo.toml"},
		"build":     {Command: "cargo build", Source: "Cargo.toml"},
		"benchmark": {Command: "cargo bench", Source: "Cargo.toml"},
	}
	if util.Which("cargo-clippy") {
		commands["lint"] = &CommandSpec{Command: "cargo clippy -- -D warnings", Source: "clippy"}
	}
	return commands
}

func detectMakefile(cwd string) map[string]*CommandSpec {
	if !existsAt(cwd, "Makefile") {
		return map[string]*CommandSpec{}
	}
	targets := map[string]bool{}
	for _, line := range strings.Split(util.ReadText(filepath.Join(cwd, "Makefile")), "\n") {
		if match := makeTargetPattern.FindStringSubmatch(strings.TrimRight(line, "\r")); len(match) == 2 {
			targets[match[1]] = true
		}
	}
	commands := map[string]*CommandSpec{}
	if targets["test"] {
		commands["test"] = &CommandSpec{Command: "make test", Source: "Makefile"}
	}
	if targets["coverage"] {
		commands["coverage"] = &CommandSpec{Command: "make coverage", Source: "Makefile"}
	}
	if targets["lint"] {
		commands["lint"] = &CommandSpec{Command: "make lint", Source: "Makefile"}
	}
	if targets["build"] {
		commands["build"] = &CommandSpec{Command: "make build", Source: "Makefile"}
	}
	if targets["bench"] {
		commands["benchmark"] = &CommandSpec{Command: "make bench", Source: "Makefile"}
	}
	return commands
}

// DetectProject identifies the language and the commands a harness should run.
func DetectProject(cwd string) Detected {
	pkg := readPackage(cwd)
	detected := Detected{Language: "unknown", Commands: map[string]*CommandSpec{}}
	if pkg != nil {
		detected.Language = "node"
		detected.PackageManager = packageManagerFor(cwd)
		detected.PackageJSON = pkg
		if scripts, ok := pkg["scripts"].(map[string]any); ok {
			detected.Scripts = scripts
		}
		detected.Commands = detectNode(cwd, pkg)
	} else if existsAt(cwd, "go.mod") {
		detected.Language = "go"
		detected.Commands = detectGo()
	} else if existsAt(cwd, "Cargo.toml") {
		detected.Language = "rust"
		detected.Commands = detectRust()
	} else if existsAt(cwd, "pyproject.toml") || existsAt(cwd, "requirements.txt") || existsAt(cwd, "setup.py") {
		detected.Language = "python"
		detected.Commands = detectPython(cwd)
	}
	for key, spec := range detectMakefile(cwd) {
		if detected.Commands[key] == nil {
			detected.Commands[key] = spec
		}
	}
	return detected
}

// ResolveCommands merges explicit config with auto-detection. Config wins.
func ResolveCommands(cfg config.Config, cwd string) Resolved {
	detected := DetectProject(cwd)
	resolved := Resolved{
		Commands: map[string]*CommandSpec{},
		Sources:  map[string]string{},
		Detected: detected,
	}
	explicit := cfg.Commands.All()
	for _, key := range commandKeys {
		if spec := explicit[key]; spec != nil && strings.TrimSpace(spec.Command) != "" {
			resolved.Commands[key] = &CommandSpec{
				Command:   spec.Command,
				TimeoutMs: spec.TimeoutMs,
				Optional:  spec.Optional,
				Source:    "config",
			}
			resolved.Sources[key] = "config"
			continue
		}
		if automatic := detected.Commands[key]; automatic != nil {
			copied := *automatic
			if copied.Source == "" {
				copied.Source = "detected"
			}
			resolved.Commands[key] = &copied
			resolved.Sources[key] = copied.Source
			continue
		}
		resolved.Sources[key] = "missing"
	}
	return resolved
}

// CommandLabel renders a resolved command for humans.
func CommandLabel(spec *CommandSpec) string {
	if spec == nil {
		return "(none)"
	}
	return spec.Command
}

// CommandsToJSON maps command names to their shell strings.
func CommandsToJSON(resolved Resolved) map[string]string {
	out := map[string]string{}
	for key, spec := range resolved.Commands {
		out[key] = spec.Command
	}
	return out
}

// DetectedCommandsToJSON maps detected command names to their shell strings.
func DetectedCommandsToJSON(detected Detected) map[string]string {
	out := map[string]string{}
	for key, spec := range detected.Commands {
		out[key] = spec.Command
	}
	return out
}

// MarshalDetection is used by `forge init` output.
func MarshalDetection(detected Detected) string {
	data, _ := json.Marshal(detected)
	return string(data)
}
