package core_test

import (
	"os"
	"strings"
	"testing"

	"github.com/havoc-rao/shell-rewrite/core"
)

func TestAddAliasBasic(t *testing.T) {
	cfg := core.NewConfig()
	st, err := cfg.AddAlias("c", "clear")
	if err != nil {
		t.Fatalf("AddAlias: %v", err)
	}
	if st != core.StatusAdded {
		t.Fatalf("want StatusAdded, got %v", st)
	}
	if got := cfg.Aliases["c"]; len(got) != 1 || got[0] != "clear" {
		t.Fatalf("Aliases[c] = %v, want [clear]", got)
	}
}

func TestAddAliasPrefix(t *testing.T) {
	cfg := core.NewConfig()
	if _, err := cfg.AddAlias("c+", "clear"); err != nil {
		t.Fatalf("AddAlias c+: %v", err)
	}
	// 前缀别名（c+）为每个前缀生成同名 wrapper，全部以 clear 为目标
	for _, name := range []string{"c", "cl", "cle", "clea"} {
		got := cfg.Expand([]string{name})
		if s := strings.Join(got, " "); s != "clear" {
			t.Fatalf("Expand(%s) = %q, want \"clear\"", name, s)
		}
	}
}

func TestAddAliasPrefixTooShort(t *testing.T) {
	cfg := core.NewConfig()
	if _, err := cfg.AddAlias("clear+", "clear"); err == nil {
		t.Fatal("expected error for prefix where base >= word length")
	}
}

func TestAddAliasConflictWithRoot(t *testing.T) {
	cfg := core.NewConfig()
	if _, err := cfg.Add("git", []string{"co"}, "checkout"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddAlias("git", "tig"); err == nil {
		t.Fatal("expected error: alias name conflicts with managed root command")
	}
}

func TestExpandAliasSimple(t *testing.T) {
	cfg := core.NewConfig()
	_, _ = cfg.AddAlias("c", "clear")
	got := cfg.Expand([]string{"c"})
	want := []string{"clear"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Expand([c]) = %v, want %v", got, want)
	}
}

func TestExpandAliasPassthroughArgs(t *testing.T) {
	cfg := core.NewConfig()
	_, _ = cfg.AddAlias("c", "clear")
	got := cfg.Expand([]string{"c", "-x"})
	want := []string{"clear", "-x"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Expand([c -x]) = %v, want %v", got, want)
	}
}

func TestExpandAliasDrillThrough(t *testing.T) {
	cfg := core.NewConfig()
	_, _ = cfg.Add("git", []string{"co"}, "checkout")
	_, _ = cfg.AddAlias("g", "git")
	// g co -b feat → git checkout -b feat
	got := cfg.Expand([]string{"g", "co", "-b", "feat"})
	want := []string{"git", "checkout", "-b", "feat"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Expand([g co -b feat]) = %v, want %v", got, want)
	}
}

func TestExpandAliasPrefix(t *testing.T) {
	cfg := core.NewConfig()
	_, _ = cfg.AddAlias("c+", "clear")
	// cl → clear (前缀命中)
	got := cfg.Expand([]string{"cl"})
	want := []string{"clear"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Expand([cl]) = %v, want %v", got, want)
	}
}

func TestExpandAliasNotManaged(t *testing.T) {
	cfg := core.NewConfig()
	// 无别名无规则的命令原样返回
	got := cfg.Expand([]string{"foo", "bar"})
	want := []string{"foo", "bar"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Expand([foo bar]) = %v, want %v", got, want)
	}
}

func TestGenAliasFuncDirectCommand(t *testing.T) {
	cfg := core.NewConfig()
	_, _ = cfg.AddAlias("c", "clear")
	out := cfg.GenPosixFuncs()
	// clear 无规则树 → command 直接执行 + 回显
	if !strings.Contains(out, "c() {") {
		t.Fatal("missing c() function")
	}
	if !strings.Contains(out, "command \"clear\" \"$@\"") {
		t.Fatalf("expected command clear, got:\n%s", out)
	}
	if !strings.Contains(out, "_shr_echo") {
		t.Fatal("expected _shr_echo for non-drill alias")
	}
}

func TestGenAliasFuncDrillThrough(t *testing.T) {
	cfg := core.NewConfig()
	_, _ = cfg.Add("git", []string{"co"}, "checkout")
	_, _ = cfg.AddAlias("g", "git")
	out := cfg.GenPosixFuncs()
	// git 有规则树 → 函数调用下钻，无 command/echo
	if !strings.Contains(out, "g() {") {
		t.Fatal("missing g() function")
	}
	if !strings.Contains(out, "git \"$@\"") {
		t.Fatalf("expected function-call drill-through, got:\n%s", out)
	}
	// 下钻不应有 command git（那是 git 函数自己的透传分支，不是 g 的）
	idx := strings.Index(out, "g() {")
	gBlock := out[idx:]
	end := strings.Index(gBlock, "}")
	gBlock = gBlock[:end+1]
	if strings.Contains(gBlock, "command") || strings.Contains(gBlock, "_shr_echo") {
		t.Fatalf("drill-through alias should not echo/command, got:\n%s", gBlock)
	}
}

func TestGenAliasFuncPrefix(t *testing.T) {
	cfg := core.NewConfig()
	_, _ = cfg.AddAlias("c+", "clear")
	out := cfg.GenPosixFuncs()
	for _, name := range []string{"c", "cl", "cle", "clea"} {
		if !strings.Contains(out, name+"() {") {
			t.Fatalf("missing %s() function in:\n%s", name, out)
		}
	}
}

func TestGenAliasDisabledKeepsNameReplace(t *testing.T) {
	cfg := core.NewConfig()
	_, _ = cfg.Add("git", []string{"co"}, "checkout")
	_, _ = cfg.AddAlias("g", "git")
	cfg.Enabled = false
	out := cfg.GenPosixFuncs()
	// shr off：g 仍替换为 git，但用 command（不下钻）
	if !strings.Contains(out, "command \"git\" \"$@\"") {
		t.Fatalf("disabled alias should still replace name via command, got:\n%s", out)
	}
	if strings.Contains(out, "g() {\n  git \"$@\"") {
		t.Fatal("disabled alias should not drill-through")
	}
}

func TestAliasTOMLRoundTrip(t *testing.T) {
	cfg := core.NewConfig()
	_, _ = cfg.Add("git", []string{"co"}, "checkout")
	_, _ = cfg.AddAlias("c", "clear")
	_, _ = cfg.AddAlias("g", "git")
	_, _ = cfg.AddAlias("c+", "clear")

	tmp := t.TempDir() + "/rules.toml"
	if err := cfg.SaveTo(tmp); err != nil {
		t.Fatal(err)
	}
	loaded, err := core.LoadFrom(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Aliases["c"]; len(got) != 1 || got[0] != "clear" {
		t.Fatalf("loaded Aliases[c] = %v", got)
	}
	if got := loaded.Aliases["g"]; len(got) != 1 || got[0] != "git" {
		t.Fatalf("loaded Aliases[g] = %v", got)
	}
	if got := loaded.Aliases["c+"]; len(got) != 1 || got[0] != "clear" {
		t.Fatalf("loaded Aliases[c+] = %v", got)
	}
}

func TestDoctorAliasConflict(t *testing.T) {
	// 手编 TOML 可能产生冲突：git 既是命令规则根又是别名（AddAlias 会拦截，
	// 但手编文件可绕过），doctor 应报告。
	toml := `[__shr]
enabled = true

[__shr.aliases]
git = "tig"

[git]
co = "checkout"
`
	tmp := t.TempDir() + "/rules.toml"
	if err := os.WriteFile(tmp, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := core.LoadFrom(tmp)
	if err != nil {
		t.Fatal(err)
	}
	issues := cfg.Doctor()
	found := false
	for _, is := range issues {
		if strings.Contains(is, "git") && strings.Contains(is, "同名") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected alias-root conflict in doctor, got: %v", issues)
	}
}

func TestRemoveAlias(t *testing.T) {
	cfg := core.NewConfig()
	_, _ = cfg.AddAlias("c", "clear")
	if !cfg.RemoveAlias("c") {
		t.Fatal("RemoveAlias returned false")
	}
	if _, ok := cfg.Aliases["c"]; ok {
		t.Fatal("alias not removed")
	}
	if cfg.RemoveAlias("c") {
		t.Fatal("RemoveAlias should return false for missing alias")
	}
}
