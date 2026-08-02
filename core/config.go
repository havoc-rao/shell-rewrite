package core

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// metaKey 是 rules.toml 中保留的元信息表名，不作为命令根解析。
// 其中 enabled 控制复写开关（shr on / shr off），
// allow_duplicates 控制是否允许同一缩写注册多个展开值（shr dup on / off），
// project_dir 自定义项目级配置的子目录名（默认 .shr）。
const metaKey = "__shr"

// DefaultProjectDir 是项目级配置的默认子目录（相对项目根）：<项目根>/.shr/rules.toml。
// 可用环境变量 SHR_PROJECT_DIR 或用户配置的 [__shr] project_dir 覆盖（如 .vscode/shr）。
const DefaultProjectDir = ".shr"

// Path 返回用户级规则文件路径：$XDG_CONFIG_HOME/shr/rules.toml 或 ~/.config/shr/rules.toml。
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "shr", "rules.toml")
}

// Load 从默认路径加载（用户级）配置；文件不存在时返回空配置。
func Load() (*Config, error) { return LoadFrom(Path()) }

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
	raw, err := loadRaw(p)
	if err != nil {
		return nil, err
	}
	return configFromRaw(raw)
}

func loadRaw(p string) (map[string]interface{}, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{}, nil
		}
		return nil, err
	}
	var raw map[string]interface{}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", p, err)
	}
	return raw, nil
}

func configFromRaw(raw map[string]interface{}) (*Config, error) {
	cfg := NewConfig()
	for cmd, v := range raw {
		if cmd == metaKey {
			if m, ok := v.(map[string]interface{}); ok {
				if e, ok := m["enabled"].(bool); ok {
					cfg.Enabled = e
					cfg.EnabledSet = true
				}
				if d, ok := m["allow_duplicates"].(bool); ok {
					cfg.AllowDuplicates = d
					cfg.AllowDuplicatesSet = true
				}
				if pd, ok := m["project_dir"].(string); ok && ValidProjectDir(pd) {
					cfg.ProjectDir = pd
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

// Save 写回用户级默认路径（必要时创建目录）。
func (c *Config) Save() error {
	return c.SaveTo(Path())
}

// SaveTo 写回指定路径（用户级模式：总是写入 enabled/allow_duplicates）。
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

// SaveProjectTo 以项目级模式写回：未显式设置的元信息不写入，避免污染项目文件。
func (c *Config) SaveProjectTo(p string) error {
	var buf bytes.Buffer
	buf.WriteString("# shr project rules — 只在本项目内生效，与用户级规则合并（同名键项目级优先）。\n")
	if err := toml.NewEncoder(&buf).Encode(c.toProjectMap()); err != nil {
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
	if c.ProjectDir != "" {
		meta["project_dir"] = c.ProjectDir
	}
	if len(c.Aliases) > 0 {
		meta["aliases"] = aliasMap(c.Aliases)
	}
	out := map[string]interface{}{metaKey: meta}
	for cmd, n := range c.Roots {
		out[cmd] = nodeToMap(n)
	}
	return out
}

func (c *Config) toProjectMap() map[string]interface{} {
	meta := map[string]interface{}{}
	if c.EnabledSet {
		meta["enabled"] = c.Enabled
	}
	if c.AllowDuplicatesSet {
		meta["allow_duplicates"] = c.AllowDuplicates
	}
	if c.ProjectDir != "" {
		meta["project_dir"] = c.ProjectDir
	}
	if len(c.Aliases) > 0 {
		meta["aliases"] = aliasMap(c.Aliases)
	}
	out := map[string]interface{}{}
	if len(meta) > 0 {
		out[metaKey] = meta
	}
	for cmd, n := range c.Roots {
		out[cmd] = nodeToMap(n)
	}
	return out
}

func aliasMap(aliases map[string][]string) map[string]interface{} {
	m := map[string]interface{}{}
	for k, v := range aliases {
		if len(v) == 1 {
			m[k] = v[0]
		} else {
			m[k] = v
		}
	}
	return m
}

func nodeToMap(n *Node) map[string]interface{} {
	m := map[string]interface{}{}
	for k, v := range n.Rules {
		if len(v) == 1 {
			m[k] = v[0]
		} else {
			m[k] = v
		}
	}
	for k, child := range n.Children {
		m[k] = nodeToMap(child)
	}
	return m
}

// ---- 项目级（scoped）配置 ----

// Scope 是从某目录出发加载的完整配置视图：用户级 + 项目级合并。
// 项目级规则文件为 <项目根>/<project_dir>/rules.toml（project_dir 默认 .shr，
// 可用 SHR_PROJECT_DIR 环境变量或用户配置 [__shr] project_dir 自定义，如 .vscode/shr）。
// 项目根由 FindProjectRoot 判定：任一祖先目录含 <project_dir> 目录或其 rules.toml，
// 或存在 .git（git 仓库/工作树）即视为项目——因此写类命令默认落在项目级。
type Scope struct {
	CWD            string
	UserConfig     *Config
	UserPath       string
	ProjectDir     string  // 生效的项目配置子目录（默认 .shr）
	ProjectRoot    string  // 项目根目录；无项目级配置时为空串
	ProjectPath    string  // 项目规则文件路径；无则空串
	HasProjectFile bool    // 项目规则文件是否已存在（add 时用于提示先 shr init）
	ProjectConfig  *Config // 项目配置；无则 nil
	Merged         *Config // 合并后的生效配置
	TargetConfig   *Config // 写入目标：项目存在则项目配置，否则用户配置
	TargetPath     string  // 写入目标路径
}

// LoadScoped 从当前目录出发加载（用户级 + 项目级合并）。
func LoadScoped(cwd string) (*Scope, error) {
	return LoadScopedAt(cwd, Path())
}

// LoadScopedAt 从指定目录出发、基于给定用户级路径加载配置视图，便于测试。
func LoadScopedAt(cwd, userPath string) (*Scope, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	userRaw, err := loadRaw(userPath)
	if err != nil {
		return nil, err
	}
	userCfg, err := configFromRaw(userRaw)
	if err != nil {
		return nil, err
	}
	sp := &Scope{
		CWD:        cwd,
		UserPath:   userPath,
		UserConfig: userCfg,
		ProjectDir: EffectiveProjectDir(userRaw),
	}
	sp.ProjectRoot, sp.ProjectPath = FindProjectRoot(cwd, sp.ProjectDir)
	if sp.ProjectPath != "" {
		if fi, err := os.Stat(sp.ProjectPath); err == nil && !fi.IsDir() {
			sp.HasProjectFile = true
		}
		praw, err := loadRaw(sp.ProjectPath)
		if err != nil {
			return nil, err
		}
		if sp.ProjectConfig, err = configFromRaw(praw); err != nil {
			return nil, err
		}
	}
	sp.Merged = MergeConfigs(sp.UserConfig, sp.ProjectConfig)
	sp.TargetConfig, sp.TargetPath = sp.UserConfig, sp.UserPath
	if sp.ProjectConfig != nil {
		sp.TargetConfig, sp.TargetPath = sp.ProjectConfig, sp.ProjectPath
	}
	return sp, nil
}

// Save 把 TargetConfig 写回目标文件：在项目内时写项目文件（项目模式，不注入默认 meta），
// 否则写用户文件。
func (s *Scope) Save() error {
	if s.ProjectConfig != nil && s.TargetConfig == s.ProjectConfig {
		return s.ProjectConfig.SaveProjectTo(s.ProjectPath)
	}
	return s.UserConfig.SaveTo(s.UserPath)
}

// SaveGlobal 强制写回用户级文件（--global 时用）。
func (s *Scope) SaveGlobal() error {
	return s.UserConfig.SaveTo(s.UserPath)
}

// EffectiveProjectDir 返回生效的项目配置子目录名：
// SHR_PROJECT_DIR 环境变量 > 用户配置 [__shr] project_dir > 默认 ".shr"。
func EffectiveProjectDir(userRaw map[string]interface{}) string {
	if v := os.Getenv("SHR_PROJECT_DIR"); v != "" && ValidProjectDir(v) {
		return v
	}
	if m, ok := userRaw[metaKey].(map[string]interface{}); ok {
		if v, ok := m["project_dir"].(string); ok && ValidProjectDir(v) {
			return v
		}
	}
	return DefaultProjectDir
}

// FindProjectRoot 从 startDir 向上查找"项目根"，就近判定。每个祖先目录按序检查：
//  1. 中央注册表 ~/.shr/projects.toml：以该目录绝对路径为 key 命中（value 为该项目的
//     规则子目录）——显式注册优先，无论规则文件是否已存在都取注册位置；或
//  2. <dir>/.shr-dir 标记文件（旧兼容，内容首行为该项目专属的规则子目录）；或
//  3. <dir>/<projDir>/rules.toml 是文件；或
//  4. <dir>/<projDir> 是目录（手动创建了 `.shr/` 等）；或
//  5. <dir>/.git 存在（git 仓库/工作树，.git 可为目录或文件）。
//
// 返回（项目根目录, <root>/<规则目录>/rules.toml）—— 规则文件可能尚不存在
// （如仅以 .git 作为项目标记时），写类命令会按需创建。找不到返回 ("", "")。
func FindProjectRoot(startDir, projDir string) (root, rulesPath string) {
	if startDir == "" {
		startDir, _ = os.Getwd()
	}
	if projDir == "" {
		projDir = DefaultProjectDir
	}
	dir := startDir
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	reg, _ := loadProjectReg()
	for {
		// 1) 中央注册表：pwd 逐级向上匹配项目根 key
		if d, ok := reg[dir]; ok && ValidProjectDir(d) {
			return dir, filepath.Join(dir, filepath.FromSlash(d), "rules.toml")
		}
		// 2) 旧兼容：<dir>/.shr-dir 标记
		if d, ok := readProjectLoc(dir); ok {
			mp := filepath.Join(dir, filepath.FromSlash(d), "rules.toml")
			return dir, mp
		}
		p := filepath.Join(dir, filepath.FromSlash(projDir), "rules.toml")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return dir, p
		}
		pd := filepath.Join(dir, filepath.FromSlash(projDir))
		if fi, err := os.Stat(pd); err == nil && fi.IsDir() {
			return dir, p
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}

// ---- 中央注册表 ~/.shr/projects.toml ----
//
// 集中管理"哪个项目用哪个规则子目录"，无需在项目根放标记文件：
//
//	["/abs/path/to/projA"]
//	dir = ".vscode/shr"
//
//	["/abs/path/to/projB"]
//	dir = ".shr"

func projectRegPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".shr", "projects.toml")
}

func loadProjectReg() (map[string]string, error) {
	data, err := os.ReadFile(projectRegPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var raw map[string]map[string]interface{}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", projectRegPath(), err)
	}
	reg := map[string]string{}
	for root, m := range raw {
		if d, ok := m["dir"].(string); ok && ValidProjectDir(d) {
			reg[root] = d
		}
	}
	return reg, nil
}

func saveProjectReg(reg map[string]string) error {
	m := map[string]interface{}{}
	for root, d := range reg {
		m[root] = map[string]interface{}{"dir": d}
	}
	var buf bytes.Buffer
	buf.WriteString("# shr 项目规则位置注册表（~/.shr/projects.toml）\n")
	buf.WriteString("# key = 项目根绝对路径，dir = 该项目的规则子目录（相对项目根）。\n")
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return err
	}
	p := projectRegPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, buf.Bytes(), 0o644)
}

// GetProjectReg 查询某项目根的注册规则目录（未注册返回 ("", false)）。
func GetProjectReg(root string) (string, bool) {
	reg, err := loadProjectReg()
	if err != nil {
		return "", false
	}
	d, ok := reg[root]
	return d, ok
}

// SetProjectReg 注册/更新某项目根的规则子目录（写 ~/.shr/projects.toml）。
func SetProjectReg(root, dir string) error {
	reg, err := loadProjectReg()
	if err != nil {
		return err
	}
	reg[root] = dir
	return saveProjectReg(reg)
}

// UnsetProjectReg 删除某项目根的注册；未注册时不做任何事。
func UnsetProjectReg(root string) error {
	reg, err := loadProjectReg()
	if err != nil {
		return err
	}
	if _, ok := reg[root]; !ok {
		return nil
	}
	delete(reg, root)
	return saveProjectReg(reg)
}

// readProjectLoc 读取 <dir>/.shr-dir 标记：取首个非空、非 # 注释、合法的行作为
// 该项目专属的规则子目录名；标记缺失或内容非法返回 ("", false)。
func readProjectLoc(dir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, ".shr-dir"))
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "/"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if ValidProjectDir(line) {
			return line, true
		}
		return "", false
	}
	return "", false
}

// ReadProjectLoc 供 CLI 展示项目专属规则目录标记（<root>/.shr-dir）。
func ReadProjectLoc(root string) (string, bool) {
	return readProjectLoc(root)
}

// MergeConfigs 合并用户级与项目级配置：同名键项目级优先，子表递归合并。
func MergeConfigs(user, proj *Config) *Config {
	out := NewConfig()
	out.Enabled = user.Enabled
	out.AllowDuplicates = user.AllowDuplicates
	out.ProjectDir = user.ProjectDir
	var projRoots map[string]*Node
	var projAliases map[string][]string
	if proj != nil {
		projRoots = proj.Roots
		projAliases = proj.Aliases
		if proj.EnabledSet {
			out.Enabled = proj.Enabled
		}
		if proj.AllowDuplicatesSet {
			out.AllowDuplicates = proj.AllowDuplicates
		}
		if proj.ProjectDir != "" {
			out.ProjectDir = proj.ProjectDir
		}
	}
	out.Roots = mergeRoots(user.Roots, projRoots)
	out.Aliases = mergeAliases(user.Aliases, projAliases)
	return out
}

func mergeRoots(user, proj map[string]*Node) map[string]*Node {
	out := map[string]*Node{}
	for k, v := range user {
		out[k] = mergeNode(v, proj[k])
	}
	for k, v := range proj {
		if _, ok := out[k]; !ok {
			out[k] = mergeNode(nil, v)
		}
	}
	return out
}

func mergeNode(user, proj *Node) *Node {
	n := NewNode()
	if user != nil {
		for k, v := range user.Rules {
			n.Rules[k] = append([]string{}, v...)
		}
		for k, uc := range user.Children {
			n.Children[k] = mergeNode(uc, projChild(proj, k))
		}
	}
	if proj != nil {
		for k, v := range proj.Rules {
			n.Rules[k] = append([]string{}, v...)
		}
		for k, pc := range proj.Children {
			n.Children[k] = mergeNode(n.Children[k], pc)
		}
	}
	return n
}

func projChild(proj *Node, k string) *Node {
	if proj == nil {
		return nil
	}
	return proj.Children[k]
}

func mergeAliases(user, proj map[string][]string) map[string][]string {
	out := map[string][]string{}
	for k, v := range user {
		out[k] = append([]string{}, v...)
	}
	for k, v := range proj {
		out[k] = append([]string{}, v...)
	}
	return out
}

// ValidProjectDir 校验项目配置子目录名：必须是相对路径（非绝对路径），
// 不含 "." / ".." / 空段 / 空白（如 ../x、/abs、.shr/..、"a b" 均非法）。
func ValidProjectDir(dir string) bool {
	if dir == "" || filepath.IsAbs(dir) {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(dir), "/") {
		if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, " \t~") {
			return false
		}
	}
	return true
}
