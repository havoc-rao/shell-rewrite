package core

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

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
// TOML 结构即嵌套规则树，同一层中字符串值是叶子规则、子表是命名空间：
//
//	[colink]
//	d  = "data"        # 缩写 → 命名空间名（下钻）
//	st = "status"      # 普通叶子
//	[colink.data]
//	u = "upload"
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
			n.Rules[k] = t
		case map[string]interface{}:
			child, err := nodeFromMap(t)
			if err != nil {
				return nil, err
			}
			n.Children[k] = child
		default:
			return nil, fmt.Errorf("%q 的值类型不支持（应为字符串或表）", k)
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
	out := map[string]interface{}{}
	for cmd, n := range c.Roots {
		out[cmd] = nodeToMap(n)
	}
	return out
}

func nodeToMap(n *Node) map[string]interface{} {
	m := map[string]interface{}{}
	for k, v := range n.Rules {
		m[k] = v
	}
	for k, child := range n.Children {
		m[k] = nodeToMap(child)
	}
	return m
}
