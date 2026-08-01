// Package core_test 提供 shr 核心逻辑的黑盒测试（外部包，仅通过导出 API 验证）。
package core_test

import (
	"strings"
	"testing"

	"github.com/havoc-rao/shell-rewrite/core"
)

// setupGitP 构造用户场景：git p = push | pull（多值规则）。
func setupGitP(t *testing.T) *core.Config {
	t.Helper()
	cfg := core.NewConfig()
	if _, err := cfg.Add("git", []string{"p"}, "push"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Add("git", []string{"p"}, "pull"); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestUniquePrefixCompletionShortest(t *testing.T) {
	cfg := setupGitP(t)
	// pus 是 push 的唯一前缀 → 补全为 push
	got := cfg.Expand([]string{"git", "pus"})
	if s := strings.Join(got, " "); s != "git push" {
		t.Fatalf("Expand(git pus) = %q, want \"git push\"", s)
	}
}

func TestUniquePrefixCompletionSecond(t *testing.T) {
	cfg := setupGitP(t)
	// pul 是 pull 的唯一前缀 → 补全为 pull
	got := cfg.Expand([]string{"git", "pul"})
	if s := strings.Join(got, " "); s != "git pull" {
		t.Fatalf("Expand(git pul) = %q, want \"git pull\"", s)
	}
}

func TestUniquePrefixCompletionAmbiguousPassthrough(t *testing.T) {
	cfg := setupGitP(t)
	// pu 是 push 与 pull 的公共前缀 → 无法唯一推断，保持透传
	got := cfg.Expand([]string{"git", "pu"})
	if s := strings.Join(got, " "); s != "git pu" {
		t.Fatalf("Expand(git pu) = %q, want \"git pu\"", s)
	}
}

func TestUniquePrefixCompletionNoMatch(t *testing.T) {
	cfg := setupGitP(t)
	// pz 不是任何候选词的前缀 → 透传
	got := cfg.Expand([]string{"git", "pz"})
	if s := strings.Join(got, " "); s != "git pz" {
		t.Fatalf("Expand(git pz) = %q, want \"git pz\"", s)
	}
}

func TestUniquePrefixCompletionExactRuleWins(t *testing.T) {
	cfg := setupGitP(t)
	// p 是精确多值规则：Expand 预览取首个候选（运行时弹 TUI 选择）
	got := cfg.Expand([]string{"git", "p"})
	if s := strings.Join(got, " "); s != "git push" {
		t.Fatalf("Expand(git p) = %q, want \"git push\"", s)
	}
}

func TestUniquePrefixCompletionArgsPassthrough(t *testing.T) {
	cfg := setupGitP(t)
	got := cfg.Expand([]string{"git", "pus", "-u", "origin"})
	if s := strings.Join(got, " "); s != "git push -u origin" {
		t.Fatalf("Expand = %q, want \"git push -u origin\"", s)
	}
}

func TestUniquePrefixCompletionFullWordPassthrough(t *testing.T) {
	cfg := setupGitP(t)
	// 完整词 push 不是严格前缀 → 不被推断，透传（git push 原样执行）
	got := cfg.Expand([]string{"git", "push"})
	if s := strings.Join(got, " "); s != "git push" {
		t.Fatalf("Expand(git push) = %q, want \"git push\"", s)
	}
}

func TestSingleValueNoInference(t *testing.T) {
	cfg := core.NewConfig()
	_, _ = cfg.Add("git", []string{"co"}, "checkout")
	// 单值规则不参与推断：che 没有多值候选 → 透传
	got := cfg.Expand([]string{"git", "che"})
	if s := strings.Join(got, " "); s != "git che" {
		t.Fatalf("Expand(git che) = %q, want passthrough", s)
	}
}

func TestGenFuncUniquePrefixInference(t *testing.T) {
	cfg := setupGitP(t)
	out := cfg.GenPosixFuncs()
	// 生成的 case 应包含 pus / pul 推断分支，各自补全并回显展开结果
	if !strings.Contains(out, "pus) shift; _shr_echo \"git\" \"push\" \"$@\"; command \"git\" \"push\" \"$@\"") {
		t.Fatalf("missing pus inference branch in:\n%s", out)
	}
	if !strings.Contains(out, "pul) shift; _shr_echo \"git\" \"pull\" \"$@\"; command \"git\" \"pull\" \"$@\"") {
		t.Fatalf("missing pul inference branch in:\n%s", out)
	}
	// 完整词 push/pull 应透传而非被推断分支捕获：整体透传分支必须存在
	if !strings.Contains(out, "*) command \"git\" \"$@\"") {
		t.Fatal("missing passthrough branch in generated function")
	}
}

func TestGenFuncInferenceNested(t *testing.T) {
	cfg := setupGitP(t)
	out := cfg.GenPosixFuncs()
	// 精确多值规则 p 分支必须保留（运行时弹 TUI）
	if !strings.Contains(out, "_shr_pick \"git p\" \"push\" \"pull\"") {
		t.Fatalf("multi-value rule branch lost, got:\n%s", out)
	}
	// 推断分支嵌套在失配 *) 内部：出现嵌套 case，且外层无碍
	if !strings.Contains(out, "*) case \"$1\" in") {
		t.Fatalf("expected nested inference case, got:\n%s", out)
	}
}
