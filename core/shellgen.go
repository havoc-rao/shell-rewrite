package core

import (
	"fmt"
	"sort"
	"strings"
)

// GenInit 输出 shell 集成代码（wrapper 函数 + 热加载 hook），供 eval "$(shr init zsh)" 使用。
func (c *Config) GenInit(shell string) (string, error) {
	var b strings.Builder
	switch shell {
	case "zsh", "bash":
		fmt.Fprintf(&b, "# >>> shr init (%s) >>>\n", shell)
		fmt.Fprintf(&b, "# rules: %s\n", Path())
		b.WriteString("# note: existing aliases take precedence over functions — unalias if needed.\n\n")
		b.WriteString(posixPrelude)
		b.WriteString(c.GenPosixFuncs())
		b.WriteString(posixReloadFunc)
		if shell == "zsh" {
			b.WriteString(zshHook)
		} else {
			b.WriteString(bashHook)
		}
		b.WriteString("# <<< shr init <<<\n")
		return b.String(), nil
	default:
		return "", fmt.Errorf("暂不支持 shell %q（可用: zsh, bash）", shell)
	}
}

const posixPrelude = `_SHR_CONFIG="${XDG_CONFIG_HOME:-$HOME/.config}/shr/rules.toml"

_shr_mtime() {
  # macOS/BSD stat 与 GNU stat 兼容
  stat -f %m "$_SHR_CONFIG" 2>/dev/null || stat -c %Y "$_SHR_CONFIG" 2>/dev/null || echo 0
}

`

const posixReloadFunc = `_shr_reload_if_stale() {
  command -v shr >/dev/null 2>&1 || return 0
  local m
  m=$(_shr_mtime) || return 0
  if [ "$m" != "$_SHR_LOADED_MTIME" ]; then
    _SHR_LOADED_MTIME="$m"
    eval "$(shr _gen posix)"
  fi
}
_SHR_LOADED_MTIME=$(_shr_mtime)

`

const zshHook = `autoload -Uz add-zsh-hook
add-zsh-hook precmd _shr_reload_if_stale
`

const bashHook = `case ";${PROMPT_COMMAND:-};" in
  *";_shr_reload_if_stale;"*) ;;
  *) PROMPT_COMMAND="_shr_reload_if_stale${PROMPT_COMMAND:+;$PROMPT_COMMAND}" ;;
esac
`

// GenPosixFuncs 生成全部 wrapper 函数（bash/zsh 通用），热加载时重新 eval 即可生效。
func (c *Config) GenPosixFuncs() string {
	var b strings.Builder
	for _, cmd := range c.SortedRoots() {
		b.WriteString(genFunc(cmd, c.Roots[cmd]))
		b.WriteString("\n")
	}
	return b.String()
}

func genFunc(cmd string, node *Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s() {\n", cmd)
	genCase(&b, node, []string{cmd}, 1)
	b.WriteString("}\n")
	return b.String()
}

// genCase 把一层规则树机械翻译成嵌套 case：
//
//	缩写 → 无子表：  abbr) shift; command <prefix> <expansion> "$@" ;;
//	缩写 → 子表名：  abbr|name) shift; <递归 case> ;;
//	纯命名空间：     name) shift; <递归 case> ;;
//	失配：           *) command <prefix> "$@" ;;   （整体透传）
func genCase(b *strings.Builder, node *Node, prefix []string, ind int) {
	pad := strings.Repeat("  ", ind)
	b.WriteString(pad + `case "$1" in` + "\n")

	merged := map[string]bool{} // 已被缩写分支覆盖的子表名

	for _, abbr := range sortedKeys(node.Rules) {
		toks := strings.Fields(node.Rules[abbr])
		child, drill := node.Children[toks[0]]
		if drill && len(toks) == 1 {
			fmt.Fprintf(b, "%s  %s|%s)\n", pad, abbr, toks[0])
			b.WriteString(pad + "    shift\n")
			genCase(b, child, appendToks(prefix, toks[0]), ind+2)
			b.WriteString(pad + "    ;;\n")
			merged[toks[0]] = true
			continue
		}
		fmt.Fprintf(b, "%s  %s)\n", pad, abbr)
		b.WriteString(pad + "    shift\n")
		fmt.Fprintf(b, "%s    command %s \"$@\"\n", pad, quoteAll(appendToks(prefix, toks...)))
		b.WriteString(pad + "    ;;\n")
	}

	for _, name := range sortedKeys(node.Children) {
		if merged[name] {
			continue
		}
		if _, clash := node.Rules[name]; clash {
			continue // 手工编辑造成的歧义：doctor 会报告；此处缩写优先
		}
		fmt.Fprintf(b, "%s  %s)\n", pad, name)
		b.WriteString(pad + "    shift\n")
		genCase(b, node.Children[name], appendToks(prefix, name), ind+2)
		b.WriteString(pad + "    ;;\n")
	}

	fmt.Fprintf(b, "%s  *) command %s \"$@\" ;;\n", pad, quoteAll(prefix))
	b.WriteString(pad + "esac\n")
}

func appendToks(prefix []string, toks ...string) []string {
	out := make([]string, 0, len(prefix)+len(toks))
	out = append(out, prefix...)
	out = append(out, toks...)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var shellEscaper = strings.NewReplacer(
	`\`, `\\`,
	`"`, `\"`,
	"$", `\$`,
	"`", "\\`",
)

// quoteAll 把 token 列表拼成 shell 安全的形式：每个 token 双引号包裹并转义。
func quoteAll(toks []string) string {
	var sb strings.Builder
	for i, t := range toks {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteByte('"')
		sb.WriteString(shellEscaper.Replace(t))
		sb.WriteByte('"')
	}
	return sb.String()
}
