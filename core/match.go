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
		if exp, ok := node.Rules[t]; ok {
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
		if child, ok := node.Children[t]; ok {
			out = append(out, t)
			node = child
			i++
			continue
		}
		break // 失配，剩余透传
	}
	return append(out, argv[i:]...)
}
