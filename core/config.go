package core

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// metaKey 是 rules.toml 中保留的元信息表名，不作为命令根解析。
// 其中 enabled 控制复写开关（shr on / shr off），
// allow_duplicates 控制是否允许同一缩写注册多个展开值（shr dup on / off）。
const metaKey = "__shr"

// Path 返回规则文件路径：$XDG_CONFIG_HOME/shr/rules.toml 或 ~/.config/shr/rules.toml。
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "shr", "rules.toml")
}

// Load 从默认路径加载配置；文件不存在时返回空配置。
func Load() (*Config, error) {
	return LoadFrom(Path())
}

// LoadFrom 从指定路径加载配置。
//
// TOML 结构即嵌套规则树，同一层中字符串值是叶子规则、子表是命名空间；
// 一个缩写可有多个展开值，用数组表示（单值仍可写成字符串，向后兼容）：
//
//	[colink]
//	d  = "data"        # 缩写 → 命名空间名（下钻）
//	st = "status"      # 普通叶子
//	[colink.data]
//	u = "upload"
//	p = ["pull", "push"]   # 多值：运行时弹 TUI 选择
func LoadFrom(p string) (*Config, error) {
	cfg := NewConfig()
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	var raw map[string]interface{}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", p, err)
	}
	for cmd, v := range raw {
		if cmd == metaKey {
			if m, ok := v.(map[string]interface{}); ok {
				if e, ok := m["enabled"].(bool); ok {
					cfg.Enabled = e
				}
				if d, ok := m["allow_duplicates"].(bool); ok {
					cfg.AllowDuplicates = d
				}
				if a, ok := m["aliases"].(map[string]interface{}); ok {
					for k, val := range a {
						switch t := val.(type) {
						case string:
							cfg.Aliases[k] = []string{t}
						case []interface{}:
							var arr []string
							for _, e := range t {
								if s, ok := e.(string); ok {
									arr = append(arr, s)
								}
							}
							if len(arr) > 0 {
								cfg.Aliases[k] = arr
							}
						}
					}
				}
			}
			continue
		}
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("根命令 %q 必须是表", cmd)
		}
		node, err := nodeFromMap(m)
		if err != nil {
			return nil, fmt.Errorf("[%s] %w", cmd, err)
		}
		cfg.Roots[cmd] = node
	}
	return cfg, nil
}

func nodeFromMap(m map[string]interface{}) (*Node, error) {
	n := NewNode()
	for k, v := range m {
		switch t := v.(type) {
		case string:
			n.Rules[k] = []string{t}
		case []interface{}:
			var arr []string
			for _, e := range t {
				s, ok := e.(string)
				if !ok {
					return nil, fmt.Errorf("%q 的数组元素必须是字符串", k)
				}
				arr = append(arr, s)
			}
			if len(arr) > 0 {
				n.Rules[k] = arr
			}
		case map[string]interface{}:
			child, err := nodeFromMap(t)
			if err != nil {
				return nil, err
			}
			n.Children[k] = child
		default:
			return nil, fmt.Errorf("%q 的值类型不支持（应为字符串、数组或表）", k)
		}
	}
	return n, nil
}

// Save 写回默认路径（必要时创建目录）。
func (c *Config) Save() error {
	return c.SaveTo(Path())
}

// SaveTo 写回指定路径。
func (c *Config) SaveTo(p string) error {
	var buf bytes.Buffer
	buf.WriteString("# shr rules — managed by `shr add/remove`, editable by hand (then run `shr doctor`)\n")
	if err := toml.NewEncoder(&buf).Encode(c.toMap()); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, buf.Bytes(), 0o644)
}

func (c *Config) toMap() map[string]interface{} {
	meta := map[string]interface{}{
		"enabled":          c.Enabled,
		"allow_duplicates": c.AllowDuplicates,
	}
	if len(c.Aliases) > 0 {
		aliases := map[string]interface{}{}
		for k, v := range c.Aliases {
			if len(v) == 1 {
				aliases[k] = v[0] // 单值写字符串，与手编/老配置一致
			} else {
				aliases[k] = v
			}
		}
		meta["aliases"] = aliases
	}
	out := map[string]interface{}{
		metaKey: meta,
	}
	for cmd, n := range c.Roots {
		out[cmd] = nodeToMap(n)
	}
	return out
}

func nodeToMap(n *Node) map[string]interface{} {
	m := map[string]interface{}{}
	for k, v := range n.Rules {
		if len(v) == 1 {
			m[k] = v[0] // 单值写字符串，与手编/老配置一致
		} else {
			m[k] = v // 多值写数组
		}
	}
	for k, child := range n.Children {
		m[k] = nodeToMap(child)
	}
	return m
}
