package core

import (
	"strings"
	"testing"
)

func TestAddAliasBasic(t *testing.T) {
	cfg := NewConfig()
	st, err := cfg.AddAlias("c", "clear")
	if err != nil {
		t.Fatalf("AddAlias: %v", err)
	}
	if st != StatusAdded {
		t.Fatalf("want StatusAdded, got %v", st)
	}
	if got := cfg.Aliases["c"]; len(got) != 1 || got[0] != "clear" {
		t.Fatalf("Aliases[c] = %v, want [clear]", got)
	}
}

func TestAddAliasPrefix(t *testing.T) {
	cfg := NewConfig()
	if _, err := cfg.AddAlias("c+", "clear"); err != nil {
		t.Fatalf("AddAlias c+: %v", err)
	}
	names := cfg.aliasFuncNames("c+")
	want := []string{"c", "cl", "cle", "clea"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("aliasFuncNames = %v, want %v", names, want)
	}
}

func TestAddAliasPrefixTooShort(t *testing.T) {
	cfg := NewConfig()
	if _, err := cfg.AddAlias("clear+", "clear"); err == nil {
		t.Fatal("expected error for prefix where base >= word length")
	}
}

func TestAddAliasConflictWithRoot(t *testing.T) {
	cfg := NewConfig()
	if _, err := cfg.Add("git", []string{"co"}, "checkout"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddAlias("git", "tig"); err == nil {
		t.Fatal("expected error: alias name conflicts with managed root command")
	}
}

func TestExpandAliasSimple(t *testing.T) {
	cfg := NewConfig()
	_, _ = cfg.AddAlias("c", "clear")
	got := cfg.Expand([]string{"c"})
	want := []string{"clear"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Expand([c]) = %v, want %v", got, want)
	}
}

func TestExpandAliasPassthroughArgs(t *testing.T) {
	cfg := NewConfig()
	_, _ = cfg.AddAlias("c", "clear")
	got := cfg.Expand([]string{"c", "-x"})
	want := []string{"clear", "-x"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Expand([c -x]) = %v, want %v", got, want)
	}
}

func TestExpandAliasDrillThrough(t *testing.T) {
	cfg := NewConfig()
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
	cfg := NewConfig()
	_, _ = cfg.AddAlias("c+", "clear")
	// cl → clear (前缀命中)
	got := cfg.Expand([]string{"cl"})
	want := []string{"clear"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Expand([cl]) = %v, want %v", got, want)
	}
}

func TestExpandAliasNotManaged(t *testing.T) {
	cfg := NewConfig()
	// 无别名无规则的命令原样返回
	got := cfg.Expand([]string{"foo", "bar"})
	want := []string{"foo", "bar"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Expand([foo bar]) = %v, want %v", got, want)
	}
}

func TestGenAliasFuncDirectCommand(t *testing.T) {
	cfg := NewConfig()
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
	cfg := NewConfig()
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
	cfg := NewConfig()
	_, _ = cfg.AddAlias("c+", "clear")
	out := cfg.GenPosixFuncs()
	for _, name := range []string{"c", "cl", "cle", "clea"} {
		if !strings.Contains(out, name+"() {") {
			t.Fatalf("missing %s() function in:\n%s", name, out)
		}
	}
}

func TestGenAliasDisabledKeepsNameReplace(t *testing.T) {
	cfg := NewConfig()
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
	cfg := NewConfig()
	_, _ = cfg.Add("git", []string{"co"}, "checkout")
	_, _ = cfg.AddAlias("c", "clear")
	_, _ = cfg.AddAlias("g", "git")
	_, _ = cfg.AddAlias("c+", "clear")

	tmp := t.TempDir() + "/rules.toml"
	if err := cfg.SaveTo(tmp); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFrom(tmp)
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
	cfg := NewConfig()
	// 手工构造冲突：alias 名与 root 同名（正常 AddAlias 会拦截，但手编 TOML 可能产生）
	cfg.Roots["git"] = NewNode()
	cfg.Aliases["git"] = []string{"tig"}
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
	cfg := NewConfig()
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
