package core_test

import (
	"strings"
	"testing"

	"github.com/havoc-rao/shell-rewrite/core"
)

// setupMultiPPlus 构造用户实际场景：git "p+" = ["pull", "push"]（多值前缀规则）。
func setupMultiPPlus(t *testing.T) *core.Config {
	t.Helper()
	cfg := core.NewConfig()
	if _, err := cfg.Add("git", []string{"p+"}, "pull"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Add("git", []string{"p+"}, "push"); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// 单值前缀规则 p+ = push：pus 直接展开
func TestSinglePrefixRule(t *testing.T) {
	cfg := core.NewConfig()
	_, _ = cfg.Add("git", []string{"p+"}, "push")
	got := cfg.Expand([]string{"git", "pus"})
	if s := strings.Join(got, " "); s != "git push" {
		t.Fatalf("Expand(git pus) = %q, want \"git push\"", s)
	}
	out := cfg.GenPosixFuncs()
	if !strings.Contains(out, "p|pu|pus)") {
		t.Fatalf("missing literal p|pu|pus pattern, got:\n%s", out)
	}
}

// 多值前缀规则：pus 是 push（而非 pull）的唯一前缀 → 直接确定
func TestMultiPrefixUniquePush(t *testing.T) {
	cfg := setupMultiPPlus(t)
	got := cfg.Expand([]string{"git", "pus"})
	if s := strings.Join(got, " "); s != "git push" {
		t.Fatalf("Expand(git pus) = %q, want \"git push\"", s)
	}
}

// 多值前缀规则：pul 是 pull 的唯一前缀 → 直接确定
func TestMultiPrefixUniquePull(t *testing.T) {
	cfg := setupMultiPPlus(t)
	got := cfg.Expand([]string{"git", "pul"})
	if s := strings.Join(got, " "); s != "git pull" {
		t.Fatalf("Expand(git pul) = %q, want \"git pull\"", s)
	}
}

// 多值前缀规则：pu 是 pull/push 的共享前缀 → 歧义（运行时弹 TUI）
func TestMultiPrefixSharedAmbiguous(t *testing.T) {
	cfg := setupMultiPPlus(t)
	got := cfg.Expand([]string{"git", "pu"})
	if s := strings.Join(got, " "); s != "git pull" {
		t.Fatalf("Expand(git pu) = %q, want \"git pull\" (preview first candidate)", s)
	}
}

// 多值前缀规则：p 是 base，命中后弹 TUI（label git p）
func TestMultiPrefixBaseTUI(t *testing.T) {
	cfg := setupMultiPPlus(t)
	out := cfg.GenPosixFuncs()
	if !strings.Contains(out, "_shr_pick \"git p\" \"pull\" \"push\"") {
		t.Fatalf("multi prefix p should TUI, got:\n%s", out)
	}
}

// 多值前缀规则：完整词 push/pull 透传（非严格前缀）
func TestMultiPrefixFullWordPassthrough(t *testing.T) {
	cfg := setupMultiPPlus(t)
	for _, full := range []string{"push", "pull"} {
		got := cfg.Expand([]string{"git", full})
		if s := strings.Join(got, " "); s != "git "+full {
			t.Fatalf("Expand(git %s) = %q, want passthrough", full, s)
		}
	}
}

// 生成的 shell 代码：外层仅为共享前缀 p|pu（TUI），唯一前缀由嵌套推断分支补全
func TestMultiPrefixGenCode(t *testing.T) {
	cfg := setupMultiPPlus(t)
	out := cfg.GenPosixFuncs()
	if !strings.Contains(out, "p|pu)") {
		t.Fatalf("expected shared-prefix pattern p|pu, got:\n%s", out)
	}
	// 唯一前缀由失配 *) 内的嵌套推断补全，且必须回显展开后的完整命令
	if !strings.Contains(out, "pus) shift; _shr_echo \"git\" \"push\" \"$@\"; command \"git\" \"push\" \"$@\"") {
		t.Fatalf("missing pus inference branch with echo, got:\n%s", out)
	}
	if !strings.Contains(out, "pul) shift; _shr_echo \"git\" \"pull\" \"$@\"; command \"git\" \"pull\" \"$@\"") {
		t.Fatalf("missing pul inference branch with echo, got:\n%s", out)
	}
}
