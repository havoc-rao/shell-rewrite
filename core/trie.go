// Package core 提供 shr 的规则树（trie）、匹配器、TOML 持久化与 shell 代码生成。
package core

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Node 是规则树的一层：Rules 为叶子缩写映射（缩写 → 一个或多个展开值），
// Children 为子命名空间。一个缩写可注册多个展开值，运行时命中多值时由
// wrapper 回调 `shr _pick` 弹出 TUI 让用户选择（见 shellgen）。
type Node struct {
	Rules    map[string][]string
	Children map[string]*Node
}

// NewNode 创建空节点。
func NewNode() *Node {
	return &Node{
		Rules:    map[string][]string{},
		Children: map[string]*Node{},
	}
}

func (n *Node) empty() bool {
	return len(n.Rules) == 0 && len(n.Children) == 0
}

// Config 持有全部根命令（argv[0]）的规则树。
//
// Enabled 控制是否实际复写：为 false 时生成的 wrapper 退化为透传
// （command <cmd> "$@"），规则本身保留，可随时 shr on 恢复。
//
// AllowDuplicates 控制是否允许同一缩写注册多个展开值：为 true（默认）时
// `add` 追加候选、运行时命中多值弹 TUI 选择；为 false 时 `add` 命中已存在
// 即报错，避免静默覆盖。
type Config struct {
	Roots           map[string]*Node
	Enabled         bool
	AllowDuplicates bool
}

// NewConfig 创建空配置（默认开启复写、允许重复）。
func NewConfig() *Config {
	return &Config{Roots: map[string]*Node{}, Enabled: true, AllowDuplicates: true}
}

var (
	// 缩写/命名空间片段：出现在 shell case pattern 中，限制字符集保证生成代码安全
	nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	// 根命令名：同时是 shell 函数名
	cmdRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
)

// AddStatus 描述一次 Add 操作对规则树的影响。
type AddStatus int

const (
	StatusAdded    AddStatus = iota // 新建了缩写键
	StatusAppended                  // 缩写键已存在，追加了新候选
	StatusExists                    // 该候选已存在，幂等无变化
)

// Add 注册一条缩写规则。
//
//	cmd:       根命令（argv[0]）
//	path:      缩写路径，最后一个元素是要定义的缩写，前面的元素是中间命名空间
//	expansion: 展开值（可含参数，如 "log --oneline"）
//
// 例：Add("colink", []string{"data", "u"}, "upload")  ⇒  colink data u ⇒ colink data upload
//
// 当 AllowDuplicates 为 true 时，对已存在的缩写键追加新候选（去重）；
// 为 false 时，命中已存在键即返回错误，不再静默覆盖。
// 前缀规则（末位以 + 结尾）的每个候选都必须满足首词以 base 开头且更长。
func (c *Config) Add(cmd string, path []string, expansion string) (AddStatus, error) {
	if cmd == metaKey {
		return StatusExists, fmt.Errorf("%q 是保留命令名", cmd)
	}
	if !cmdRe.MatchString(cmd) {
		return StatusExists, fmt.Errorf("非法命令名 %q（需可作为 shell 函数名）", cmd)
	}
	if len(path) == 0 {
		return StatusExists, fmt.Errorf("缺少缩写路径")
	}
	for i, p := range path {
		name := p
		if i == len(path)-1 {
			name = strings.TrimSuffix(p, "+") // 末位允许 + 后缀表示前缀模式
		}
		if !nameRe.MatchString(name) {
			return StatusExists, fmt.Errorf("非法路径片段 %q（仅允许字母、数字和 . _ -）", p)
		}
	}
	expansion = strings.Join(strings.Fields(expansion), " ")
	if expansion == "" {
		return StatusExists, fmt.Errorf("展开值不能为空")
	}

	root := c.Roots[cmd]
	if root == nil {
		root = NewNode()
		c.Roots[cmd] = root
	}

	// 中间片段：逐层下钻命名空间，必要时创建
	node := root
	for _, p := range path[:len(path)-1] {
		if exps, ok := node.Rules[p]; ok {
			return StatusExists, fmt.Errorf("路径片段 %q 已被用作缩写（→ %q），与命名空间冲突", p, strings.Join(exps, " | "))
		}
		child := node.Children[p]
		if child == nil {
			child = NewNode()
			node.Children[p] = child
		}
		node = child
	}

	last := path[len(path)-1]
	if strings.HasSuffix(last, "+") {
		// 前缀模式："b+" = "branch" 表示 b、br、bra、bran、branc 均展开为 branch
		base := strings.TrimSuffix(last, "+")
		word := strings.Fields(expansion)[0]
		if base == "" || len(word) <= len(base) || !strings.HasPrefix(word, base) {
			return StatusExists, fmt.Errorf("前缀规则 %q 要求展开值首词 %q 以 %q 开头且更长", last, word, base)
		}
	}
	base := strings.TrimSuffix(last, "+")
	if _, ok := node.Children[base]; ok {
		return StatusExists, fmt.Errorf("%q 已是命名空间，不能再定义为缩写（可先 shr remove 删除其下规则）", base)
	}

	existing := node.Rules[last]
	if len(existing) == 0 {
		node.Rules[last] = []string{expansion}
		return StatusAdded, nil
	}
	if !c.AllowDuplicates {
		return StatusExists, fmt.Errorf("%q 已存在（→ %s）；当前为「不允许重复」模式，如需覆盖请先 shr remove，或开启: shr dup on", last, strings.Join(existing, " | "))
	}
	for _, e := range existing {
		if e == expansion {
			return StatusExists, nil // 该候选已存在，幂等无变化
		}
	}
	node.Rules[last] = append(existing, expansion)
	return StatusAppended, nil
}

// Prefixes 返回 word 从 len(base) 开始的逐字符前缀（不含 word 本身）。
// 用于前缀规则（"b+" → branch）展开为 case pattern 与展示。
func Prefixes(base, word string) []string {
	var out []string
	for l := len(base); l < len(word); l++ {
		out = append(out, word[:l])
	}
	return out
}

// Remove 删除一条规则或整个命名空间，并回收空节点。返回是否删除成功。
func (c *Config) Remove(cmd string, path []string) bool {
	root := c.Roots[cmd]
	if root == nil || len(path) == 0 {
		return false
	}
	var rec func(n *Node, segs []string) bool
	rec = func(n *Node, segs []string) bool {
		if len(segs) == 1 {
			if _, ok := n.Rules[segs[0]]; ok {
				delete(n.Rules, segs[0])
				return true
			}
			if _, ok := n.Children[segs[0]]; ok {
				delete(n.Children, segs[0])
				return true
			}
			return false
		}
		child := n.Children[segs[0]]
		if child == nil {
			return false
		}
		ok := rec(child, segs[1:])
		if child.empty() {
			delete(n.Children, segs[0])
		}
		return ok
	}
	removed := rec(root, path)
	if root.empty() {
		delete(c.Roots, cmd)
	}
	return removed
}

// SortedRoots 返回排序后的根命令名列表。
func (c *Config) SortedRoots() []string {
	names := make([]string, 0, len(c.Roots))
	for k := range c.Roots {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Doctor 校验规则树，返回问题列表（主要防御手工编辑 TOML 造成的冲突）。
func (c *Config) Doctor() []string {
	var issues []string
	for _, cmd := range c.SortedRoots() {
		issues = append(issues, checkNode(c.Roots[cmd], []string{cmd})...)
	}
	return issues
}

func checkNode(n *Node, prefix []string) []string {
	var out []string
	at := strings.Join(prefix, " ")
	for abbr, exps := range n.Rules {
		isPrefix := strings.HasSuffix(abbr, "+")
		base := strings.TrimSuffix(abbr, "+")
		if !nameRe.MatchString(base) {
			out = append(out, fmt.Sprintf("%s: 缩写 %q 含非法字符", at, abbr))
		}
		if _, ok := n.Children[base]; ok {
			out = append(out, fmt.Sprintf("%s: %q 既是缩写（→ %q）又是命名空间，行为有歧义", at, abbr, strings.Join(exps, " | ")))
		}
		for _, exp := range exps {
			toks := strings.Fields(exp)
			if isPrefix {
				if len(toks) == 0 || len(toks[0]) <= len(base) || !strings.HasPrefix(toks[0], base) {
					out = append(out, fmt.Sprintf("%s: 前缀规则 %q 的目标 %q 不以 %q 开头或不够长", at, abbr, exp, base))
				}
			} else if len(toks) > 1 {
				if _, ok := n.Children[toks[0]]; ok {
					out = append(out, fmt.Sprintf("%s: 缩写 %q 的展开值 %q 带参数，其首词 %q 又有子命名空间，下钻不会发生", at, abbr, exp, toks[0]))
				}
			}
		}
		if len(exps) > 1 {
			// 多值缩写作为终点：选完直接执行，不下钻。若某候选首词恰有子表，提示用户。
			for _, exp := range exps {
				toks := strings.Fields(exp)
				if len(toks) == 1 {
					if _, ok := n.Children[toks[0]]; ok {
						out = append(out, fmt.Sprintf("%s: 多值缩写 %q 的候选 %q 是命名空间，但多值分支不下钻（选完即执行）", at, abbr, exp))
						break
					}
				}
			}
		}
	}
	for name, child := range n.Children {
		if !nameRe.MatchString(name) {
			out = append(out, fmt.Sprintf("%s: 命名空间 %q 含非法字符", at, name))
		}
		out = append(out, checkNode(child, append(prefix, name))...)
	}
	return out
}
