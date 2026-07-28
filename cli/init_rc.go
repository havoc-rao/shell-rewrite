// Package cli: shr init --install / --uninstall — 把集成行写入或移出 shell rc 文件。
//
// rc 中写入的形态（sentinel 块，便于精确清理）：
//
//	# >>> shr init >>>
//	eval "$(shr init zsh)"
//	# <<< shr init <<<
//
// 幂等：检测到任何已存在的 `shr init` 调用行（含手工 echo 添加的）即跳过；
// 卸载时移除 sentinel 块以及独立的手工调用行。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	rcBlockBegin = "# >>> shr init >>>"
	rcBlockEnd   = "# <<< shr init <<<"
)

// shrInitLineRe 匹配 rc 中独立的 `shr init` 调用整行。
// 注释行（以 # 开头）不算，避免误判 `# shr init` 这类说明性注释。
var shrInitLineRe = regexp.MustCompile(`(?m)^[ \t]*[^#\n].*shr\s+init[^\n]*\n?`)

// shrBlockRe 匹配 sentinel 块（含首尾标记行），用于卸载时整体移除。
var shrBlockRe = regexp.MustCompile(`(?ms)^# >>> shr init >>>.*?^# <<< shr init <<<\n?`)

// multiBlankRe 匹配 3 个及以上连续换行，卸载清理后压缩为最多 2 个。
var multiBlankRe = regexp.MustCompile(`\n{3,}`)

// installToRc 在 shell 的 rc 文件中安装或卸载集成行。
func installToRc(shell, rcOverride string, uninstall bool) int {
	if shell != "zsh" && shell != "bash" {
		fmt.Fprintf(os.Stderr, "shr init: --install/--uninstall only supports zsh/bash (got %q)\n", shell)
		return 1
	}
	rc := rcOverride
	if rc == "" {
		rc = defaultRcPath(shell)
	}
	if uninstall {
		return uninstallFromRc(rc)
	}
	return installIntoRc(shell, rc)
}

// defaultRcPath 返回 shell 默认的 rc 文件路径。
func defaultRcPath(shell string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "bash":
		return filepath.Join(home, ".bashrc")
	}
	return ""
}

// installIntoRc 把 sentinel 块追加到 rc（已存在则跳过）。
func installIntoRc(shell, rc string) int {
	data, _ := os.ReadFile(rc)
	content := string(data)

	if shrInitLineRe.MatchString(content) {
		fmt.Printf("shr: already installed in %s\n", rc)
		return 0
	}

	var b strings.Builder
	b.WriteString(content)
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		b.WriteByte('\n')
	}
	if len(content) > 0 {
		b.WriteByte('\n') // 与已有内容空行分隔
	}
	fmt.Fprintf(&b, "%s\n", rcBlockBegin)
	fmt.Fprintf(&b, "eval \"$(shr init %s)\"\n", shell)
	fmt.Fprintf(&b, "%s\n", rcBlockEnd)

	if err := os.WriteFile(rc, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	fmt.Printf("shr: installed into %s\n", rc)
	fmt.Printf("shr: restart your shell, or run now:  eval \"$(shr init %s)\"\n", shell)
	return 0
}

// uninstallFromRc 从 rc 移除 sentinel 块及独立的手工调用行。
func uninstallFromRc(rc string) int {
	data, err := os.ReadFile(rc)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("shr: %s does not exist; nothing to remove\n", rc)
			return 0
		}
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	content := string(data)

	out := shrBlockRe.ReplaceAllString(content, "")
	out = shrInitLineRe.ReplaceAllString(out, "")
	out = multiBlankRe.ReplaceAllString(out, "\n\n")
	out = strings.TrimRight(out, "\n") + "\n"

	if out == content {
		fmt.Printf("shr: no shr init entry found in %s\n", rc)
		return 0
	}
	if err := os.WriteFile(rc, []byte(out), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	fmt.Printf("shr: removed from %s\n", rc)
	fmt.Println("shr: restart your shell for changes to take effect")
	return 0
}
