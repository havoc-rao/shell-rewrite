package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/havoc-rao/shell-rewrite/core"
)

func TestFindProjectRootWalkUp(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(filepath.Join(proj, ".shr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".shr", "rules.toml"), []byte("[git]\nco = \"checkout\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	depth := filepath.Join(proj, "sub", "deep", "x")
	rootGot, pathGot := core.FindProjectRoot(depth, ".shr")
	want := filepath.Join(proj, ".shr", "rules.toml")
	if rootGot != proj || pathGot != want {
		t.Fatalf("FindProjectRoot(%q) = (%q, %q), want (%q, %q)", depth, rootGot, pathGot, proj, want)
	}
	if _, p := core.FindProjectRoot(root, ".shr"); p != "" {
		t.Fatalf("unexpected project config found above root: %s", p)
	}
}

func TestFindProjectRootViaGitMarker(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "repo", "sub")
	if err := os.MkdirAll(filepath.Join(root, "repo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	rootGot, pathGot := core.FindProjectRoot(proj, ".shr")
	repo := filepath.Join(root, "repo")
	if rootGot != repo || pathGot != filepath.Join(repo, ".shr", "rules.toml") {
		t.Fatalf("git repo should be a project: got (%q, %q), want (%q, %q)", rootGot, pathGot, repo, filepath.Join(repo, ".shr", "rules.toml"))
	}
}

func TestFindProjectRootViaProjectDir(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "p")
	if err := os.MkdirAll(filepath.Join(proj, ".shr"), 0o755); err != nil {
		t.Fatal(err)
	}
	rootGot, pathGot := core.FindProjectRoot(proj, ".shr")
	if rootGot != proj || pathGot != filepath.Join(proj, ".shr", "rules.toml") {
		t.Fatalf("existing <project_dir> directory should be a project: got (%q, %q)", rootGot, pathGot)
	}
}

func TestScopeProjectOverridesUser(t *testing.T) {
	base := t.TempDir()
	userFile := filepath.Join(base, "rules.toml")
	if err := os.WriteFile(userFile, []byte("[git]\nco = \"checkout\"\nlg = \"log\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projRoot := filepath.Join(base, "proj")
	if err := os.MkdirAll(filepath.Join(projRoot, ".shr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projRoot, ".shr", "rules.toml"), []byte("[git]\nco = \"commit\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scope, err := core.LoadScopedAt(filepath.Join(projRoot, "sub"), userFile)
	if err != nil {
		t.Fatal(err)
	}
	// 同名键项目级优先
	if got := strings.Join(scope.Merged.Expand([]string{"git", "co"}), " "); got != "git commit" {
		t.Fatalf("project rule should override user: got %q", got)
	}
	// 用户独有键保留
	if got := strings.Join(scope.Merged.Expand([]string{"git", "lg"}), " "); got != "git log" {
		t.Fatalf("user rule should be kept: got %q", got)
	}
	if scope.ProjectPath == "" {
		t.Fatal("project scope not detected")
	}
	if scope.TargetConfig != scope.ProjectConfig {
		t.Fatal("target should be the project config inside a project")
	}
}

func TestScopeGitRepoDefaultsToProjectTarget(t *testing.T) {
	base := t.TempDir()
	userFile := filepath.Join(base, "rules.toml")
	if err := os.WriteFile(userFile, []byte("[git]\nco = \"checkout\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 用户场景：git 仓库内尚无 .shr/rules.toml，写类命令默认应落在项目级
	scope, err := core.LoadScopedAt(filepath.Join(repo, "sub", "deep"), userFile)
	if err != nil {
		t.Fatal(err)
	}
	if scope.ProjectPath == "" {
		t.Fatal("git repo should be detected as a project")
	}
	if scope.TargetConfig != scope.ProjectConfig || scope.TargetPath != scope.ProjectPath {
		t.Fatalf("default write target should be the project: target=%s", scope.TargetPath)
	}
	if got := strings.Join(scope.Merged.Expand([]string{"git", "co"}), " "); got != "git checkout" {
		t.Fatalf("merged view should keep user rules: %q", got)
	}
}

func TestScopeNoProjectKeepsUserTarget(t *testing.T) {
	base := t.TempDir()
	userFile := filepath.Join(base, "rules.toml")
	if err := os.WriteFile(userFile, []byte("[git]\nco = \"checkout\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scope, err := core.LoadScopedAt(base, userFile)
	if err != nil {
		t.Fatal(err)
	}
	if scope.ProjectPath != "" {
		t.Fatalf("unexpected project: %s", scope.ProjectPath)
	}
	if scope.TargetConfig != scope.UserConfig {
		t.Fatal("outside a project, target should be the user config")
	}
	if got := strings.Join(scope.Merged.Expand([]string{"git", "co"}), " "); got != "git checkout" {
		t.Fatalf("user rule missing: %q", got)
	}
}

func TestScopeMetaEnabledOverride(t *testing.T) {
	base := t.TempDir()
	userFile := filepath.Join(base, "rules.toml")
	if err := os.WriteFile(userFile, []byte("[__shr]\nenabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projRoot := filepath.Join(base, "p")
	if err := os.MkdirAll(filepath.Join(projRoot, ".shr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projRoot, ".shr", "rules.toml"), []byte("[__shr]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scope, err := core.LoadScopedAt(projRoot, userFile)
	if err != nil {
		t.Fatal(err)
	}
	if scope.Merged.Enabled {
		t.Fatal("project [__shr] enabled=false should disable rewriting")
	}
	if !scope.UserConfig.Enabled {
		t.Fatal("user config should remain enabled")
	}
}

func TestScopeCustomProjectDir(t *testing.T) {
	base := t.TempDir()
	userFile := filepath.Join(base, "rules.toml")
	if err := os.WriteFile(userFile, []byte("[__shr]\nproject_dir = \".vscode/shr\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projRoot := filepath.Join(base, "proj")
	if err := os.MkdirAll(filepath.Join(projRoot, ".vscode", "shr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projRoot, ".vscode", "shr", "rules.toml"), []byte("[git]\nco = \"checkout\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scope, err := core.LoadScopedAt(projRoot, userFile)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(projRoot, ".vscode", "shr", "rules.toml")
	if scope.ProjectPath != want {
		t.Fatalf("custom project_dir not honored: got %q, want %q", scope.ProjectPath, want)
	}
	if got := strings.Join(scope.Merged.Expand([]string{"git", "co"}), " "); got != "git checkout" {
		t.Fatalf("custom project rules not merged: %q", got)
	}
}

func TestProjectSaveDoesNotInjectMeta(t *testing.T) {
	base := t.TempDir()
	projRoot := filepath.Join(base, "p")
	if err := os.MkdirAll(filepath.Join(projRoot, ".shr"), 0o755); err != nil {
		t.Fatal(err)
	}
	projFile := filepath.Join(projRoot, ".shr", "rules.toml")
	if err := os.WriteFile(projFile, []byte("[git]\nco = \"commit\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scope, err := core.LoadScopedAt(projRoot, filepath.Join(base, "rules.toml"))
	if err != nil {
		t.Fatal(err)
	}
	// 项目级配置尚未显式设置 meta → 保存不写 [__shr]
	if err := scope.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(projFile)
	if strings.Contains(string(data), "[__shr]") {
		t.Fatalf("project save should not inject meta, got:\n%s", data)
	}
	// 显式设置后保存 → 写入 [__shr]
	scope.ProjectConfig.EnabledSet = true
	scope.ProjectConfig.Enabled = false
	if err := scope.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(projFile)
	if !strings.Contains(string(data), "[__shr]") || !strings.Contains(string(data), "enabled = false") {
		t.Fatalf("project save should write explicitly-set meta, got:\n%s", data)
	}
}
