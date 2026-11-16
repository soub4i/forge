package gates

import "testing"

func TestProjectVersion(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "package.json",
			files: map[string]string{"package.json": `{"name":"demo","version":"1.2.3"}`},
			want:  "1.2.3",
		},
		{
			name:  "composer.json",
			files: map[string]string{"composer.json": `{"name":"demo","version":"2.0.0"}`},
			want:  "2.0.0",
		},
		{
			name:  "deno.json",
			files: map[string]string{"deno.json": `{"version":"1.4.0"}`},
			want:  "1.4.0",
		},
		{
			name:  "cargo ignores dependency versions",
			files: map[string]string{"Cargo.toml": "[dependencies]\nserde = { version = \"9.9.9\" }\n\n[package]\nname = \"demo\"\nversion = \"0.3.1\"\n"},
			want:  "0.3.1",
		},
		{
			name:  "pyproject project",
			files: map[string]string{"pyproject.toml": "[build-system]\nrequires = [\"setuptools\"]\n\n[project]\nname = \"demo\"\nversion = \"3.1.4\"\n"},
			want:  "3.1.4",
		},
		{
			name:  "pyproject poetry",
			files: map[string]string{"pyproject.toml": "[tool.poetry]\nname = \"demo\"\nversion = \"0.9.0\"\n"},
			want:  "0.9.0",
		},
		{
			name:  "julia project",
			files: map[string]string{"Project.toml": "name = \"Demo\"\nversion = \"0.2.0\"\n\n[deps]\n"},
			want:  "0.2.0",
		},
		{
			name:  "setup.cfg metadata",
			files: map[string]string{"setup.cfg": "[options]\nversion = 9.9.9\n\n[metadata]\nname = demo\nversion = 1.0.1\n"},
			want:  "1.0.1",
		},
		{
			name:  "setup.py",
			files: map[string]string{"setup.py": "setup(name='demo', version='4.2.0')\n"},
			want:  "4.2.0",
		},
		{
			name:  "maven project version",
			files: map[string]string{"pom.xml": `<project><modelVersion>4.0.0</modelVersion><parent><version>1.0.0</version></parent><artifactId>demo</artifactId><version>2.3.4</version></project>`},
			want:  "2.3.4",
		},
		{
			name:  "maven parent fallback",
			files: map[string]string{"pom.xml": `<project><modelVersion>4.0.0</modelVersion><parent><version>5.6.7</version></parent><artifactId>demo</artifactId></project>`},
			want:  "5.6.7",
		},
		{
			name:  "maven property is not a version",
			files: map[string]string{"pom.xml": `<project><version>${revision}</version></project>`},
			want:  "",
		},
		{
			name:  "gradle kotlin",
			files: map[string]string{"build.gradle.kts": "plugins { java }\nversion = \"1.8.2\"\n"},
			want:  "1.8.2",
		},
		{
			name:  "gradle groovy",
			files: map[string]string{"build.gradle": "plugins { id 'java' }\nversion '2.2.0'\n"},
			want:  "2.2.0",
		},
		{
			name:  "dotnet",
			files: map[string]string{"demo.csproj": `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework><Version>6.0.1</Version></PropertyGroup></Project>`},
			want:  "6.0.1",
		},
		{
			name: "gemspec constant",
			files: map[string]string{
				"demo.gemspec":        "Gem::Specification.new do |spec|\n  spec.version = Demo::VERSION\nend\n",
				"lib/demo/version.rb": "module Demo\n  VERSION = \"1.5.0\"\nend\n",
			},
			want: "1.5.0",
		},
		{
			name:  "pubspec strips build metadata",
			files: map[string]string{"pubspec.yaml": "name: demo\nversion: 3.0.2+17\n"},
			want:  "3.0.2",
		},
		{
			name:  "mix",
			files: map[string]string{"mix.exs": "def project do\n  [app: :demo, version: \"0.7.3\"]\nend\n"},
			want:  "0.7.3",
		},
		{
			name:  "sbt",
			files: map[string]string{"build.sbt": "name := \"demo\"\nversion := \"2.1.0\"\n"},
			want:  "2.1.0",
		},
		{
			name:  "R DESCRIPTION",
			files: map[string]string{"DESCRIPTION": "Package: demo\nVersion: 0.4.2\n"},
			want:  "0.4.2",
		},
		{
			name:  "cmake",
			files: map[string]string{"CMakeLists.txt": "cmake_minimum_required(VERSION 3.20)\nproject(demo VERSION 1.9.0 LANGUAGES CXX)\n"},
			want:  "1.9.0",
		},
		{
			name:  "autotools brackets",
			files: map[string]string{"configure.ac": "AC_INIT([demo], [2.5.1], [bugs@example.com])\n"},
			want:  "2.5.1",
		},
		{
			name:  "autotools bare",
			files: map[string]string{"configure.ac": "AC_INIT(demo, 2.6.0, bugs@example.com)\n"},
			want:  "2.6.0",
		},
		{
			name:  "meson",
			files: map[string]string{"meson.build": "project('demo', 'cpp', version: '1.1.1')\n"},
			want:  "1.1.1",
		},
		{
			name:  "zig",
			files: map[string]string{"build.zig.zon": ".{\n    .name = \"demo\",\n    .version = \"0.0.9\",\n}\n"},
			want:  "0.0.9",
		},
		{
			name:  "missing version",
			files: map[string]string{"package.json": `{"name":"demo"}`},
			want:  "",
		},
		{
			name:  "empty project",
			files: map[string]string{},
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tc.files {
				writeFile(t, dir, rel, content)
			}
			if got := ProjectVersion(dir); got != tc.want {
				t.Fatalf("ProjectVersion = %q, want %q", got, tc.want)
			}
		})
	}
}
