package core

import "strings"

// Expand 对 argv 做逐 token 下钻展开，返回改写后的命令行。
//
// 三条边界规则：
//  1. 替换值不二次展开（天然防环）；
//  2. 失配即整体透传——某 token 既不命中叶子规则也无同名子表时，
//     从它开始剩余全部原样保留（flag 及其值因此安全）；
//  3. 下钻用展开后的 token 名（d→data 后可继续匹配 [x.data] 下的规则）。
func (c *Config) Expand(argv []string) []string {
	if len(argv) == 0 {
		return argv
	}
	node := c.Roots[argv[0]]
	if node == nil {
		return argv // 未管理的命令，原样返回
	}
	out := []string{argv[0]}
	i := 1
	for i < len(argv) {
		t := argv[i]
		// 优先级：精确规则 > 命名空间下钻 > 前缀规则 > 透传
		exp, ok := node.Rules[t]
		if !ok {
			if child, ok2 := node.Children[t]; ok2 {
				out = append(out, t)
				node = child
				i++
				continue
			}
			exp, ok = matchPrefix(node, t)
		}
		if !ok {
			break // 失配，剩余透传
		}
		toks := strings.Fields(exp)
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
	return append(out, argv[i:]...)
}

// matchPrefix 查找前缀规则：key 形如 "b+"，当 t 是目标词的前缀、
// 长度不短于 base、且不等于目标词时命中。多个命中时取 base 最长（最具体）者。
func matchPrefix(n *Node, t string) (string, bool) {
	best := ""
	bestLen := -1
	for key, target := range n.Rules {
		if !strings.HasSuffix(key, "+") {
			continue
		}
		base := key[:len(key)-1]
		if len(t) < len(base) {
			continue
		}
		word := strings.Fields(target)[0]
		if len(t) < len(word) && strings.HasPrefix(word, t) && len(base) > bestLen {
			best, bestLen = target, len(base)
		}
	}
	return best, bestLen >= 0
}
