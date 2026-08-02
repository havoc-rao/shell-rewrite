package core

import (
	"fmt"
	"sort"
	"strings"
)

// GenInit 输出 shell 集成代码（wrapper 函数 + 热加载 hook），供 eval "$(shr init zsh)" 使用。
// userPath 是用户级规则路径，projPath 是当前目录附近的项目级规则路径（无则空串，
// 仅用于注释说明；shell 侧在运行时自行向上查找）。
func (c *Config) GenInit(shell, userPath, projPath string) (string, error) {
	var b strings.Builder
	switch shell {
	case "zsh", "bash":
		fmt.Fprintf(&b, "# >>> shr init (%s) >>>\n", shell)
		fmt.Fprintf(&b, "# user rules: %s\n", userPath)
		if projPath != "" {
			fmt.Fprintf(&b, "# project rules: %s (仅在本项目内生效)\n", projPath)
		}
		b.WriteString("# note: existing aliases take precedence over functions — unalias if needed.\n\n")
		b.WriteString(posixPrelude)
		b.WriteString(c.genPosixFuncs(projPath))
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

// posixPickFunc 是多值缩写的选择器 helper。它随 _gen posix 一起输出，确保
// 热加载（只 eval _gen posix，不含 prelude）也能定义它——否则老会话升级后
// 多值分支会引用未定义的 _shr_pick（command not found）。
const posixPickFunc = `# 多值缩写命中时弹出 TUI 选择器：shr _pick 把 TUI 渲染到 /dev/tty，
# 选中值写到 stdout 供本函数捕获；取消时返回非零，wrapper 据 return 中止执行。
# SHR_PICK=off 直接取首个候选（脚本确定性执行，不弹 TUI）。
_shr_pick() {
  if [ "${SHR_PICK:-on}" = "off" ]; then
    shift; printf '%s\n' "$1"; return 0
  fi
  shr _pick "$@"
}

`

const posixPrelude = `_SHR_CONFIG="${XDG_CONFIG_HOME:-$HOME/.config}/shr/rules.toml"
# 当前目录的项目规则文件路径：由 shr 按当前目录计算（注册表/默认探测），
# 每次热加载（_gen posix）重写；空串 = 无项目规则。
_SHR_PROJECT_PATH=''

# 热加载标记：用户配置 mtime | 当前目录 | 注册表 mtime | [项目规则路径|mtime]。
# 含 $PWD 使 cd 进出项目触发重载；注册表 mtime 使项目位置变更立即生效；
# 项目规则文件 mtime 使 add/remove 在下一个提示符前生效。
_shr_marker() {
  local m pc
  m=$(stat -f %m "$_SHR_CONFIG" 2>/dev/null || stat -c %Y "$_SHR_CONFIG" 2>/dev/null || echo 0)
  m="$m|$PWD|$(stat -f %m "$HOME/.shr/projects.toml" 2>/dev/null || stat -c %Y "$HOME/.shr/projects.toml" 2>/dev/null || echo 0)"
  pc="${_SHR_PROJECT_PATH:-}"
  if [ -n "$pc" ]; then
    m="$m|$pc|$(stat -f %m "$pc" 2>/dev/null || stat -c %Y "$pc" 2>/dev/null || echo 0)"
  fi
  printf '%s\n' "$m"
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
  m=$(_shr_marker) || return 0
  if [ "$m" != "$_SHR_LOADED_MTIME" ]; then
    _SHR_LOADED_MTIME="$m"
    eval "$(shr _gen posix)"
  fi
}
_SHR_LOADED_MTIME=$(_shr_marker)

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
// Enabled 为 false 时退化为透传函数（command <cmd> "$@"），保留函数定义以便
// shr on 后热加载无缝恢复。一级命令名别名始终保留命令名替换（否则缩写名
// 会变成找不到的命令），仅在 Enabled 时下钻目标规则树。
func (c *Config) GenPosixFuncs() string {
	return c.genPosixFuncs("")
}

// GenPosixFuncsFor 与 GenPosixFuncs 相同，但显式指定当前目录的项目规则文件路径
// （projPath，可为空），供 _gen posix 使用，输出 _SHR_PROJECT_PATH 供热加载标记。
func (c *Config) GenPosixFuncsFor(projPath string) string {
	return c.genPosixFuncs(projPath)
}

func (c *Config) genPosixFuncs(projPath string) string {
	var b strings.Builder
	if projPath != "" {
		fmt.Fprintf(&b, "_SHR_PROJECT_PATH=%s\n", shSingleQuote(projPath))
	} else {
		b.WriteString("_SHR_PROJECT_PATH=''\n")
	}
	// _shr_pick 随热加载一起重新定义：老会话的 prelude 可能是旧版（无此函数），
	// 仅靠 init 注入会让多值分支引用未定义函数；放在此处保证 _gen posix 自洽。
	b.WriteString(posixPickFunc)
	for _, cmd := range c.SortedRoots() {
		if c.Enabled {
			b.WriteString(genFunc(cmd, c.Roots[cmd]))
		} else {
			b.WriteString(genPassthrough(cmd))
		}
		b.WriteString("\n")
	}
	b.WriteString(c.genAliasFuncs())
	return b.String()
}

// shSingleQuote 用单引号包裹并转义，生成 shell 安全的字面量。
func shSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// genAliasFuncs 生成一级命令名缩写的 wrapper 函数。
//
//	目标有规则树（root）+ Enabled → 函数调用下钻（target "$@"），
//	  由目标规则树负责回显与子命令展开；
//	目标无规则树 + Enabled        → command 直接执行，命中时回显展开后的命令；
//	Enabled 为 false（shr off）   → command <target> "$@"，保留命令名替换、关闭下钻。
//
// 前缀模式（"c+"）为每个未被更具体规则覆盖的前缀各生成一个同名函数。
func (c *Config) genAliasFuncs() string {
	var b strings.Builder
	for _, abbr := range c.SortedAliases() {
		targets := c.Aliases[abbr]
		if len(targets) == 0 {
			continue
		}
		toks := strings.Fields(targets[0])
		word := toks[0]
		_, hasRoot := c.Roots[word]
		drill := c.Enabled && hasRoot && len(toks) == 1
		var body string
		switch {
		case drill:
			body = fmt.Sprintf("  %s \"$@\"\n", word)
		case c.Enabled:
			body = fmt.Sprintf("  _shr_echo %s \"$@\"\n  command %s \"$@\"\n", quoteAll(toks), quoteAll(toks))
		default:
			body = fmt.Sprintf("  command %s \"$@\"\n", quoteAll(toks))
		}
		for _, name := range c.aliasFuncNames(abbr) {
			fmt.Fprintf(&b, "%s() {\n%s}\n", name, body)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// genPassthrough 生成透传函数：复写关闭时仍覆盖同名 wrapper，确保命令直通。
func genPassthrough(cmd string) string {
	return fmt.Sprintf("%s() { command %s \"$@\"; }\n", cmd, cmd)
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
//	缩写 → 单值无子表：abbr) shift; command <prefix> <expansion> "$@" ;;
//	缩写 → 单值子表名：abbr|name) shift; <递归 case> ;;   （下钻）
//	缩写 → 多值：      abbr) shift; _shr_pick 弹选后 command <prefix> <picked> "$@" ;;
//	纯命名空间：        name) shift; <递归 case> ;;
//	失配：              *) command <prefix> "$@" ;;   （整体透传）
func genCase(b *strings.Builder, node *Node, prefix []string, ind int) {
	pad := strings.Repeat("  ", ind)
	b.WriteString(pad + `case "$1" in` + "\n")

	merged := map[string]bool{} // 已被缩写分支覆盖的子表名

	for _, abbr := range sortedKeys(node.Rules) {
		values := node.Rules[abbr]
		if len(values) == 0 {
			continue
		}
		multi := len(values) > 1
		toks0 := strings.Fields(values[0])
		word := toks0[0]
		child, drill := node.Children[word]
		drill = drill && !multi && len(toks0) == 1

		// 计算 case pattern 列表
		var pats []string
		if strings.HasSuffix(abbr, "+") {
			base := strings.TrimSuffix(abbr, "+")
			if multi {
				// 多值前缀规则（p+ = ["pull", "push"]）：共享前缀（p、pu）是
				// 歧义的，显式生成并弹 TUI；唯一前缀（pul、pus）交由失配推断
				// 分支补全（与 matchPrefix 的 prefixRuleMatch 语义一致）
				var cands []string
				for _, v := range values {
					cands = append(cands, strings.Fields(v)[0])
				}
				for _, p := range sharedPrefixes(base, cands) {
					if _, exact := node.Rules[p]; exact {
						continue
					}
					pats = append(pats, p)
				}
			} else {
				// 单值前缀规则："b+" → branch 展开为 b|br|bra|bran|branc；
				// 让位顺序与 matchPrefix 一致：精确规则 > 更长 base 的前缀规则
				for _, p := range Prefixes(base, word) {
					if _, exact := node.Rules[p]; exact {
						continue
					}
					if shadowedByLongerPrefix(node, abbr, base, p) {
						continue
					}
					pats = append(pats, p)
				}
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
		switch {
		case drill:
			genCase(b, child, appendToks(prefix, word), ind+2)
			merged[word] = true
		case multi:
			// 多值：运行时回调 _shr_pick 弹 TUI 选择，选中值按空格分词后执行
			label := strings.Join(append(append([]string{}, prefix...), trimPrefixMark(abbr)), " ")
			fmt.Fprintf(b, "%s    local _shr_picked\n", pad)
			fmt.Fprintf(b, "%s    _shr_picked=$(_shr_pick %s %s) || return $?\n", pad, quote(label), quoteAll(values))
			fmt.Fprintf(b, "%s    _shr_echo %s \"$_shr_picked\" \"$@\"\n", pad, quoteAll(prefix))
			fmt.Fprintf(b, "%s    command %s $_shr_picked \"$@\"\n", pad, quoteAll(prefix))
		default:
			fmt.Fprintf(b, "%s    _shr_echo %s \"$@\"\n", pad, quoteAll(appendToks(prefix, toks0...)))
			fmt.Fprintf(b, "%s    command %s \"$@\"\n", pad, quoteAll(appendToks(prefix, toks0...)))
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

	// 失配：若存在多值候选的唯一前缀推断，生成嵌套 case 做补全
	// （如 git p=push|pull 时，git pus → git push、git pul → git pull）。
	// 与 match.go 的 UniquePrefixWord 保持一致：仅严格前缀、唯一、未被规则占用。
	words := MultiCandidateWords(node)
	type inferBranch struct{ p, w string }
	var infer []inferBranch
	for _, w := range words {
		for _, p := range UniquePrefixesOf(node, words, w) {
			infer = append(infer, inferBranch{p, w})
		}
	}
	if len(infer) > 0 {
		b.WriteString(pad + `  *) case "$1" in` + "\n")
		for _, br := range infer {
			// 补全命中：shift 后原地补全，若补全词恰为子表名则下钻
			if child, ok := node.Children[br.w]; ok {
				fmt.Fprintf(b, "%s       %s) shift\n", pad, br.p)
				genCase(b, child, appendToks(prefix, br.w), ind+3)
				b.WriteString(pad + "       ;;\n")
			} else {
				// 回显展开后的完整命令（与其他缩写分支一致）
				fmt.Fprintf(b, "%s       %s) shift; _shr_echo %s \"$@\"; command %s \"$@\" ;;\n",
					pad, br.p, quoteAll(appendToks(prefix, br.w)), quoteAll(appendToks(prefix, br.w)))
			}
		}
		fmt.Fprintf(b, "%s       *) command %s \"$@\" ;;\n", pad, quoteAll(prefix))
		b.WriteString(pad + "    esac ;;\n")
	} else {
		fmt.Fprintf(b, "%s  *) command %s \"$@\" ;;\n", pad, quoteAll(prefix))
	}
	b.WriteString(pad + "esac\n")
}

// shadowedByLongerPrefix 判断前缀 p 是否会被另一条 base 更长的前缀规则捕获
// （与 matchPrefix 的"最长 base 优先"语义保持一致，避免 case 顺序匹配产生歧义）。
func shadowedByLongerPrefix(node *Node, self, base, p string) bool {
	for key, targets := range node.Rules {
		if key == self || !strings.HasSuffix(key, "+") {
			continue
		}
		if len(targets) == 0 {
			continue
		}
		base2 := strings.TrimSuffix(key, "+")
		word2 := strings.Fields(targets[0])[0]
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
		sb.WriteString(quote(t))
	}
	return sb.String()
}

// quote 把单个 token 双引号包裹并转义。
func quote(s string) string {
	return `"` + shellEscaper.Replace(s) + `"`
}

// trimPrefixMark 去掉前缀规则的 + 后缀，用于生成 TUI 选择器的可读标签。
func trimPrefixMark(abbr string) string {
	return strings.TrimSuffix(abbr, "+")
}
