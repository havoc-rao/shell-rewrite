// Package cli: 多值缩写选择记忆——记住每个歧义点上次选中的候选，
// 下次弹窗时光标默认停在该候选上。缓存独立于 rules.toml，避免写入记忆时
// 改变 rules.toml 的 mtime 而触发 wrapper 热加载（无谓地重新 eval _gen posix）。
//
// 文件格式（TOML）：
//
//	# shr pick history — last selection per ambiguous abbrev (auto-managed)
//	["git p"]
//	last = "pull"
//
//	["colink data p"]
//	last = "push"
package cli

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// picksPath 返回选择记忆文件路径：与 rules.toml 同目录下的 picks.toml。
func picksPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "shr", "picks.toml")
}

// loadPicks 解码 picks.toml 为原始 map（label → {last: value}）。
// 文件不存在或损坏时返回空 map（静默忽略错误，记忆只是锦上添花）。
func loadPicks() map[string]interface{} {
	raw := map[string]interface{}{}
	_, _ = toml.DecodeFile(picksPath(), &raw)
	return raw
}

// pickLast 从记忆中取出 label 上次选中的值；无记录返回 ""。
func pickLast(raw map[string]interface{}, label string) string {
	if tbl, ok := raw[label].(map[string]interface{}); ok {
		if last, ok := tbl["last"].(string); ok {
			return last
		}
	}
	return ""
}

// savePick 记录 label 的上次选中值，保留其它 label 的记忆后写回。
func savePick(label, value string) {
	raw := loadPicks()
	raw[label] = map[string]interface{}{"last": value}
	var buf bytes.Buffer
	buf.WriteString("# shr pick history — last selection per ambiguous abbrev (auto-managed)\n")
	if err := toml.NewEncoder(&buf).Encode(raw); err != nil {
		return
	}
	p := picksPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, buf.Bytes(), 0o644)
}
