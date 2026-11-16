package gates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	Language       string                  `json:"language"`
	Languages      []string                `json:"languages,omitempty"`
	PackageManager string                  `json:"packageManager,omitempty"`
	PackageJSON    map[string]any          `json:"packageJson,omitempty"`
	Scripts        map[string]any          `json:"scripts,omitempty"`
	Manifests      []string                `json:"manifests,omitempty"`
	Commands       map[string]*CommandSpec `json:"commands,omitempty"`
}

// Resolved is the final command set for a run.
type Resolved struct {
	Commands map[string]*CommandSpec
	Sources  map[string]string
	Detected Detected
}

var commandKeys = []string{"install", "test", "coverage", "lint", "typecheck", "build", "benchmark"}

// knownManifests are files that identify a project or its build system.
var knownManifests = []string{
	"package.json", "go.mod", "go.work", "Cargo.toml", "pyproject.toml", "requirements.txt",
	"setup.py", "setup.cfg", "Pipfile", "tox.ini", "noxfile.py", "pom.xml", "build.gradle",
	"build.gradle.kts", "settings.gradle", "settings.gradle.kts", "composer.json", "Gemfile",
	"Rakefile", "Package.swift", "pubspec.yaml", "mix.exs", "build.sbt", "CMakeLists.txt",
	"meson.build", "configure.ac", "Makefile", "makefile", "GNUmakefile", "justfile",
	"Justfile", "Taskfile.yml", "Taskfile.yaml", "deno.json", "deno.jsonc", "build.zig",
	"Project.toml", "DESCRIPTION", "global.json", "flake.nix", "WORKSPACE", "WORKSPACE.bazel",
	"MODULE.bazel", "BUILD.bazel", "Earthfile", "tsconfig.json", "jsconfig.json",
	"stack.yaml", "package.yaml", "cabal.project",
	"phpunit.xml", "phpunit.xml.dist", "phpstan.neon", "phpstan.neon.dist", "psalm.xml",
	".rubocop.yml", ".swiftlint.yml", ".clang-tidy", ".clang-format", ".scalafmt.conf",
	".busted", ".luacheckrc", "vcpkg.json",
}

var manifestExtensions = map[string]bool{
	".csproj": true, ".fsproj": true, ".vbproj": true,
	".sln": true, ".slnx": true, ".cabal": true, ".gemspec": true, ".rockspec": true,
}

// sourceLanguage maps a file extension to the ecosystem used for source counting.
var sourceLanguage = map[string]string{
	".js": "node", ".jsx": "node", ".mjs": "node", ".cjs": "node",
	".ts": "node", ".tsx": "node", ".vue": "node", ".svelte": "node",
	".go": "go", ".rs": "rust", ".py": "python", ".pyi": "python",
	".java": "java", ".kt": "java", ".kts": "java", ".groovy": "java",
	".cs": "dotnet", ".fs": "dotnet", ".vb": "dotnet",
	".rb": "ruby", ".rake": "ruby", ".gemspec": "ruby",
	".php": "php",
	".c":   "cpp", ".h": "cpp", ".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp",
	".hpp": "cpp", ".hh": "cpp", ".hxx": "cpp", ".m": "cpp", ".mm": "cpp",
	".swift": "swift", ".dart": "dart", ".ex": "elixir", ".exs": "elixir",
	".scala": "scala", ".sc": "scala", ".hs": "haskell", ".lhs": "haskell",
	".zig": "zig", ".lua": "lua", ".r": "r", ".jl": "julia",
}

// fallbackLanguage labels source-only projects that have no build manifest.
var fallbackLanguage = map[string]string{
	".sh": "shell", ".bash": "shell", ".zsh": "shell", ".ps1": "powershell",
	".sol": "solidity", ".tf": "terraform", ".nix": "nix", ".sql": "sql",
	".html": "web", ".css": "web", ".scss": "web", ".sass": "web", ".less": "web",
	".tex": "latex", ".proto": "protobuf",
}

type projectScan struct {
	cwd   string
	files []util.FileInfo
	set   map[string]bool
}

func newProjectScan(cwd string) *projectScan {
	files, _ := util.WalkFiles(cwd, 4000, 5, 0)
	set := make(map[string]bool, len(files))
	for _, file := range files {
		set[file.Rel] = true
	}
	return &projectScan{cwd: cwd, files: files, set: set}
}

func (s *projectScan) hasFile(rel string) bool { return s.set[rel] }

func (s *projectScan) firstFile(names ...string) string {
	for _, name := range names {
		if s.hasFile(name) {
			return name
		}
	}
	return ""
}

func (s *projectScan) read(rel string) string {
	if rel == "" {
		return ""
	}
	return util.ReadText(filepath.Join(s.cwd, rel))
}

func (s *projectScan) dirExists(rel string) bool {
	return util.DirExists(filepath.Join(s.cwd, rel))
}

func (s *projectScan) executable(rel string) bool {
	info, err := os.Stat(filepath.Join(s.cwd, rel))
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func (s *projectScan) filesWithExt(exts ...string) []string {
	wanted := map[string]bool{}
	for _, ext := range exts {
		wanted[strings.ToLower(ext)] = true
	}
	var out []string
	for _, file := range s.files {
		if wanted[strings.ToLower(filepath.Ext(file.Rel))] {
			out = append(out, file.Rel)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return pathDepth(out[i]) < pathDepth(out[j]) || pathDepth(out[i]) == pathDepth(out[j]) && out[i] < out[j]
	})
	return out
}

func (s *projectScan) manifests() []string {
	var out []string
	for _, name := range knownManifests {
		if s.hasFile(name) {
			out = append(out, name)
		}
	}
	for _, file := range s.files {
		if manifestExtensions[strings.ToLower(filepath.Ext(file.Rel))] && pathDepth(file.Rel) <= 3 {
			out = append(out, file.Rel)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		di, dj := pathDepth(out[i]), pathDepth(out[j])
		if di == dj {
			return out[i] < out[j]
		}
		return di < dj
	})
	return out
}

func (s *projectScan) sourceCounts() map[string]int {
	counts := map[string]int{}
	for _, file := range s.files {
		if language, ok := sourceLanguage[strings.ToLower(filepath.Ext(file.Rel))]; ok {
			counts[language]++
		}
	}
	return counts
}

func (s *projectScan) dominantLanguage() string {
	counts := s.sourceCounts()
	for _, file := range s.files {
		ext := strings.ToLower(filepath.Ext(file.Rel))
		if language, ok := fallbackLanguage[ext]; ok {
			counts[language]++
		}
	}
	best, bestCount := "unknown", 0
	for language, count := range counts {
		if count > bestCount || count == bestCount && language < best {
			best, bestCount = language, count
		}
	}
	return best
}

func pathDepth(rel string) int {
	return strings.Count(filepath.ToSlash(rel), "/")
}

func shellArg(value string) string {
	if value == "" {
		return ""
	}
	if !strings.ContainsAny(value, " \t\"'$`\\") {
		return value
	}
	if filepath.Separator == '\\' {
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func command(value, source string) *CommandSpec {
	return &CommandSpec{Command: value, Source: source}
}

func wrapper(scan *projectScan, name, fallback string) string {
	if filepath.Separator == '\\' {
		if scan.hasFile(name + ".cmd") {
			return name + ".cmd"
		}
		if scan.hasFile(name + ".bat") {
			return name + ".bat"
		}
	} else if scan.executable(name) {
		return "./" + name
	}
	return fallback
}

func readJSONFile(scan *projectScan, rel string) map[string]any {
	var value map[string]any
	if err := util.ReadJSON(filepath.Join(scan.cwd, rel), &value); err != nil {
		return nil
	}
	return value
}

type ecosystemResult struct {
	name     string
	manager  string
	commands map[string]*CommandSpec
	pkg      map[string]any
	scripts  map[string]any
}

type ecosystemDetector struct {
	name   string
	detect func(*projectScan) *ecosystemResult
}

var ecosystemDetectors = []ecosystemDetector{
	{"node", detectNode},
	{"go", detectGo},
	{"rust", detectRust},
	{"python", detectPython},
	{"java", detectJava},
	{"dotnet", detectDotnet},
	{"ruby", detectRuby},
	{"php", detectPHP},
	{"cpp", detectCPP},
	{"swift", detectSwift},
	{"dart", detectDart},
	{"elixir", detectElixir},
	{"scala", detectScala},
	{"haskell", detectHaskell},
	{"zig", detectZig},
	{"deno", detectDeno},
	{"lua", detectLua},
	{"r", detectR},
	{"julia", detectJulia},
}

func detectNode(scan *projectScan) *ecosystemResult {
	pkg := readJSONFile(scan, "package.json")
	if pkg == nil {
		return nil
	}
	scripts, _ := pkg["scripts"].(map[string]any)
	manager := nodePackageManager(scan, pkg)
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
	commands := map[string]*CommandSpec{}

	if install := nodeInstall(scan, pkg, manager); install != nil {
		commands["install"] = install
	}
	if scriptValue("test") != "" {
		commands["test"] = command(run("test"), "package.json#scripts.test")
	}
	for _, name := range []string{"test:coverage", "coverage", "test:cov", "test-coverage"} {
		if scriptValue(name) != "" {
			commands["coverage"] = command(run(name), "package.json#scripts."+name)
			break
		}
	}
	if commands["test"] == nil && (scan.dirExists("test") || scan.dirExists("tests") || scan.dirExists("__tests__")) {
		commands["test"] = command("node --test", "node:test")
		commands["coverage"] = command("node --test --experimental-test-coverage", "node:test")
	}
	if scriptValue("lint") != "" {
		commands["lint"] = command(run("lint"), "package.json#scripts.lint")
	} else if lint := nodeLintFallback(scan, pkg); lint != nil {
		commands["lint"] = lint
	}
	for _, name := range []string{"typecheck", "type-check"} {
		if scriptValue(name) != "" {
			commands["typecheck"] = command(run(name), "package.json#scripts."+name)
			break
		}
	}
	if commands["typecheck"] == nil && scan.hasFile("tsconfig.json") && hasNodeDep(pkg, "typescript") {
		commands["typecheck"] = command("npx --no-install tsc --noEmit", "tsconfig.json")
	}
	if scriptValue("build") != "" {
		commands["build"] = command(run("build"), "package.json#scripts.build")
	}
	for _, name := range []string{"bench", "benchmark", "perf"} {
		if scriptValue(name) != "" {
			commands["benchmark"] = command(run(name), "package.json#scripts."+name)
			break
		}
	}
	return &ecosystemResult{manager: manager, commands: commands, pkg: pkg, scripts: scripts}
}

func nodePackageManager(scan *projectScan, pkg map[string]any) string {
	switch {
	case scan.hasFile("pnpm-lock.yaml"):
		return "pnpm"
	case scan.hasFile("yarn.lock"):
		return "yarn"
	case scan.hasFile("bun.lockb") || scan.hasFile("bun.lock"):
		return "bun"
	}
	if declared, _ := pkg["packageManager"].(string); declared != "" {
		name := strings.ToLower(strings.SplitN(declared, "@", 2)[0])
		switch name {
		case "pnpm", "yarn", "bun", "npm":
			return name
		}
	}
	return "npm"
}

func nodeInstall(scan *projectScan, pkg map[string]any, manager string) *CommandSpec {
	hasDeps := false
	for _, key := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		if deps, ok := pkg[key].(map[string]any); ok && len(deps) > 0 {
			hasDeps = true
		}
	}
	switch manager {
	case "pnpm":
		if scan.hasFile("pnpm-lock.yaml") {
			return command("pnpm install --frozen-lockfile", "pnpm-lock.yaml")
		}
		return command("pnpm install", "pnpm")
	case "yarn":
		if scan.hasFile("yarn.lock") {
			if scan.hasFile(".yarnrc.yml") {
				return command("yarn install --immutable", "yarn.lock")
			}
			return command("yarn install --frozen-lockfile", "yarn.lock")
		}
		return command("yarn install", "yarn")
	case "bun":
		return command("bun install", "bun")
	}
	if scan.hasFile("package-lock.json") {
		return command("npm ci", "package-lock.json")
	}
	if hasDeps {
		return command("npm install --no-audit --no-fund", "package.json")
	}
	return nil
}

func nodeLintFallback(scan *projectScan, pkg map[string]any) *CommandSpec {
	if scan.hasFile("biome.json") || scan.hasFile("biome.jsonc") {
		if hasNodeDep(pkg, "@biomejs/biome") {
			return command("npx --no-install biome check .", "biome.json")
		}
	}
	for _, name := range []string{"eslint.config.js", "eslint.config.mjs", "eslint.config.cjs", "eslint.config.ts", ".eslintrc", ".eslintrc.json", ".eslintrc.js", ".eslintrc.yml", ".eslintrc.yaml"} {
		if scan.hasFile(name) && hasNodeDep(pkg, "eslint") {
			return command("npx --no-install eslint .", name)
		}
	}
	return nil
}

func hasNodeDep(pkg map[string]any, name string) bool {
	for _, key := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		if deps, ok := pkg[key].(map[string]any); ok {
			if _, ok := deps[name]; ok {
				return true
			}
		}
	}
	return false
}

func detectGo(scan *projectScan) *ecosystemResult {
	if !scan.hasFile("go.mod") {
		return nil
	}
	return &ecosystemResult{manager: "go", commands: map[string]*CommandSpec{
		"install":   command("go mod download", "go.mod"),
		"test":      command("go test ./...", "go.mod"),
		"coverage":  command("go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1", "go.mod"),
		"lint":      command("go vet ./...", "go.mod"),
		"build":     command("go build ./...", "go.mod"),
		"benchmark": command(`go test -bench . -run "^$" ./...`, "go.mod"),
	}}
}

func detectRust(scan *projectScan) *ecosystemResult {
	if !scan.hasFile("Cargo.toml") {
		return nil
	}
	cargo := scan.read("Cargo.toml")
	commands := map[string]*CommandSpec{
		"install":   command("cargo fetch", "Cargo.toml"),
		"test":      command("cargo test --all-targets", "Cargo.toml"),
		"typecheck": command("cargo check --all-targets", "Cargo.toml"),
		"build":     command("cargo build --all-targets", "Cargo.toml"),
	}
	if util.Which("cargo-clippy") {
		commands["lint"] = command("cargo clippy --all-targets -- -D warnings", "clippy")
	}
	switch {
	case util.Which("cargo-llvm-cov"):
		commands["coverage"] = command("cargo llvm-cov --all-targets --summary-only", "cargo-llvm-cov")
	case util.Which("cargo-tarpaulin"):
		commands["coverage"] = command("cargo tarpaulin --out Stdout", "cargo-tarpaulin")
	}
	if scan.dirExists("benches") || strings.Contains(cargo, "[[bench]]") {
		commands["benchmark"] = command("cargo bench --all-targets", "Cargo.toml")
	}
	return &ecosystemResult{manager: "cargo", commands: commands}
}

func pythonExe() string {
	if util.Which("python3") {
		return "python3"
	}
	if util.Which("python") {
		return "python"
	}
	return "python3"
}

func detectPython(scan *projectScan) *ecosystemResult {
	markers := []string{"pyproject.toml", "requirements.txt", "setup.py", "setup.cfg", "Pipfile", "poetry.lock", "uv.lock", "tox.ini", "noxfile.py"}
	matched := ""
	for _, name := range markers {
		if scan.hasFile(name) {
			matched = name
			break
		}
	}
	if matched == "" {
		return nil
	}
	python := pythonExe()
	pyproject := strings.ToLower(scan.read("pyproject.toml"))
	requirements := strings.ToLower(scan.read("requirements.txt"))
	setupCfg := strings.ToLower(scan.read("setup.cfg"))
	tox := strings.ToLower(scan.read("tox.ini"))
	haystack := pyproject + "\n" + requirements + "\n" + setupCfg + "\n" + tox
	manager := "pip"
	switch {
	case scan.hasFile("uv.lock") || strings.Contains(pyproject, "[tool.uv]"):
		manager = "uv"
	case scan.hasFile("poetry.lock") || strings.Contains(pyproject, "[tool.poetry]"):
		manager = "poetry"
	case scan.hasFile("Pipfile"):
		manager = "pipenv"
	}
	commands := map[string]*CommandSpec{}

	switch manager {
	case "uv":
		commands["install"] = command("uv sync", "uv.lock")
	case "poetry":
		commands["install"] = command("poetry install", "poetry.lock")
	case "pipenv":
		commands["install"] = command("pipenv install --dev", "Pipfile")
	default:
		switch {
		case scan.hasFile("requirements.txt") && (scan.hasFile("pyproject.toml") || scan.hasFile("setup.py")):
			commands["install"] = command(python+" -m pip install -r requirements.txt && "+python+" -m pip install -e .", "requirements.txt")
		case scan.hasFile("requirements.txt"):
			commands["install"] = command(python+" -m pip install -r requirements.txt", "requirements.txt")
		case scan.hasFile("pyproject.toml") || scan.hasFile("setup.py") || scan.hasFile("setup.cfg"):
			commands["install"] = command(python+" -m pip install -e .", matched)
		}
	}

	hasTests := scan.dirExists("tests") || scan.dirExists("test")
	usesPytest := strings.Contains(haystack, "pytest") || scan.hasFile("conftest.py") || scan.hasFile("pytest.ini")
	if usesPytest {
		commands["test"] = command(python+" -m pytest -q", "pytest")
	} else if hasTests {
		dir := "tests"
		if !scan.dirExists("tests") {
			dir = "test"
		}
		commands["test"] = command(python+" -m unittest discover -s "+dir+" -v", "unittest")
	}
	hasCoverage := strings.Contains(haystack, "coverage") || strings.Contains(haystack, "pytest-cov")
	if commands["test"] != nil && hasCoverage {
		switch {
		case usesPytest && strings.Contains(haystack, "pytest-cov"):
			commands["coverage"] = command(python+" -m pytest --cov --cov-report=term-missing -q", "pytest-cov")
		case usesPytest:
			commands["coverage"] = command(python+" -m coverage run -m pytest -q && "+python+" -m coverage report -m", "coverage")
		default:
			commands["coverage"] = command(python+" -m coverage run -m unittest discover -s tests -v && "+python+" -m coverage report -m", "coverage")
		}
	}

	if strings.Contains(haystack, "ruff") || scan.hasFile("ruff.toml") || scan.hasFile(".ruff.toml") {
		if util.Which("ruff") {
			commands["lint"] = command("ruff check .", "ruff")
		} else {
			commands["lint"] = command(python+" -m ruff check .", "ruff")
		}
	} else if strings.Contains(haystack, "flake8") || scan.hasFile(".flake8") {
		commands["lint"] = command(python+" -m flake8 .", "flake8")
	}

	switch {
	case strings.Contains(haystack, "mypy") || scan.hasFile("mypy.ini") || scan.hasFile(".mypy.ini"):
		if util.Which("mypy") {
			commands["typecheck"] = command("mypy .", "mypy")
		} else {
			commands["typecheck"] = command(python+" -m mypy .", "mypy")
		}
	case scan.hasFile("pyrightconfig.json") && util.Which("pyright"):
		commands["typecheck"] = command("pyright", "pyrightconfig.json")
	}

	if strings.Contains(pyproject, "[build-system]") || scan.hasFile("setup.py") {
		commands["build"] = command(python+" -m build", matched)
	}
	return &ecosystemResult{manager: manager, commands: commands}
}

func detectJava(scan *projectScan) *ecosystemResult {
	if scan.hasFile("pom.xml") {
		base := wrapper(scan, "mvnw", "mvn")
		pom := scan.read("pom.xml")
		commands := map[string]*CommandSpec{
			"install": command(base+" -B -DskipTests install", "pom.xml"),
			"test":    command(base+" -B test", "pom.xml"),
			"build":   command(base+" -B -DskipTests package", "pom.xml"),
		}
		if strings.Contains(pom, "jacoco") {
			commands["coverage"] = command(base+" -B test jacoco:report", "pom.xml#jacoco")
		}
		switch {
		case strings.Contains(pom, "checkstyle"):
			commands["lint"] = command(base+" -B checkstyle:check", "pom.xml#checkstyle")
		case strings.Contains(pom, "spotbugs"):
			commands["lint"] = command(base+" -B spotbugs:check", "pom.xml#spotbugs")
		case strings.Contains(pom, "spotless"):
			commands["lint"] = command(base+" -B spotless:check", "pom.xml#spotless")
		}
		return &ecosystemResult{manager: "maven", commands: commands}
	}
	buildFile := scan.firstFile("build.gradle", "build.gradle.kts")
	if buildFile == "" && !scan.hasFile("settings.gradle") && !scan.hasFile("settings.gradle.kts") {
		return nil
	}
	base := wrapper(scan, "gradlew", "gradle")
	build := scan.read(buildFile)
	commands := map[string]*CommandSpec{
		"test":  command(base+" test", buildFile),
		"build": command(base+" assemble", buildFile),
	}
	if strings.Contains(build, "jacoco") {
		commands["coverage"] = command(base+" test jacocoTestReport", buildFile+"#jacoco")
	}
	switch {
	case strings.Contains(build, "spotless"):
		commands["lint"] = command(base+" spotlessCheck", buildFile+"#spotless")
	case strings.Contains(build, "checkstyle"):
		commands["lint"] = command(base+" checkstyleMain checkstyleTest", buildFile+"#checkstyle")
	case strings.Contains(build, "detekt"):
		commands["lint"] = command(base+" detekt", buildFile+"#detekt")
	case strings.Contains(build, "ktlint"):
		commands["lint"] = command(base+" ktlintCheck", buildFile+"#ktlint")
	}
	return &ecosystemResult{manager: "gradle", commands: commands}
}

func detectDotnet(scan *projectScan) *ecosystemResult {
	projects := scan.filesWithExt(".csproj", ".fsproj", ".vbproj")
	solutions := scan.filesWithExt(".sln", ".slnx")
	if len(projects) == 0 && len(solutions) == 0 && !scan.hasFile("global.json") {
		return nil
	}
	target := ""
	if len(solutions) == 0 && len(projects) == 1 {
		target = projects[0]
	}
	suffix := ""
	if target != "" {
		suffix = " " + shellArg(target)
	}
	commands := map[string]*CommandSpec{
		"install": command("dotnet restore --nologo"+suffix, "dotnet"),
		"test":    command("dotnet test --nologo"+suffix, "dotnet"),
		"build":   command("dotnet build --nologo"+suffix, "dotnet"),
		"lint":    command("dotnet format"+suffix+" --verify-no-changes", "dotnet"),
	}
	usesCoverlet := false
	benchmarkProject := ""
	for _, project := range append(projects, solutions...) {
		content := scan.read(project)
		if strings.Contains(strings.ToLower(content), "coverlet") {
			usesCoverlet = true
		}
		if strings.Contains(strings.ToLower(content), "benchmarkdotnet") || strings.Contains(strings.ToLower(filepath.Base(project)), "benchmark") {
			if benchmarkProject == "" {
				benchmarkProject = project
			}
		}
	}
	if usesCoverlet {
		commands["coverage"] = command(`dotnet test --nologo --collect:"XPlat Code Coverage" --results-directory coverage`+suffix, "coverlet")
	}
	if benchmarkProject != "" {
		commands["benchmark"] = command("dotnet run -c Release --project "+shellArg(benchmarkProject), "BenchmarkDotNet")
	}
	return &ecosystemResult{manager: "dotnet", commands: commands}
}

func detectRuby(scan *projectScan) *ecosystemResult {
	gemspecs := scan.filesWithExt(".gemspec")
	if !scan.hasFile("Gemfile") && !scan.hasFile("Rakefile") && len(gemspecs) == 0 {
		return nil
	}
	gemfile := strings.ToLower(scan.read("Gemfile"))
	rake := strings.ToLower(scan.read("Rakefile"))
	commands := map[string]*CommandSpec{}
	if scan.hasFile("Gemfile") {
		commands["install"] = command("bundle install", "Gemfile")
	}
	usesRSpec := scan.dirExists("spec") || strings.Contains(gemfile, "rspec")
	switch {
	case usesRSpec:
		commands["test"] = command("bundle exec rspec", "spec")
		if strings.Contains(gemfile, "simplecov") {
			commands["coverage"] = command("bundle exec rspec", "Gemfile#simplecov")
		}
	case scan.hasFile("Rakefile"):
		commands["test"] = command("bundle exec rake test", "Rakefile")
	}
	if scan.hasFile(".rubocop.yml") || strings.Contains(gemfile, "rubocop") {
		commands["lint"] = command("bundle exec rubocop", ".rubocop.yml")
	}
	if scan.hasFile("sorbet/config") {
		commands["typecheck"] = command("bundle exec srb tc", "sorbet/config")
	} else if scan.hasFile("Steepfile") {
		commands["typecheck"] = command("bundle exec steep check", "Steepfile")
	}
	if len(gemspecs) > 0 && strings.Contains(rake, "build") {
		commands["build"] = command("bundle exec rake build", "Rakefile")
	}
	if strings.Contains(rake, "benchmark") {
		commands["benchmark"] = command("bundle exec rake benchmark", "Rakefile")
	}
	return &ecosystemResult{manager: "bundler", commands: commands}
}

func detectPHP(scan *projectScan) *ecosystemResult {
	pkg := readJSONFile(scan, "composer.json")
	if pkg == nil {
		return nil
	}
	commands := map[string]*CommandSpec{"install": command("composer install --no-interaction --prefer-dist", "composer.json")}
	scripts, _ := pkg["scripts"].(map[string]any)
	scriptValue := func(name string) string {
		value := scripts[name]
		switch typed := value.(type) {
		case string:
			return typed
		case []any:
			var parts []string
			for _, item := range typed {
				if text, ok := item.(string); ok {
					parts = append(parts, text)
				}
			}
			return strings.Join(parts, " && ")
		}
		return ""
	}
	for _, name := range []string{"test", "tests"} {
		if scriptValue(name) != "" {
			commands["test"] = command("composer run "+name, "composer.json#scripts."+name)
			break
		}
	}
	for _, name := range []string{"test:coverage", "coverage", "test-coverage"} {
		if scriptValue(name) != "" {
			commands["coverage"] = command("composer run "+name, "composer.json#scripts."+name)
			break
		}
	}
	if commands["test"] == nil && (scan.hasFile("phpunit.xml") || scan.hasFile("phpunit.xml.dist")) {
		commands["test"] = command("vendor/bin/phpunit", "phpunit.xml")
	}
	if commands["coverage"] == nil && (scan.hasFile("phpunit.xml") || scan.hasFile("phpunit.xml.dist")) {
		commands["coverage"] = command("vendor/bin/phpunit --coverage-text", "phpunit.xml")
	}
	if scriptValue("lint") != "" {
		commands["lint"] = command("composer run lint", "composer.json#scripts.lint")
	} else if scan.hasFile("phpcs.xml") || scan.hasFile("phpcs.xml.dist") || scan.hasFile(".phpcs.xml") {
		commands["lint"] = command("vendor/bin/phpcs", "phpcs.xml")
	} else if scan.hasFile(".php-cs-fixer.php") || scan.hasFile(".php-cs-fixer.dist.php") || scan.hasFile(".php_cs") {
		commands["lint"] = command("vendor/bin/php-cs-fixer fix --dry-run --diff", ".php-cs-fixer.php")
	}
	if scriptValue("typecheck") != "" {
		commands["typecheck"] = command("composer run typecheck", "composer.json#scripts.typecheck")
	} else if scan.hasFile("phpstan.neon") || scan.hasFile("phpstan.neon.dist") {
		commands["typecheck"] = command("vendor/bin/phpstan analyse --no-progress", "phpstan.neon")
	} else if scan.hasFile("psalm.xml") || scan.hasFile("psalm.xml.dist") {
		commands["typecheck"] = command("vendor/bin/psalm --no-progress", "psalm.xml")
	}
	if scriptValue("build") != "" {
		commands["build"] = command("composer run build", "composer.json#scripts.build")
	}
	if scriptValue("benchmark") != "" {
		commands["benchmark"] = command("composer run benchmark", "composer.json#scripts.benchmark")
	} else if scan.hasFile("phpbench.json") {
		commands["benchmark"] = command("vendor/bin/phpbench run --report=default", "phpbench.json")
	}
	return &ecosystemResult{manager: "composer", commands: commands}
}

func detectCPP(scan *projectScan) *ecosystemResult {
	if scan.hasFile("CMakeLists.txt") {
		cmake := scan.read("CMakeLists.txt")
		commands := map[string]*CommandSpec{
			"install": command("cmake -S . -B build", "CMakeLists.txt"),
			"build":   command("cmake --build build --parallel", "CMakeLists.txt"),
		}
		if strings.Contains(cmake, "enable_testing") || strings.Contains(cmake, "add_test") || strings.Contains(cmake, "CTest") {
			commands["test"] = command("ctest --test-dir build --output-on-failure", "CMakeLists.txt")
		}
		if scan.hasFile(".clang-tidy") && util.Which("run-clang-tidy") {
			commands["lint"] = command("run-clang-tidy -p build", ".clang-tidy")
		}
		return &ecosystemResult{manager: "cmake", commands: commands}
	}
	if scan.hasFile("meson.build") {
		return &ecosystemResult{manager: "meson", commands: map[string]*CommandSpec{
			"install": command("meson setup build", "meson.build"),
			"build":   command("meson compile -C build", "meson.build"),
			"test":    command("meson test -C build --print-errorlogs", "meson.build"),
		}}
	}
	if scan.hasFile("configure.ac") || scan.hasFile("configure") {
		install := "./configure"
		if !scan.executable("configure") {
			install = "autoreconf -fi && ./configure"
		}
		return &ecosystemResult{manager: "autotools", commands: map[string]*CommandSpec{
			"install": command(install, "configure"),
			"build":   command("make", "configure"),
			"test":    command("make check", "configure"),
		}}
	}
	if scan.hasFile("vcpkg.json") {
		return &ecosystemResult{manager: "vcpkg", commands: map[string]*CommandSpec{
			"install": command("vcpkg install", "vcpkg.json"),
		}}
	}
	return nil
}

func detectSwift(scan *projectScan) *ecosystemResult {
	if !scan.hasFile("Package.swift") {
		return nil
	}
	commands := map[string]*CommandSpec{
		"install": command("swift package resolve", "Package.swift"),
		"build":   command("swift build", "Package.swift"),
		"test":    command("swift test", "Package.swift"),
	}
	if scan.hasFile(".swiftlint.yml") && util.Which("swiftlint") {
		commands["lint"] = command("swiftlint lint --strict", ".swiftlint.yml")
	}
	if scan.dirExists("Benchmarks") && strings.Contains(scan.read("Package.swift"), "Benchmark") {
		commands["benchmark"] = command("swift package benchmark", "Package.swift")
	}
	return &ecosystemResult{manager: "swift", commands: commands}
}

func detectDart(scan *projectScan) *ecosystemResult {
	pubspec := scan.read("pubspec.yaml")
	if pubspec == "" {
		return nil
	}
	flutter := strings.Contains(pubspec, "sdk: flutter") || strings.Contains(pubspec, "flutter:")
	manager := "dart"
	if flutter {
		manager = "flutter"
	}
	pub := "dart pub get"
	test := "dart test"
	analyze := "dart analyze"
	if flutter {
		pub = "flutter pub get"
		test = "flutter test"
		analyze = "flutter analyze"
	}
	commands := map[string]*CommandSpec{
		"install": command(pub, "pubspec.yaml"),
		"test":    command(test, "pubspec.yaml"),
		"lint":    command(analyze, "pubspec.yaml"),
	}
	if flutter {
		commands["coverage"] = command("flutter test --coverage", "pubspec.yaml")
	} else if scan.hasFile("bin/main.dart") {
		commands["build"] = command("dart compile exe bin/main.dart -o build/app", "bin/main.dart")
	}
	return &ecosystemResult{manager: manager, commands: commands}
}

func detectElixir(scan *projectScan) *ecosystemResult {
	mix := scan.read("mix.exs")
	if mix == "" {
		return nil
	}
	commands := map[string]*CommandSpec{
		"install":  command("mix deps.get", "mix.exs"),
		"test":     command("mix test", "mix.exs"),
		"coverage": command("mix test --cover", "mix.exs"),
		"build":    command("mix compile", "mix.exs"),
	}
	switch {
	case strings.Contains(mix, "credo"):
		commands["lint"] = command("mix credo --strict", "mix.exs#credo")
	case scan.hasFile(".formatter.exs"):
		commands["lint"] = command("mix format --check-formatted", ".formatter.exs")
	}
	if strings.Contains(mix, "dialyxir") {
		commands["typecheck"] = command("mix dialyzer", "mix.exs#dialyxir")
	}
	return &ecosystemResult{manager: "mix", commands: commands}
}

func detectScala(scan *projectScan) *ecosystemResult {
	if !scan.hasFile("build.sbt") {
		return nil
	}
	commands := map[string]*CommandSpec{
		"install": command("sbt update", "build.sbt"),
		"test":    command("sbt test", "build.sbt"),
		"build":   command("sbt compile", "build.sbt"),
	}
	if scan.hasFile(".scalafmt.conf") {
		commands["lint"] = command("sbt scalafmtCheckAll", ".scalafmt.conf")
	}
	if strings.Contains(scan.read("project/plugins.sbt")+scan.read("build.sbt"), "scoverage") {
		commands["coverage"] = command("sbt coverage test coverageReport", "build.sbt#scoverage")
	}
	return &ecosystemResult{manager: "sbt", commands: commands}
}

func detectHaskell(scan *projectScan) *ecosystemResult {
	cabalFiles := scan.filesWithExt(".cabal")
	if len(cabalFiles) == 0 && !scan.hasFile("stack.yaml") && !scan.hasFile("cabal.project") {
		return nil
	}
	manager := "cabal"
	commands := map[string]*CommandSpec{}
	if scan.hasFile("stack.yaml") && !scan.hasFile("cabal.project") {
		manager = "stack"
		commands["install"] = command("stack build --only-dependencies", "stack.yaml")
		commands["build"] = command("stack build", "stack.yaml")
		commands["test"] = command("stack test", "stack.yaml")
		if hasBenchmark(cabalFiles, scan) || strings.Contains(scan.read("package.yaml"), "benchmarks:") {
			commands["benchmark"] = command("stack bench", "stack.yaml")
		}
	} else {
		commands["install"] = command("cabal build all --only-dependencies", "cabal")
		commands["build"] = command("cabal build all", "cabal")
		commands["test"] = command("cabal test all", "cabal")
		if hasBenchmark(cabalFiles, scan) {
			commands["benchmark"] = command("cabal bench all", "cabal")
		}
	}
	if util.Which("hlint") {
		commands["lint"] = command("hlint .", "hlint")
	}
	return &ecosystemResult{manager: manager, commands: commands}
}

func hasBenchmark(cabalFiles []string, scan *projectScan) bool {
	for _, file := range cabalFiles {
		if strings.Contains(strings.ToLower(scan.read(file)), "benchmark") {
			return true
		}
	}
	return false
}

func detectZig(scan *projectScan) *ecosystemResult {
	build := scan.read("build.zig")
	if build == "" {
		return nil
	}
	commands := map[string]*CommandSpec{
		"build": command("zig build", "build.zig"),
		"test":  command("zig build test", "build.zig"),
		"lint":  command("zig fmt --check .", "build.zig"),
	}
	if strings.Contains(build, "bench") {
		commands["benchmark"] = command("zig build bench", "build.zig")
	}
	return &ecosystemResult{manager: "zig", commands: commands}
}

var denoTasksPattern = regexp.MustCompile(`(?s)"tasks"\s*:\s*\{(.*?)\}`)
var denoTaskKeyPattern = regexp.MustCompile(`"([^"]+)"\s*:`)

func denoTasks(scan *projectScan) map[string]bool {
	tasks := map[string]bool{}
	for _, name := range []string{"deno.json", "deno.jsonc"} {
		text := scan.read(name)
		if text == "" {
			continue
		}
		match := denoTasksPattern.FindStringSubmatch(text)
		if len(match) != 2 {
			continue
		}
		for _, key := range denoTaskKeyPattern.FindAllStringSubmatch(match[1], -1) {
			tasks[key[1]] = true
		}
	}
	return tasks
}

func detectDeno(scan *projectScan) *ecosystemResult {
	if !scan.hasFile("deno.json") && !scan.hasFile("deno.jsonc") {
		return nil
	}
	tasks := denoTasks(scan)
	commands := map[string]*CommandSpec{}
	setTask := func(key string, names ...string) bool {
		for _, name := range names {
			if tasks[name] {
				commands[key] = command("deno task "+name, "deno.json#tasks."+name)
				return true
			}
		}
		return false
	}
	if !setTask("test", "test") {
		commands["test"] = command("deno test", "deno.json")
	}
	if !setTask("lint", "lint") {
		commands["lint"] = command("deno lint", "deno.json")
	}
	if !setTask("typecheck", "check", "typecheck") {
		commands["typecheck"] = command("deno check .", "deno.json")
	}
	setTask("build", "build", "compile")
	setTask("benchmark", "bench", "benchmark")
	setTask("coverage", "coverage", "test:coverage")
	return &ecosystemResult{manager: "deno", commands: commands}
}

func detectLua(scan *projectScan) *ecosystemResult {
	rockspecs := scan.filesWithExt(".rockspec")
	if len(rockspecs) == 0 && !scan.hasFile(".busted") && !scan.hasFile(".luacheckrc") {
		return nil
	}
	commands := map[string]*CommandSpec{}
	if len(rockspecs) > 0 {
		commands["install"] = command("luarocks install --only-deps "+shellArg(rockspecs[0]), rockspecs[0])
		commands["build"] = command("luarocks make "+shellArg(rockspecs[0]), rockspecs[0])
	}
	if scan.hasFile(".busted") || scan.dirExists("spec") {
		commands["test"] = command("busted", ".busted")
	}
	if scan.hasFile(".luacheckrc") || util.Which("luacheck") {
		commands["lint"] = command("luacheck .", ".luacheckrc")
	}
	return &ecosystemResult{manager: "luarocks", commands: commands}
}

func detectR(scan *projectScan) *ecosystemResult {
	if !scan.hasFile("DESCRIPTION") {
		return nil
	}
	commands := map[string]*CommandSpec{
		"install": command("R CMD INSTALL .", "DESCRIPTION"),
		"build":   command("R CMD build .", "DESCRIPTION"),
	}
	if scan.dirExists("tests/testthat") {
		commands["test"] = command(`Rscript -e 'testthat::test_local()'`, "tests/testthat")
	}
	return &ecosystemResult{manager: "R", commands: commands}
}

func detectJulia(scan *projectScan) *ecosystemResult {
	if !scan.hasFile("Project.toml") {
		return nil
	}
	return &ecosystemResult{manager: "julia", commands: map[string]*CommandSpec{
		"install": command("julia --project=. -e 'using Pkg; Pkg.instantiate()'", "Project.toml"),
		"test":    command("julia --project=. -e 'using Pkg; Pkg.test()'", "Project.toml"),
	}}
}

var makeTargetPattern = regexp.MustCompile(`^([A-Za-z0-9_.-]+)\s*:([^=]|$)`)
var justRecipePattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)(?:\s+.*)?:([^=]|$)`)
var taskfileTaskPattern = regexp.MustCompile(`^  ([A-Za-z0-9_.-]+):\s*$`)

var taskAliases = map[string][]string{
	"install":   {"install", "deps", "dependencies", "setup", "bootstrap", "init"},
	"test":      {"test", "tests", "check", "spec"},
	"coverage":  {"coverage", "cover", "test-coverage", "coverage-report"},
	"lint":      {"lint", "vet", "style", "check-lint"},
	"typecheck": {"typecheck", "type-check", "types", "check-types"},
	"build":     {"build", "compile", "all"},
	"benchmark": {"bench", "benchmark", "perf", "performance"},
}

func detectMakefile(scan *projectScan) map[string]*CommandSpec {
	name := scan.firstFile("Makefile", "makefile", "GNUmakefile")
	if name == "" {
		return nil
	}
	targets := map[string]bool{}
	for _, line := range strings.Split(scan.read(name), "\n") {
		if match := makeTargetPattern.FindStringSubmatch(strings.TrimRight(line, "\r")); len(match) >= 2 {
			targets[match[1]] = true
		}
	}
	commands := map[string]*CommandSpec{}
	for key, aliases := range taskAliases {
		for _, target := range aliases {
			if targets[target] {
				commands[key] = command("make "+target, name+"#"+target)
				break
			}
		}
	}
	return commands
}

func detectJustfile(scan *projectScan) map[string]*CommandSpec {
	name := scan.firstFile("justfile", "Justfile", ".justfile")
	if name == "" {
		return nil
	}
	targets := map[string]bool{}
	for _, line := range strings.Split(scan.read(name), "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "@") || strings.Contains(line, ":=") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "set ") || strings.HasPrefix(lower, "alias ") || strings.HasPrefix(lower, "export ") || strings.HasPrefix(lower, "import ") || strings.HasPrefix(lower, "mod ") {
			continue
		}
		if match := justRecipePattern.FindStringSubmatch(line); len(match) >= 2 {
			targets[match[1]] = true
		}
	}
	commands := map[string]*CommandSpec{}
	for key, aliases := range taskAliases {
		for _, target := range aliases {
			if targets[target] {
				commands[key] = command("just "+target, name+"#"+target)
				break
			}
		}
	}
	return commands
}

func detectTaskfile(scan *projectScan) map[string]*CommandSpec {
	name := scan.firstFile("Taskfile.yml", "Taskfile.yaml")
	if name == "" {
		return nil
	}
	targets := map[string]bool{}
	for _, line := range strings.Split(scan.read(name), "\n") {
		if match := taskfileTaskPattern.FindStringSubmatch(strings.TrimRight(line, "\r")); len(match) == 2 {
			targets[match[1]] = true
		}
	}
	commands := map[string]*CommandSpec{}
	for key, aliases := range taskAliases {
		for _, target := range aliases {
			if targets[target] {
				commands[key] = command("task "+target, name+"#"+target)
				break
			}
		}
	}
	return commands
}

func detectBazel(scan *projectScan) map[string]*CommandSpec {
	if !scan.hasFile("WORKSPACE") && !scan.hasFile("WORKSPACE.bazel") && !scan.hasFile("MODULE.bazel") && !scan.hasFile("BUILD.bazel") {
		return nil
	}
	return map[string]*CommandSpec{
		"test":  command("bazel test //...", "bazel"),
		"build": command("bazel build //...", "bazel"),
	}
}

func detectNix(scan *projectScan) map[string]*CommandSpec {
	if !scan.hasFile("flake.nix") {
		return nil
	}
	return map[string]*CommandSpec{
		"build": command("nix build", "flake.nix"),
		"test":  command("nix flake check", "flake.nix"),
	}
}

func detectScripts(scan *projectScan) map[string]*CommandSpec {
	names := map[string][]string{
		"test":      {"scripts/test.sh", "test.sh", "scripts/run-tests.sh", "run-tests.sh"},
		"coverage":  {"scripts/coverage.sh", "coverage.sh"},
		"lint":      {"scripts/lint.sh", "lint.sh"},
		"typecheck": {"scripts/typecheck.sh", "typecheck.sh"},
		"build":     {"scripts/build.sh", "build.sh"},
		"benchmark": {"scripts/bench.sh", "scripts/benchmark.sh", "bench.sh", "benchmark.sh"},
	}
	if filepath.Separator == '\\' {
		names["test"] = []string{`scripts\test.bat`, `test.bat`, `scripts\test.cmd`, `test.cmd`}
		names["coverage"] = []string{`scripts\coverage.bat`, `coverage.bat`}
		names["lint"] = []string{`scripts\lint.bat`, `lint.bat`}
		names["typecheck"] = []string{`scripts\typecheck.bat`, `typecheck.bat`}
		names["build"] = []string{`scripts\build.bat`, `build.bat`}
		names["benchmark"] = []string{`scripts\bench.bat`, `bench.bat`, `benchmark.bat`}
	}
	commands := map[string]*CommandSpec{}
	for _, key := range commandKeys {
		for _, name := range names[key] {
			if scan.executable(name) {
				prefix := "./"
				if filepath.Separator == '\\' {
					prefix = `.\`
				}
				commands[key] = command(prefix+name, name)
				break
			}
		}
	}
	if commands["install"] == nil {
		if scan.executable("scripts/install.sh") || scan.executable("install.sh") {
			commands["install"] = command("./install.sh", "install.sh")
		}
	}
	return commands
}

func detectTaskRunners(scan *projectScan) map[string]*CommandSpec {
	commands := map[string]*CommandSpec{}
	for _, group := range []map[string]*CommandSpec{
		detectMakefile(scan), detectJustfile(scan), detectTaskfile(scan),
		detectBazel(scan), detectNix(scan), detectScripts(scan),
	} {
		for key, spec := range group {
			if commands[key] == nil {
				commands[key] = spec
			}
		}
	}
	return commands
}

// DetectProject identifies the ecosystems and the commands a harness should run.
func DetectProject(cwd string) Detected {
	scan := newProjectScan(cwd)
	var results []*ecosystemResult
	for _, detector := range ecosystemDetectors {
		if result := detector.detect(scan); result != nil {
			result.name = detector.name
			results = append(results, result)
		}
	}
	counts := scan.sourceCounts()
	order := make([]int, len(results))
	for index := range results {
		order[index] = index
	}
	if len(results) > 1 {
		sort.SliceStable(order, func(i, j int) bool {
			return counts[results[order[i]].name] > counts[results[order[j]].name]
		})
	}

	detected := Detected{
		Language:  "unknown",
		Manifests: scan.manifests(),
		Commands:  map[string]*CommandSpec{},
	}
	if len(results) > 0 {
		detected.Language = results[order[0]].name
		for _, index := range order {
			detected.Languages = append(detected.Languages, results[index].name)
		}
	} else {
		detected.Language = scan.dominantLanguage()
	}
	for _, index := range order {
		result := results[index]
		if detected.PackageManager == "" {
			detected.PackageManager = result.manager
		}
		if result.pkg != nil && detected.PackageJSON == nil {
			detected.PackageJSON = result.pkg
			detected.Scripts = result.scripts
		}
		for key, spec := range result.commands {
			if detected.Commands[key] == nil {
				detected.Commands[key] = spec
			}
		}
	}
	for key, spec := range detectTaskRunners(scan) {
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

// Profile renders a short human-readable project profile.
func (detected Detected) Profile() string {
	languages := detected.Languages
	if len(languages) == 0 && detected.Language != "" {
		languages = []string{detected.Language}
	}
	label := strings.Join(languages, ", ")
	if label == "" {
		label = "unknown"
	}
	if detected.PackageManager != "" && !strings.EqualFold(detected.PackageManager, label) {
		label += " (" + detected.PackageManager + ")"
	}
	return label
}

// MarshalDetection is used by `forge init` output.
func MarshalDetection(detected Detected) string {
	data, _ := json.Marshal(detected)
	return string(data)
}
