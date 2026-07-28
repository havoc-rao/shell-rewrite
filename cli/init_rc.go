// Package cli: shell rc 文件读写（供 shr setup 复用）。
//
// rc 中写入的形态（sentinel 块，便于精确清理）：
//
//	# >>> shr init >>>
//	case ":$PATH:" in *":<bindir>:"*) ;; *) export PATH="<bindir>:$PATH" ;; esac   # 仅 bindir 非空
//	eval "$(shr init <shell>)"
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

// applyInstall 执行实际的 rc 写入，返回状态消息（供 CLI 与 TUI 复用，不直接打印）。
// binDir 非空时在块内加一行运行时幂等的 PATH 守卫。
func applyInstall(shell, rc, binDir string) (string, error) {
	data, _ := os.ReadFile(rc)
	content := string(data)

	if shrInitLineRe.MatchString(content) {
		return fmt.Sprintf("already installed in %s", rc), nil
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
	if binDir != "" {
		// 运行时幂等地把 binDir 加入 PATH（已存在则跳过，避免每次启动重复追加）
		fmt.Fprintf(&b, "case \":$PATH:\" in *\":%s:\"*) ;; *) export PATH=\"%s:$PATH\" ;; esac\n", binDir, binDir)
	}
	fmt.Fprintf(&b, "eval \"$(shr init %s)\"\n", shell)
	fmt.Fprintf(&b, "%s\n", rcBlockEnd)

	if err := os.WriteFile(rc, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("installed into %s", rc), nil
}

// applyUninstall 从 rc 移除 sentinel 块及独立的手工调用行。
func applyUninstall(rc string) (string, error) {
	data, err := os.ReadFile(rc)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("%s does not exist; nothing to remove", rc), nil
		}
		return "", err
	}
	content := string(data)

	out := shrBlockRe.ReplaceAllString(content, "")
	out = shrInitLineRe.ReplaceAllString(out, "")
	out = multiBlankRe.ReplaceAllString(out, "\n\n")
	out = strings.TrimRight(out, "\n") + "\n"

	if out == content {
		return fmt.Sprintf("no shr init entry found in %s", rc), nil
	}
	if err := os.WriteFile(rc, []byte(out), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("removed from %s", rc), nil
}
