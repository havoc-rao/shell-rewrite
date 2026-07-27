// Package core 提供 shr 的规则树（trie）、匹配器、TOML 持久化与 shell 代码生成。
package core

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Node 是规则树的一层：Rules 为叶子缩写映射（缩写 → 展开值），Children 为子命名空间。
type Node struct {
	Rules    map[string]string
	Children map[string]*Node
}

// NewNode 创建空节点。
func NewNode() *Node {
	return &Node{
		Rules:    map[string]string{},
		Children: map[string]*Node{},
	}
}

func (n *Node) empty() bool {
	return len(n.Rules) == 0 && len(n.Children) == 0
}

// Config 持有全部根命令（argv[0]）的规则树。
type Config struct {
	Roots map[string]*Node
}

// NewConfig 创建空配置。
func NewConfig() *Config {
	return &Config{Roots: map[string]*Node{}}
}

var (
	// 缩写/命名空间片段：出现在 shell case pattern 中，限制字符集保证生成代码安全
	nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	// 根命令名：同时是 shell 函数名
	cmdRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
)

// Add 注册一条缩写规则。
//
//	cmd:       根命令（argv[0]）
//	path:      缩写路径，最后一个元素是要定义的缩写，前面的元素是中间命名空间
//	expansion: 展开值（可含参数，如 "log --oneline"）
//
// 例：Add("colink", []string{"data", "u"}, "upload")  ⇒  colink data u ⇒ colink data upload
//
// 返回 overwritten 表示覆盖了同名规则。
func (c *Config) Add(cmd string, path []string, expansion string) (overwritten bool, err error) {
	if !cmdRe.MatchString(cmd) {
		return false, fmt.Errorf("非法命令名 %q（需可作为 shell 函数名）", cmd)
	}
	if len(path) == 0 {
		return false, fmt.Errorf("缺少缩写路径")
	}
	for _, p := range path {
		if !nameRe.MatchString(p) {
			return false, fmt.Errorf("非法路径片段 %q（仅允许字母、数字和 . _ -）", p)
		}
	}
	expansion = strings.Join(strings.Fields(expansion), " ")
	if expansion == "" {
		return false, fmt.Errorf("展开值不能为空")
	}

	root := c.Roots[cmd]
	if root == nil {
		root = NewNode()
		c.Roots[cmd] = root
	}

	// 中间片段：逐层下钻命名空间，必要时创建
	node := root
	for _, p := range path[:len(path)-1] {
		if exp, ok := node.Rules[p]; ok {
			return false, fmt.Errorf("路径片段 %q 已被用作缩写（→ %q），与命名空间冲突", p, exp)
		}
		child := node.Children[p]
		if child == nil {
			child = NewNode()
			node.Children[p] = child
		}
		node = child
	}

	last := path[len(path)-1]
	if _, ok := node.Children[last]; ok {
		return false, fmt.Errorf("%q 已是命名空间，不能再定义为缩写（可先 shr remove 删除其下规则）", last)
	}
	_, overwritten = node.Rules[last]
	node.Rules[last] = expansion
	return overwritten, nil
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
	for abbr, exp := range n.Rules {
		if !nameRe.MatchString(abbr) {
			out = append(out, fmt.Sprintf("%s: 缩写 %q 含非法字符", at, abbr))
		}
		if _, ok := n.Children[abbr]; ok {
			out = append(out, fmt.Sprintf("%s: %q 既是缩写（→ %q）又是命名空间，行为有歧义", at, abbr, exp))
		}
		toks := strings.Fields(exp)
		if len(toks) > 1 {
			if _, ok := n.Children[toks[0]]; ok {
				out = append(out, fmt.Sprintf("%s: 缩写 %q 的展开值 %q 带参数，其首词 %q 又有子命名空间，下钻不会发生", at, abbr, exp, toks[0]))
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
