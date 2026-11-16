package gates

import (
	"encoding/xml"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/soub4i/forge/internal/util"
)

var versionPattern = regexp.MustCompile(`^\d+(?:\.\d+){0,3}(?:[-+][0-9A-Za-z][0-9A-Za-z.+-]*)?$`)

var (
	gradleVersionPattern   = regexp.MustCompile(`(?m)^\s*version\s*[=:]\s*['"]([^'"]+)['"]`)
	gradleBarePattern      = regexp.MustCompile(`(?m)^\s*version\s+['"]([^'"]+)['"]`)
	sbtVersionPattern      = regexp.MustCompile(`(?m)^\s*version\s*:=\s*"([^"]+)"`)
	mixVersionPattern      = regexp.MustCompile(`(?m)\bversion:\s*"([^"]+)"`)
	gemspecVersionPattern  = regexp.MustCompile(`(?m)\.version\s*=\s*['"]([^'"]+)['"]`)
	rubyVersionPattern     = regexp.MustCompile(`(?m)^\s*VERSION\s*=\s*['"]([^'"]+)['"]`)
	cmakeVersionPattern    = regexp.MustCompile(`(?i)\bproject\s*\([^)\n]*?\bVERSION\s+["']?([0-9][^"')\s]*)`)
	mesonVersionPattern    = regexp.MustCompile(`(?i)\bproject\s*\([^)\n]*?\bversion\s*:\s*['"]([^'"]+)['"]`)
	autotoolsBracket       = regexp.MustCompile(`(?i)AC_INIT\s*\(\s*\[[^]]+\]\s*,\s*\[([^]]+)\]`)
	autotoolsBare          = regexp.MustCompile(`(?i)AC_INIT\s*\(\s*[^,\s]+\s*,\s*([^,\s)]+)`)
	descriptionVersion     = regexp.MustCompile(`(?m)^Version:\s*(\S+)`)
	setupPyVersionPattern  = regexp.MustCompile(`(?m)\bversion\s*=\s*['"]([^'"]+)['"]`)
	zigVersionPattern      = regexp.MustCompile(`(?m)\.version\s*=\s*"([^"]+)"`)
	setupCfgVersionPattern = regexp.MustCompile(`(?m)^\s*version\s*=\s*(\S+)`)
)

// ProjectVersion returns the version declared by the project's primary
// manifest. It recognizes the common manifest formats Forge can detect and
// returns an empty string when no valid version is declared.
func ProjectVersion(cwd string) string {
	if version := jsonManifestVersion(cwd, "package.json"); version != "" {
		return version
	}
	if version := jsonManifestVersion(cwd, "composer.json"); version != "" {
		return version
	}
	if version := jsonManifestVersion(cwd, "deno.json"); version != "" {
		return version
	}
	if version := tomlSectionVersion(util.ReadText(filepath.Join(cwd, "Cargo.toml")), "package"); version != "" {
		return version
	}
	pyproject := util.ReadText(filepath.Join(cwd, "pyproject.toml"))
	if version := tomlSectionVersion(pyproject, "project"); version != "" {
		return version
	}
	if version := tomlSectionVersion(pyproject, "tool.poetry"); version != "" {
		return version
	}
	if version := tomlSectionVersion(util.ReadText(filepath.Join(cwd, "Project.toml")), ""); version != "" {
		return version
	}
	if version := setupCfgVersion(util.ReadText(filepath.Join(cwd, "setup.cfg"))); version != "" {
		return version
	}
	if version := firstMatch(setupPyVersionPattern, util.ReadText(filepath.Join(cwd, "setup.py"))); version != "" {
		return version
	}
	if version := pomVersion(util.ReadText(filepath.Join(cwd, "pom.xml"))); version != "" {
		return version
	}
	for _, name := range []string{"build.gradle", "build.gradle.kts"} {
		text := util.ReadText(filepath.Join(cwd, name))
		if version := firstMatch(gradleVersionPattern, text); version != "" {
			return version
		}
		if version := firstMatch(gradleBarePattern, text); version != "" {
			return version
		}
	}
	if version := dotnetVersion(cwd); version != "" {
		return version
	}
	if version := rubyVersion(cwd); version != "" {
		return version
	}
	if version := yamlTopLevelVersion(util.ReadText(filepath.Join(cwd, "pubspec.yaml"))); version != "" {
		return version
	}
	if version := firstMatch(mixVersionPattern, util.ReadText(filepath.Join(cwd, "mix.exs"))); version != "" {
		return version
	}
	if version := firstMatch(sbtVersionPattern, util.ReadText(filepath.Join(cwd, "build.sbt"))); version != "" {
		return version
	}
	if version := firstMatch(descriptionVersion, util.ReadText(filepath.Join(cwd, "DESCRIPTION"))); version != "" {
		return version
	}
	for _, name := range []string{"CMakeLists.txt", "cmake/CMakeLists.txt"} {
		if version := firstMatch(cmakeVersionPattern, util.ReadText(filepath.Join(cwd, name))); version != "" {
			return version
		}
	}
	for _, name := range []string{"configure.ac", "configure.in"} {
		text := util.ReadText(filepath.Join(cwd, name))
		if version := firstMatch(autotoolsBracket, text); version != "" {
			return version
		}
		if version := firstMatch(autotoolsBare, text); version != "" {
			return version
		}
	}
	if version := firstMatch(mesonVersionPattern, util.ReadText(filepath.Join(cwd, "meson.build"))); version != "" {
		return version
	}
	if version := firstMatch(zigVersionPattern, util.ReadText(filepath.Join(cwd, "build.zig.zon"))); version != "" {
		return version
	}
	return ""
}

func cleanVersion(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '#'); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}
	value = strings.Trim(value, `"'`)
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	if versionPattern.MatchString(value) {
		return value
	}
	return ""
}

func firstMatch(pattern *regexp.Regexp, text string) string {
	if text == "" {
		return ""
	}
	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			if version := cleanVersion(match[1]); version != "" {
				return version
			}
		}
	}
	return ""
}

func jsonManifestVersion(cwd, name string) string {
	var manifest map[string]any
	if err := util.ReadJSON(filepath.Join(cwd, name), &manifest); err != nil {
		return ""
	}
	value, _ := manifest["version"].(string)
	return cleanVersion(value)
}

func tomlSectionVersion(text, section string) string {
	if text == "" {
		return ""
	}
	current := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			current = strings.Trim(strings.TrimSpace(trimmed), "[]")
			continue
		}
		if current != section {
			continue
		}
		if value, ok := tomlKeyValue(trimmed, "version"); ok {
			if version := cleanVersion(value); version != "" {
				return version
			}
		}
	}
	return ""
}

func tomlKeyValue(line, key string) (string, bool) {
	if !strings.HasPrefix(line, key) {
		return "", false
	}
	rest := strings.TrimSpace(line[len(key):])
	if rest == "" || rest[0] != '=' {
		return "", false
	}
	rest = strings.TrimSpace(rest[1:])
	if rest == "" {
		return "", false
	}
	return rest, true
}

func setupCfgVersion(text string) string {
	if text == "" {
		return ""
	}
	inMetadata := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inMetadata = strings.EqualFold(strings.Trim(trimmed, "[]"), "metadata")
			continue
		}
		if inMetadata {
			if version := firstMatch(setupCfgVersionPattern, trimmed); version != "" {
				return version
			}
		}
	}
	return ""
}

func pomVersion(text string) string {
	if text == "" {
		return ""
	}
	var project struct {
		Version string `xml:"version"`
		Parent  struct {
			Version string `xml:"version"`
		} `xml:"parent"`
	}
	if err := xml.Unmarshal([]byte(text), &project); err != nil {
		return ""
	}
	if version := cleanVersion(project.Version); version != "" {
		return version
	}
	return cleanVersion(project.Parent.Version)
}

func dotnetVersion(cwd string) string {
	for _, extension := range []string{"*.csproj", "*.fsproj", "*.vbproj"} {
		matches, _ := filepath.Glob(filepath.Join(cwd, extension))
		sort.Strings(matches)
		for _, path := range matches {
			text := util.ReadText(path)
			if text == "" {
				continue
			}
			var project struct {
				PropertyGroups []struct {
					Version        string `xml:"Version"`
					VersionPrefix  string `xml:"VersionPrefix"`
					PackageVersion string `xml:"PackageVersion"`
				} `xml:"PropertyGroup"`
			}
			if err := xml.Unmarshal([]byte(text), &project); err != nil {
				continue
			}
			for _, group := range project.PropertyGroups {
				for _, value := range []string{group.Version, group.VersionPrefix, group.PackageVersion} {
					if version := cleanVersion(value); version != "" {
						return version
					}
				}
			}
		}
	}
	return ""
}

func rubyVersion(cwd string) string {
	matches, _ := filepath.Glob(filepath.Join(cwd, "*.gemspec"))
	sort.Strings(matches)
	for _, path := range matches {
		if version := firstMatch(gemspecVersionPattern, util.ReadText(path)); version != "" {
			return version
		}
	}
	files, _ := util.WalkFiles(cwd, 5000, 6, 1<<20)
	for _, file := range files {
		if filepath.Base(file.Rel) != "version.rb" {
			continue
		}
		if version := firstMatch(rubyVersionPattern, util.ReadText(filepath.Join(cwd, filepath.FromSlash(file.Rel)))); version != "" {
			return version
		}
	}
	return ""
}

func yamlTopLevelVersion(text string) string {
	if text == "" {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' || strings.HasPrefix(line, "#") {
			continue
		}
		value, ok := yamlKeyValue(line, "version")
		if !ok {
			continue
		}
		if index := strings.IndexByte(value, '+'); index > 0 {
			value = value[:index]
		}
		if version := cleanVersion(value); version != "" {
			return version
		}
	}
	return ""
}

func yamlKeyValue(line, key string) (string, bool) {
	if !strings.HasPrefix(line, key) {
		return "", false
	}
	rest := strings.TrimSpace(line[len(key):])
	if !strings.HasPrefix(rest, ":") {
		return "", false
	}
	return strings.TrimSpace(rest[1:]), true
}
