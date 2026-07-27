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

# 缩写命中时回显展开后的命令：暗色输出到 stderr，仅交互式终端；
# SHR_ECHO=0 可关闭
_shr_echo() {
  [ "${SHR_ECHO:-1}" = "1" ] && [ -t 2 ] || return 0
  printf '\033[2m↪ %s\033[0m\n' "$*" >&2
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
		word := toks[0]
		child, drill := node.Children[word]
		drill = drill && len(toks) == 1

		// 计算 case pattern 列表
		var pats []string
		if strings.HasSuffix(abbr, "+") {
			// 前缀规则："b+" → branch 展开为 b|br|bra|bran|branc；
			// 让位顺序与 matchPrefix 一致：精确规则 > 更长 base 的前缀规则
			base := strings.TrimSuffix(abbr, "+")
			for _, p := range Prefixes(base, word) {
				if _, exact := node.Rules[p]; exact {
					continue
				}
				if shadowedByLongerPrefix(node, abbr, base, p) {
					continue
				}
				pats = append(pats, p)
			}
		} else {
			pats = []string{abbr}
		}
		if drill {
			pats = append(pats, word) // 下钻需要原词入口
		}
		if len(pats) == 0 {
			continue // 前缀被精确规则全覆盖
		}

		fmt.Fprintf(b, "%s  %s)\n", pad, strings.Join(pats, "|"))
		b.WriteString(pad + "    shift\n")
		if drill {
			genCase(b, child, appendToks(prefix, word), ind+2)
			merged[word] = true
		} else {
			fmt.Fprintf(b, "%s    _shr_echo %s \"$@\"\n", pad, quoteAll(appendToks(prefix, toks...)))
			fmt.Fprintf(b, "%s    command %s \"$@\"\n", pad, quoteAll(appendToks(prefix, toks...)))
		}
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

// shadowedByLongerPrefix 判断前缀 p 是否会被另一条 base 更长的前缀规则捕获
//（与 matchPrefix 的"最长 base 优先"语义保持一致，避免 case 顺序匹配产生歧义）。
func shadowedByLongerPrefix(node *Node, self, base, p string) bool {
	for key, target := range node.Rules {
		if key == self || !strings.HasSuffix(key, "+") {
			continue
		}
		base2 := strings.TrimSuffix(key, "+")
		word2 := strings.Fields(target)[0]
		if len(base2) > len(base) && len(p) >= len(base2) &&
			len(p) < len(word2) && strings.HasPrefix(word2, p) {
			return true
		}
	}
	return false
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
