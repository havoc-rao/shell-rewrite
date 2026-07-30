package core

import "strings"

// Expand 对 argv 做逐 token 下钻展开，返回改写后的命令行。
//
// 先查一级命令名别名（argv[0] → target）：命中则替换命令名，若 target 有
// 规则树则继续下钻其子命令规则。之后走三条边界规则：
//  1. 替换值不二次展开（天然防环）；
//  2. 失配即整体透传——某 token 既不命中叶子规则也无同名子表时，
//     从它开始剩余全部原样保留（flag 及其值因此安全）；
//  3. 下钻用展开后的 token 名（d→data 后可继续匹配 [x.data] 下的规则）。
//
// 当某缩写注册了多个展开值时，这里取首个候选仅用于 `shr expand` 预览；
// 真正执行时由 shell wrapper 回调 `shr _pick` 弹 TUI 让用户选择（见 Ambiguities）。
func (c *Config) Expand(argv []string) []string {
	if len(argv) == 0 {
		return argv
	}
	// 一级命令名别名：替换 argv[0] 后继续下钻目标规则树
	if target, ok := c.matchAlias(argv[0]); ok {
		node := c.Roots[target]
		if node == nil {
			return append([]string{target}, argv[1:]...)
		}
		return append([]string{target}, c.expandAt(node, argv[1:])...)
	}
	node := c.Roots[argv[0]]
	if node == nil {
		return argv // 未管理的命令，原样返回
	}
	return append([]string{argv[0]}, c.expandAt(node, argv[1:])...)
}

// expandAt 从 node 开始对 rest（argv[1:]）做逐 token 下钻展开，返回展开后的
// 参数部分（不含命令名）。三条边界规则在此成立（见 Expand 注释）。
func (c *Config) expandAt(node *Node, rest []string) []string {
	var out []string
	i := 0
	for i < len(rest) {
		t := rest[i]
		// 优先级：精确规则 > 命名空间下钻 > 前缀规则 > 透传
		exps, ok := node.Rules[t]
		if !ok {
			if child, ok2 := node.Children[t]; ok2 {
				out = append(out, t)
				node = child
				i++
				continue
			}
			if pexp, ok2 := matchPrefix(node, t); ok2 {
				exps = []string{pexp}
				ok = true
			}
		}
		if !ok {
			break // 失配，剩余透传
		}
		toks := strings.Fields(exps[0])
		out = append(out, toks...)
		i++
		// 单 token 展开值且存在同名子表 → 下钻继续匹配
		if len(toks) == 1 {
			if child, ok2 := node.Children[toks[0]]; ok2 {
				node = child
				continue
			}
		}
		break // 普通叶子或多 token 展开是终点
	}
	return append(out, rest[i:]...)
}

// matchAlias 查找一级命令名缩写：精确别名优先，再查前缀规则（最长 base 优先）。
// 返回首个候选目标（多值别名的运行时选择仍由 wrapper 弹 TUI）。
func (c *Config) matchAlias(name string) (string, bool) {
	if targets, ok := c.Aliases[name]; ok && len(targets) > 0 {
		return targets[0], true
	}
	best := ""
	bestLen := -1
	for key, targets := range c.Aliases {
		if !strings.HasSuffix(key, "+") || len(targets) == 0 {
			continue
		}
		base := key[:len(key)-1]
		if len(name) < len(base) {
			continue
		}
		word := strings.Fields(targets[0])[0]
		if len(name) < len(word) && strings.HasPrefix(word, name) && len(base) > bestLen {
			best, bestLen = targets[0], len(base)
		}
	}
	return best, bestLen >= 0
}

// Ambiguity 描述命令行中一个命中了多个展开值的缩写点。
type Ambiguity struct {
	At     string   // 命中点的完整路径，如 "git p"
	Values []string // 候选展开值列表
}

// Ambiguities 遍历 argv 路径上命中多个候选的精确缩写，供 `shr expand` 标注
// "运行时将弹出选择"。前缀规则的多值不在此报告（极少见，且 Expand 已取首个）。
// 一级命令名别名命中时，替换命令名后在目标规则树中继续报告歧义。
func (c *Config) Ambiguities(argv []string) []Ambiguity {
	var out []Ambiguity
	if len(argv) == 0 {
		return out
	}
	cmd := argv[0]
	prefix := []string{cmd}
	// 一级命令名别名：替换命令名后在目标规则树中报告歧义
	if target, ok := c.matchAlias(cmd); ok {
		cmd = target
		prefix = []string{target}
	}
	node := c.Roots[cmd]
	if node == nil {
		return out
	}
	rest := argv[1:]
	i := 0
	for i < len(rest) {
		t := rest[i]
		exps, ok := node.Rules[t]
		if !ok {
			if child, ok2 := node.Children[t]; ok2 {
				node = child
				prefix = append(prefix, t)
				i++
				continue
			}
			break // 失配（含前缀命中，不深入报告）
		}
		if len(exps) > 1 {
			out = append(out, Ambiguity{At: strings.Join(append(append([]string{}, prefix...), t), " "), Values: exps})
		}
		toks := strings.Fields(exps[0])
		if len(toks) == 1 {
			if child, ok2 := node.Children[toks[0]]; ok2 {
				node = child
				prefix = append(prefix, toks[0])
				i++
				continue
			}
		}
		break
	}
	return out
}

// matchPrefix 查找前缀规则：key 形如 "b+"，当 t 是目标词的前缀、
// 长度不短于 base、且不等于目标词时命中。多个命中时取 base 最长（最具体）者。
// 返回该规则的首个候选展开值（多值前缀规则的运行时选择仍由 wrapper 弹 TUI）。
func matchPrefix(n *Node, t string) (string, bool) {
	best := ""
	bestLen := -1
	for key, targets := range n.Rules {
		if !strings.HasSuffix(key, "+") {
			continue
		}
		if len(targets) == 0 {
			continue
		}
		base := key[:len(key)-1]
		if len(t) < len(base) {
			continue
		}
		word := strings.Fields(targets[0])[0]
		if len(t) < len(word) && strings.HasPrefix(word, t) && len(base) > bestLen {
			best, bestLen = targets[0], len(base)
		}
	}
	return best, bestLen >= 0
}
