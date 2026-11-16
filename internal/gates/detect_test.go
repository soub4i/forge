package gates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExecutable(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func detectFixture(t *testing.T, files map[string]string, repoExecutables, pathTools []string) Detected {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		writeFile(t, dir, rel, content)
	}
	for _, rel := range repoExecutables {
		writeExecutable(t, dir, rel, "#!/bin/sh\nexit 0\n")
	}
	binDir := t.TempDir()
	for _, name := range pathTools {
		writeExecutable(t, binDir, name, "#!/bin/sh\nexit 0\n")
	}
	t.Setenv("PATH", binDir)
	return DetectProject(dir)
}

func assertCommand(t *testing.T, detected Detected, key, want string) {
	t.Helper()
	spec := detected.Commands[key]
	if spec == nil {
		t.Fatalf("%s command is missing; detected = %+v", key, detected.Commands)
	}
	if spec.Command != want {
		t.Fatalf("%s command = %q, want %q", key, spec.Command, want)
	}
}

func TestDetectEcosystems(t *testing.T) {
	cases := []struct {
		name            string
		files           map[string]string
		repoExecutables []string
		pathTools       []string
		language        string
		manager         string
		want            map[string]string
	}{
		{
			name: "pnpm",
			files: map[string]string{
				"package.json":   `{"scripts":{"test":"vitest run","lint":"eslint ."}}`,
				"pnpm-lock.yaml": "lockfileVersion: 9\n",
			},
			language: "node", manager: "pnpm",
			want: map[string]string{
				"install": "pnpm install --frozen-lockfile",
				"test":    "pnpm run test",
				"lint":    "pnpm run lint",
			},
		},
		{
			name: "yarn berry",
			files: map[string]string{
				"package.json": `{"scripts":{"test":"jest"}}`,
				"yarn.lock":    "",
				".yarnrc.yml":  "nodeLinker: node-modules\n",
			},
			language: "node", manager: "yarn",
			want: map[string]string{
				"install": "yarn install --immutable",
				"test":    "yarn test",
			},
		},
		{
			name: "bun",
			files: map[string]string{
				"package.json": `{"scripts":{"test":"bun test"}}`,
				"bun.lockb":    "",
			},
			language: "node", manager: "bun",
			want: map[string]string{
				"install": "bun install",
				"test":    "bun run test",
			},
		},
		{
			name: "declared package manager",
			files: map[string]string{
				"package.json": `{"packageManager":"pnpm@9.1.0","scripts":{"test":"vitest"}}`,
			},
			language: "node", manager: "pnpm",
			want: map[string]string{
				"install": "pnpm install",
				"test":    "pnpm run test",
			},
		},
		{
			name: "node typescript",
			files: map[string]string{
				"package.json":  `{"devDependencies":{"typescript":"5.0.0"}}`,
				"tsconfig.json": "{}",
			},
			language: "node", manager: "npm",
			want: map[string]string{
				"install":   "npm install --no-audit --no-fund",
				"typecheck": "npx --no-install tsc --noEmit",
			},
		},
		{
			name: "maven",
			files: map[string]string{
				"pom.xml": `<project><build><plugins>
					<plugin><artifactId>jacoco-maven-plugin</artifactId></plugin>
					<plugin><artifactId>maven-checkstyle-plugin</artifactId></plugin>
				</plugins></build></project>`,
			},
			repoExecutables: []string{"mvnw"},
			language:        "java", manager: "maven",
			want: map[string]string{
				"install":  "./mvnw -B -DskipTests install",
				"test":     "./mvnw -B test",
				"coverage": "./mvnw -B test jacoco:report",
				"lint":     "./mvnw -B checkstyle:check",
				"build":    "./mvnw -B -DskipTests package",
			},
		},
		{
			name: "gradle",
			files: map[string]string{
				"build.gradle.kts": "plugins { jacoco }\nspotless { }\n",
			},
			repoExecutables: []string{"gradlew"},
			language:        "java", manager: "gradle",
			want: map[string]string{
				"test":     "./gradlew test",
				"coverage": "./gradlew test jacocoTestReport",
				"lint":     "./gradlew spotlessCheck",
				"build":    "./gradlew assemble",
			},
		},
		{
			name: "dotnet",
			files: map[string]string{
				"demo.sln": `<Solution />`,
				"demo.csproj": `<Project>
					<ItemGroup>
						<PackageReference Include="coverlet.collector" Version="6.0.0" />
						<PackageReference Include="BenchmarkDotNet" Version="0.14.0" />
					</ItemGroup>
				</Project>`,
			},
			language: "dotnet", manager: "dotnet",
			want: map[string]string{
				"install":   "dotnet restore --nologo",
				"test":      "dotnet test --nologo",
				"build":     "dotnet build --nologo",
				"lint":      "dotnet format --verify-no-changes",
				"coverage":  `dotnet test --nologo --collect:"XPlat Code Coverage" --results-directory coverage`,
				"benchmark": "dotnet run -c Release --project demo.csproj",
			},
		},
		{
			name: "ruby",
			files: map[string]string{
				"Gemfile":             "gem \"rspec\"\ngem \"simplecov\"\ngem \"rubocop\"\n",
				"spec/spec_helper.rb": "require \"simplecov\"\n",
				".rubocop.yml":        "AllCops:\n  NewCops: enable\n",
			},
			language: "ruby", manager: "bundler",
			want: map[string]string{
				"install":  "bundle install",
				"test":     "bundle exec rspec",
				"coverage": "bundle exec rspec",
				"lint":     "bundle exec rubocop",
			},
		},
		{
			name: "php",
			files: map[string]string{
				"composer.json": `{"scripts":{
					"test":"phpunit",
					"test:coverage":"phpunit --coverage-text",
					"lint":"phpcs",
					"typecheck":"phpstan analyse",
					"build":"box compile",
					"benchmark":"phpbench run"
				}}`,
			},
			language: "php", manager: "composer",
			want: map[string]string{
				"install":   "composer install --no-interaction --prefer-dist",
				"test":      "composer run test",
				"coverage":  "composer run test:coverage",
				"lint":      "composer run lint",
				"typecheck": "composer run typecheck",
				"build":     "composer run build",
				"benchmark": "composer run benchmark",
			},
		},
		{
			name: "cmake",
			files: map[string]string{
				"CMakeLists.txt": "cmake_minimum_required(VERSION 3.20)\nenable_testing()\n",
				".clang-tidy":    "Checks: '*'\n",
			},
			pathTools: []string{"run-clang-tidy"},
			language:  "cpp", manager: "cmake",
			want: map[string]string{
				"install": "cmake -S . -B build",
				"build":   "cmake --build build --parallel",
				"test":    "ctest --test-dir build --output-on-failure",
				"lint":    "run-clang-tidy -p build",
			},
		},
		{
			name: "meson",
			files: map[string]string{
				"meson.build": "project('demo', 'c')\n",
			},
			language: "cpp", manager: "meson",
			want: map[string]string{
				"install": "meson setup build",
				"build":   "meson compile -C build",
				"test":    "meson test -C build --print-errorlogs",
			},
		},
		{
			name: "autotools",
			files: map[string]string{
				"configure.ac": "AC_INIT([demo], [1.0])\n",
			},
			language: "cpp", manager: "autotools",
			want: map[string]string{
				"install": "autoreconf -fi && ./configure",
				"build":   "make",
				"test":    "make check",
			},
		},
		{
			name: "swift",
			files: map[string]string{
				"Package.swift":        "// swift-tools-version:5.9\nimport PackageDescription\n// Benchmark\n",
				".swiftlint.yml":       "strict: true\n",
				"Benchmarks/Run.swift": "// benchmark\n",
			},
			pathTools: []string{"swiftlint"},
			language:  "swift", manager: "swift",
			want: map[string]string{
				"install":   "swift package resolve",
				"build":     "swift build",
				"test":      "swift test",
				"lint":      "swiftlint lint --strict",
				"benchmark": "swift package benchmark",
			},
		},
		{
			name: "dart",
			files: map[string]string{
				"pubspec.yaml":  "name: demo\nenvironment:\n  sdk: '>=3.0.0 <4.0.0'\n",
				"bin/main.dart": "void main() {}\n",
			},
			language: "dart", manager: "dart",
			want: map[string]string{
				"install": "dart pub get",
				"test":    "dart test",
				"lint":    "dart analyze",
				"build":   "dart compile exe bin/main.dart -o build/app",
			},
		},
		{
			name: "flutter",
			files: map[string]string{
				"pubspec.yaml": "name: demo\nenvironment:\n  sdk: flutter\n",
			},
			language: "dart", manager: "flutter",
			want: map[string]string{
				"install":  "flutter pub get",
				"test":     "flutter test",
				"lint":     "flutter analyze",
				"coverage": "flutter test --coverage",
			},
		},
		{
			name: "elixir",
			files: map[string]string{
				"mix.exs":        "defp deps, do: [{:credo, \"~> 1.7\"}, {:dialyxir, \"~> 1.4\"}]\n",
				".formatter.exs": "[]\n",
			},
			language: "elixir", manager: "mix",
			want: map[string]string{
				"install":   "mix deps.get",
				"test":      "mix test",
				"coverage":  "mix test --cover",
				"build":     "mix compile",
				"lint":      "mix credo --strict",
				"typecheck": "mix dialyzer",
			},
		},
		{
			name: "scala",
			files: map[string]string{
				"build.sbt":      "addSbtPlugin(\"scoverage\")\n",
				".scalafmt.conf": "version = 3.7.0\n",
			},
			language: "scala", manager: "sbt",
			want: map[string]string{
				"install":  "sbt update",
				"test":     "sbt test",
				"build":    "sbt compile",
				"lint":     "sbt scalafmtCheckAll",
				"coverage": "sbt coverage test coverageReport",
			},
		},
		{
			name: "cabal",
			files: map[string]string{
				"demo.cabal": "benchmark demo-bench\n  type: exitcode-stdio-1.0\n",
			},
			language: "haskell", manager: "cabal",
			want: map[string]string{
				"install":   "cabal build all --only-dependencies",
				"build":     "cabal build all",
				"test":      "cabal test all",
				"benchmark": "cabal bench all",
			},
		},
		{
			name: "stack",
			files: map[string]string{
				"stack.yaml":   "resolver: lts-22.0\n",
				"package.yaml": "benchmarks:\n  demo-bench:\n    main: Bench.hs\n",
			},
			language: "haskell", manager: "stack",
			want: map[string]string{
				"install":   "stack build --only-dependencies",
				"build":     "stack build",
				"test":      "stack test",
				"benchmark": "stack bench",
			},
		},
		{
			name: "zig",
			files: map[string]string{
				"build.zig": "// bench step\n",
			},
			language: "zig", manager: "zig",
			want: map[string]string{
				"build":     "zig build",
				"test":      "zig build test",
				"lint":      "zig fmt --check .",
				"benchmark": "zig build bench",
			},
		},
		{
			name: "deno tasks",
			files: map[string]string{
				"deno.json": `{"tasks":{"test":"deno test","lint":"deno lint","check":"deno check .","build":"deno compile","bench":"deno bench","coverage":"deno test --coverage"}}`,
			},
			language: "deno", manager: "deno",
			want: map[string]string{
				"test":      "deno task test",
				"lint":      "deno task lint",
				"typecheck": "deno task check",
				"build":     "deno task build",
				"benchmark": "deno task bench",
				"coverage":  "deno task coverage",
			},
		},
		{
			name: "deno defaults",
			files: map[string]string{
				"deno.json": "{}",
			},
			language: "deno", manager: "deno",
			want: map[string]string{
				"test":      "deno test",
				"lint":      "deno lint",
				"typecheck": "deno check .",
			},
		},
		{
			name: "lua",
			files: map[string]string{
				"demo-1.0-1.rockspec": "package = 'demo'\n",
				".busted":             "pattern = '_spec'\n",
				".luacheckrc":         "std = 'lua51'\n",
			},
			language: "lua", manager: "luarocks",
			want: map[string]string{
				"install": "luarocks install --only-deps demo-1.0-1.rockspec",
				"build":   "luarocks make demo-1.0-1.rockspec",
				"test":    "busted",
				"lint":    "luacheck .",
			},
		},
		{
			name: "R",
			files: map[string]string{
				"DESCRIPTION":                "Package: demo\n",
				"tests/testthat/test-demo.R": "test_that('it works', {})\n",
			},
			language: "r", manager: "R",
			want: map[string]string{
				"install": "R CMD INSTALL .",
				"build":   "R CMD build .",
				"test":    "Rscript -e 'testthat::test_local()'",
			},
		},
		{
			name: "julia",
			files: map[string]string{
				"Project.toml": "name = \"Demo\"\n",
			},
			language: "julia", manager: "julia",
			want: map[string]string{
				"install": "julia --project=. -e 'using Pkg; Pkg.instantiate()'",
				"test":    "julia --project=. -e 'using Pkg; Pkg.test()'",
			},
		},
		{
			name: "python uv",
			files: map[string]string{
				"pyproject.toml":     "[project]\nname = \"demo\"\n[build-system]\nrequires = [\"setuptools\"]\n[dependency-groups]\ntest = [\"pytest\", \"pytest-cov\"]\n[tool.uv]\n",
				"uv.lock":            "",
				"tests/test_demo.py": "def test_it():\n    assert True\n",
			},
			language: "python", manager: "uv",
			want: map[string]string{
				"install":  "uv sync",
				"test":     "python3 -m pytest -q",
				"coverage": "python3 -m pytest --cov --cov-report=term-missing -q",
				"build":    "python3 -m build",
			},
		},
		{
			name: "python poetry",
			files: map[string]string{
				"pyproject.toml":     "[tool.poetry]\nname = \"demo\"\n",
				"poetry.lock":        "",
				"tests/test_demo.py": "def test_it():\n    assert True\n",
			},
			language: "python", manager: "poetry",
			want: map[string]string{
				"install": "poetry install",
				"test":    "python3 -m unittest discover -s tests -v",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			detected := detectFixture(t, tc.files, tc.repoExecutables, tc.pathTools)
			if detected.Language != tc.language {
				t.Fatalf("language = %q, want %q (languages = %v)", detected.Language, tc.language, detected.Languages)
			}
			if detected.PackageManager != tc.manager {
				t.Fatalf("package manager = %q, want %q", detected.PackageManager, tc.manager)
			}
			for key, want := range tc.want {
				assertCommand(t, detected, key, want)
			}
			if len(detected.Manifests) == 0 {
				t.Fatal("expected at least one manifest")
			}
		})
	}
}

func TestDetectMultiEcosystemPrimaryAndGapFill(t *testing.T) {
	detected := detectFixture(t, map[string]string{
		"Cargo.toml":   "[package]\nname = \"demo\"\n",
		"package.json": `{"scripts":{"test":"vitest","lint":"eslint .","benchmark":"vitest bench"}}`,
		"src/lib.rs":   "pub fn demo() {}\n",
		"src/main.rs":  "fn main() {}\n",
		"src/util.rs":  "pub fn util() {}\n",
		"src/index.js": "console.log(1)\n",
	}, nil, nil)
	if detected.Language != "rust" {
		t.Fatalf("primary language = %q (languages = %v)", detected.Language, detected.Languages)
	}
	if strings.Join(detected.Languages, ",") != "rust,node" {
		t.Fatalf("languages = %v", detected.Languages)
	}
	if detected.PackageManager != "cargo" {
		t.Fatalf("package manager = %q", detected.PackageManager)
	}
	assertCommand(t, detected, "test", "cargo test --all-targets")
	assertCommand(t, detected, "build", "cargo build --all-targets")
	assertCommand(t, detected, "lint", "npm run lint")
	assertCommand(t, detected, "benchmark", "npm run benchmark")
	for _, manifest := range []string{"Cargo.toml", "package.json"} {
		found := false
		for _, item := range detected.Manifests {
			if item == manifest {
				found = true
			}
		}
		if !found {
			t.Errorf("manifest %q missing from %v", manifest, detected.Manifests)
		}
	}
}

func TestDetectTaskRunnersAndScripts(t *testing.T) {
	t.Run("precedence", func(t *testing.T) {
		detected := detectFixture(t, map[string]string{
			"Makefile":        "test:\n\ttrue\n\nlint:\n\ttrue\n",
			"justfile":        "test:\n  true\n",
			"Taskfile.yml":    "version: '3'\ntasks:\n  test:\n    cmds:\n      - true\n",
			"scripts/test.sh": "#!/bin/sh\nexit 0\n",
		}, []string{"scripts/test.sh"}, nil)
		assertCommand(t, detected, "test", "make test")
		assertCommand(t, detected, "lint", "make lint")
	})

	t.Run("just", func(t *testing.T) {
		detected := detectFixture(t, map[string]string{
			"justfile": "test:\n  go test ./...\n\nbuild target=\"release\":\n  go build ./...\n\nbench: test\n  go test -bench . ./...\n",
		}, nil, nil)
		assertCommand(t, detected, "test", "just test")
		assertCommand(t, detected, "build", "just build")
		assertCommand(t, detected, "benchmark", "just bench")
	})

	t.Run("taskfile", func(t *testing.T) {
		detected := detectFixture(t, map[string]string{
			"Taskfile.yml": "version: '3'\ntasks:\n  test:\n    cmds:\n      - go test ./...\n  build:\n    cmds:\n      - go build ./...\n",
		}, nil, nil)
		assertCommand(t, detected, "test", "task test")
		assertCommand(t, detected, "build", "task build")
	})

	t.Run("scripts", func(t *testing.T) {
		detected := detectFixture(t, map[string]string{}, []string{
			"test.sh", "coverage.sh", "lint.sh", "typecheck.sh", "build.sh", "benchmark.sh",
		}, nil)
		assertCommand(t, detected, "test", "./test.sh")
		assertCommand(t, detected, "coverage", "./coverage.sh")
		assertCommand(t, detected, "lint", "./lint.sh")
		assertCommand(t, detected, "typecheck", "./typecheck.sh")
		assertCommand(t, detected, "build", "./build.sh")
		assertCommand(t, detected, "benchmark", "./benchmark.sh")
	})
}

func TestDetectSourceOnlyLanguages(t *testing.T) {
	cases := map[string]map[string]string{
		"go":      {"main.go": "package main\n", "util.go": "package main\n"},
		"rust":    {"src/lib.rs": "pub fn x() {}\n"},
		"python":  {"src/app.py": "print('x')\n", "src/util.py": "x = 1\n", "src/extra.js": "1\n"},
		"java":    {"src/App.java": "class App {}\n"},
		"dotnet":  {"src/App.cs": "class App {}\n"},
		"ruby":    {"lib/app.rb": "puts 1\n"},
		"php":     {"src/index.php": "<?php\n"},
		"cpp":     {"src/main.cpp": "int main() {}\n"},
		"swift":   {"Sources/App/main.swift": "print(1)\n"},
		"dart":    {"lib/main.dart": "void main() {}\n"},
		"elixir":  {"lib/app.ex": "defmodule App do\nend\n"},
		"scala":   {"src/Main.scala": "object Main\n"},
		"haskell": {"src/Main.hs": "main = pure ()\n"},
		"zig":     {"src/main.zig": "pub fn main() void {}\n"},
		"lua":     {"src/main.lua": "print(1)\n"},
		"r":       {"R/plot.R": "plot(1)\n"},
		"julia":   {"src/main.jl": "println(1)\n"},
		"shell":   {"scripts/run.sh": "#!/bin/sh\n"},
	}
	for want, files := range cases {
		t.Run(want, func(t *testing.T) {
			detected := detectFixture(t, files, nil, nil)
			if detected.Language != want {
				t.Fatalf("language = %q, want %q", detected.Language, want)
			}
			if len(detected.Commands) != 0 {
				t.Fatalf("source-only project should not invent commands: %+v", detected.Commands)
			}
		})
	}
}

func TestDetectWrapperAndConfigSources(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"scripts":{"test":"node --test","build":"node build.js"}}`)
	writeFile(t, dir, "Makefile", "coverage:\n\ttrue\n")
	detected := DetectProject(dir)
	if detected.Commands["test"].Source != "package.json#scripts.test" {
		t.Fatalf("test source = %q", detected.Commands["test"].Source)
	}
	if detected.Commands["coverage"].Source != "Makefile#coverage" {
		t.Fatalf("coverage source = %q", detected.Commands["coverage"].Source)
	}
}

func TestMarshalDetection(t *testing.T) {
	detected := detectFixture(t, map[string]string{
		"go.mod": "module demo\n\ngo 1.23\n",
	}, nil, nil)
	encoded := MarshalDetection(detected)
	for _, want := range []string{`"language":"go"`, `"manifests":`, `"commands":`, `"test":{"command":"go test ./..."`} {
		if !strings.Contains(encoded, want) {
			t.Errorf("encoded detection is missing %q: %s", want, encoded)
		}
	}
}
