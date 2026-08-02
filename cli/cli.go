// Package cli 实现 shr 的命令行界面（零依赖手写 argv 解析）。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/havoc-rao/shell-rewrite/core"
	"github.com/havoc-rao/shell-rewrite/version"
	"golang.org/x/term"
)

// 版本信息：Version 来自 VERSION 文件（go:embed 嵌入），Commit/Date 由 goreleaser 注入。
var (
	Version = version.Version
	Commit  = "none"
	Date    = "unknown"
)

const usageText = `shr — shell command shortener: rewrite commands by rules before execution

Usage:
  shr init [zsh|bash|project]             print shell integration code (eval "$(shr init zsh)");
                                          'project' scaffolds <project_dir>/rules.toml in current dir
  shr setup                               interactive setup wizard (TUI, writes rc)
  shr add <cmd> <path...> <expansion>     register a rule (3+ args: subcommand abbrev)
  shr add <abbr> <target>                 register a top-level command alias (2 args)
  shr remove <cmd> <path...>              remove a rule or namespace (2+ args)
  shr remove <abbr>                       remove a top-level alias (1 arg)
  shr list                                show all rules as a tree
  shr expand <argv...>                    show what a command expands to
  shr doctor                              check rules for conflicts
  shr path                                print user rules path; project rules path if inside a project
  shr on | shr off [--global]             enable/disable rewriting (use --global to target user-level)
  shr status                              show whether rewriting is enabled
  shr dup on|off [--global]               allow/disallow multi-value abbrevs (default on)
  shr update [version] [--check]          self-update from GitHub Releases
  shr version                             print version

Project-level rules:
  <项目根>/<project_dir>/rules.toml（默认 .shr，可用 SHR_PROJECT_DIR 环境变量或
  用户配置 [__shr] project_dir 自定义，如 .vscode/shr）与用户级规则合并：
  同名键项目级优先，其余继承用户级。项目内 shr add/remove 等默认写项目文件，
  传 --global 写用户级文件。

Examples:
  shr add git co checkout               # git co        → git checkout
  shr add git lg "log --oneline --graph"
  shr add colink data u upload          # colink data u → colink data upload
  shr add colink d data                 # colink d u    → colink data upload (drill-through)
  shr add git b+ branch                 # git b / br / bra / bran / branc → git branch (prefix)
  shr add git p pull                    # git p → git pull
  shr add git p push                    # git p → git pull | push (runtime TUI pick when dup on)
  shr add c clear                       # c → clear (参数透传, 2-arg alias)
  shr add g git                         # g → git (继续下钻 git 规则树)
  shr add c+ clear                      # c / cl / cle / clea → clear (prefix)

Setup:  shr setup   # interactive wizard (recommended), or: echo 'eval "$(shr init zsh)"' >> ~/.zshrc
`

// Run 执行 CLI，返回进程退出码。
func Run(args []string) int {
	if len(args) < 2 {
		fmt.Print(usageText)
		return 2
	}
	switch args[1] {
	case "init":
		return cmdInit(args[2:])
	case "setup":
		return cmdSetup(args[2:])
	case "add":
		return cmdAdd(args[2:])
	case "remove", "rm":
		return cmdRemove(args[2:])
	case "list", "ls":
		return cmdList()
	case "expand":
		return cmdExpand(args[2:])
	case "doctor":
		return cmdDoctor()
	case "_gen":
		return cmdGen(args[2:])
	case "_pick":
		return cmdPick(args[2:])
	case "path":
		return cmdPath()
	case "on", "enable":
		return cmdToggle(args[2:], true)
	case "off", "disable":
		return cmdToggle(args[2:], false)
	case "status":
		return cmdStatus()
	case "dup":
		return cmdDup(args[2:])
	case "update":
		return cmdUpdate(args[2:])
	case "version", "-version", "--version", "-v":
		fmt.Printf("shr %s (commit %s, built %s)\n", Version, Commit, Date)
		return 0
	case "help", "-h", "--help":
		fmt.Print(usageText)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (see: shr help)\n", args[1])
		return 2
	}
}

// loadScopeOrDie 从当前目录加载（用户级 + 项目级合并）配置视图，失败则退出。
//
// 注意：写类命令（add/remove/on/off/dup）在项目内默认写项目文件（见
// Scope.TargetConfig），--global 时写用户级。
func loadScopeOrDie() *core.Scope {
	s, err := core.LoadScoped("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		os.Exit(1)
	}
	return s
}

// parseGlobalFlag 提取 --global / -g，返回是否强制写用户级，以及剩余参数。
func parseGlobalFlag(args []string) (global bool, rest []string) {
	for _, a := range args {
		if a == "--global" || a == "-g" {
			global = true
		} else {
			rest = append(rest, a)
		}
	}
	return global, rest
}

func saveScope(scope *core.Scope, global bool) error {
	if global {
		return scope.SaveGlobal()
	}
	return scope.Save()
}

func printScopeTarget(scope *core.Scope, global bool) {
	if !global && scope.ProjectPath != "" {
		fmt.Printf("  (project: %s)\n", scope.ProjectPath)
	}
}

const initTTYHintTpl = `shr: 'init' prints shell code that must be evaluated into your shell.

You ran it directly — the code was NOT loaded. Enable shr with one of:

  shr setup                                  # interactive wizard (writes rc, recommended)
  eval "$(shr init %[1]s)"                   # load for the current shell only
  echo 'eval "$(shr init %[1]s)"' >> ~/%[2]s # or append manually (then: source ~/%[2]s)

To just inspect the generated code without loading, pipe it:

  shr init %[1]s | less
  shr init %[1]s > /tmp/shr-init.sh
`

func cmdInit(args []string) int {
	shell := ""
	for _, a := range args {
		if a == "project" {
			return cmdInitProject()
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			fmt.Fprintf(os.Stderr, "shr init: unknown flag %q (see: shr help)\n", a)
			return 2
		}
		if shell == "" {
			shell = a
		} else {
			fmt.Fprintf(os.Stderr, "shr init: unexpected argument %q\n", a)
			return 2
		}
	}
	if shell == "" {
		shell = filepath.Base(os.Getenv("SHELL"))
	}

	scope := loadScopeOrDie()
	code, err := scope.Merged.GenInit(shell, scope.UserPath, scope.ProjectPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	// 直接在交互式终端运行 `shr init bash` 只是把代码打印到屏幕，不会加载进
	// 当前 shell（用户常误以为已生效）。检测到 stdout 是终端时改为给出 eval
	// 引导；真正要查看代码请用管道（如 | less），管道场景下 stdout 非终端，
	// 仍原样输出代码供 eval / 重定向使用。
	if isTerminal(os.Stdout) {
		fmt.Fprintf(os.Stderr, initTTYHintTpl, shell, rcFile(shell))
		return 1
	}
	fmt.Print(code)
	return 0
}

// cmdInitProject 在当前目录生成一份项目级规则文件 <project_dir>/rules.toml，
// 之后在项目内的 shr add/remove 默认写到这里。
func cmdInitProject() int {
	scope := loadScopeOrDie()
	p := filepath.Join(scope.CWD, filepath.FromSlash(scope.ProjectDir), "rules.toml")
	if _, err := os.Stat(p); err == nil {
		fmt.Println("exists: " + p)
		return 0
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	const content = "# shr 项目级规则：只在本项目目录树下生效，与用户级规则（~/.config/shr/rules.toml）合并，\n" +
		"# 同名键以项目级优先；项目内执行 shr add/remove 会写到这里（--global 写用户级）。\n" +
		"#\n# 示例：\n# [git]\n# co = \"checkout\"\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	fmt.Println("created: " + p)
	return 0
}

// rcFile 返回 shell 对应的 rc 文件名（用于 init 引导提示）。
func rcFile(shell string) string {
	switch shell {
	case "zsh":
		return ".zshrc"
	default:
		return ".bashrc"
	}
}

// isTerminal 判断文件描述符是否为交互式终端（用 golang.org/x/term 的 ioctl 检测，
// 比 os.ModeCharDevice 更准确——后者会把 /dev/null 这类字符设备误判为终端）。
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func cmdAdd(args []string) int {
	global, args := parseGlobalFlag(args)

	scope := loadScopeOrDie()
	cfg := scope.TargetConfig
	if global {
		cfg = scope.UserConfig
	}

	// 两参数：一级命令名缩写（argv[0] → target），无子命令路径
	if len(args) == 2 {
		abbr, target := args[0], args[1]
		status, err := cfg.AddAlias(abbr, target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "shr:", err)
			return 1
		}
		if status == core.StatusExists {
			fmt.Printf("exists:  %s → %s\n", abbr, target)
			return 0
		}
		if err := saveScope(scope, global); err != nil {
			fmt.Fprintln(os.Stderr, "shr:", err)
			return 1
		}
		fmt.Printf("%s %s → %s\n", addVerb(status), abbr, target)
		printScopeTarget(scope, global)
		return 0
	}
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: shr add <cmd> <path...> <expansion>  |  shr add <abbr> <target>")
		return 2
	}
	cmd := args[0]
	path := args[1 : len(args)-1]
	expansion := args[len(args)-1]

	status, err := cfg.Add(cmd, path, expansion)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	// 幂等：候选已存在则不写盘
	if status == core.StatusExists {
		lhs := strings.Join(append([]string{cmd}, path...), " ")
		fmt.Printf("exists:  %s → %s\n", lhs, expansion)
		return 0
	}
	if err := saveScope(scope, global); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}

	lhs := strings.Join(append([]string{cmd}, path...), " ")
	rhsParts := append([]string{cmd}, path[:len(path)-1]...)
	rhsParts = append(rhsParts, strings.Fields(expansion)...)
	fmt.Printf("%s %s → %s\n", addVerb(status), lhs, strings.Join(rhsParts, " "))
	printScopeTarget(scope, global)
	return 0
}

func addVerb(status core.AddStatus) string {
	if status == core.StatusAppended {
		return "appended:"
	}
	return "added:  "
}

func cmdRemove(args []string) int {
	global, args := parseGlobalFlag(args)
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: shr remove <cmd> <path...>  |  shr remove <abbr>")
		return 2
	}
	scope := loadScopeOrDie()
	cfg := scope.TargetConfig
	if global {
		cfg = scope.UserConfig
	}
	// 单参数：删除一级命令名缩写
	if len(args) == 1 {
		if !cfg.RemoveAlias(args[0]) {
			fmt.Fprintf(os.Stderr, "shr: alias not found: %s\n", args[0])
			return 1
		}
		if err := saveScope(scope, global); err != nil {
			fmt.Fprintln(os.Stderr, "shr:", err)
			return 1
		}
		fmt.Printf("removed: %s\n", args[0])
		printScopeTarget(scope, global)
		return 0
	}
	if !cfg.Remove(args[0], args[1:]) {
		fmt.Fprintf(os.Stderr, "shr: rule not found: %s\n", strings.Join(args, " "))
		return 1
	}
	if err := saveScope(scope, global); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	fmt.Printf("removed: %s\n", strings.Join(args, " "))
	printScopeTarget(scope, global)
	return 0
}

func cmdList() int {
	scope := loadScopeOrDie()
	cfg := scope.Merged
	if !cfg.Enabled {
		fmt.Println("(rewriting disabled — shr on to enable)")
	}
	if len(cfg.Roots) == 0 && len(cfg.Aliases) == 0 {
		fmt.Println("no rules yet — try: shr add git co checkout")
		return 0
	}
	if scope.ProjectPath != "" {
		fmt.Println("(project: " + scope.ProjectPath + ")")
	}
	for _, cmd := range cfg.SortedRoots() {
		fmt.Println(cmd)
		printNode(cfg.Roots[cmd], "  ")
	}
	if aliases := cfg.SortedAliases(); len(aliases) > 0 {
		fmt.Println("(aliases)")
		width := 0
		for _, k := range aliases {
			if len(k) > width {
				width = len(k)
			}
		}
		for _, k := range aliases {
			v := cfg.Aliases[k]
			disp := strings.Join(v, " | ")
			if strings.HasSuffix(k, "+") {
				word := strings.Fields(v[0])[0]
				fmt.Printf("  %-*s → %s  (%s)\n", width, k, disp,
					strings.Join(core.Prefixes(strings.TrimSuffix(k, "+"), word), ", "))
				continue
			}
			fmt.Printf("  %-*s → %s\n", width, k, disp)
		}
	}
	return 0
}

func printNode(n *core.Node, pad string) {
	width := 0
	for k := range n.Rules {
		if len(k) > width {
			width = len(k)
		}
	}
	for _, k := range sortedKeys(n.Rules) {
		v := n.Rules[k]
		disp := strings.Join(v, " | ")
		if strings.HasSuffix(k, "+") {
			word := strings.Fields(v[0])[0]
			fmt.Printf("%s%-*s → %s  (%s)\n", pad, width, k, disp,
				strings.Join(core.Prefixes(strings.TrimSuffix(k, "+"), word), ", "))
			continue
		}
		fmt.Printf("%s%-*s → %s\n", pad, width, k, disp)
	}
	for _, name := range sortedKeys(n.Children) {
		fmt.Printf("%s%s/\n", pad, name)
		printNode(n.Children[name], pad+"  ")
	}
}

func cmdExpand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: shr expand <argv...>")
		return 2
	}
	scope := loadScopeOrDie()
	fmt.Println(strings.Join(scope.Merged.Expand(args), " "))
	for _, a := range scope.Merged.Ambiguities(args) {
		fmt.Fprintf(os.Stderr, "shr: %s 有多个候选: %s（运行时将弹出选择）\n", a.At, strings.Join(a.Values, " | "))
	}
	return 0
}

func cmdDoctor() int {
	scope := loadScopeOrDie()
	issues := scope.Merged.Doctor()
	if len(issues) == 0 {
		fmt.Println("no issues found")
		return 0
	}
	for _, is := range issues {
		fmt.Println("✗", is)
	}
	return 1
}

func cmdGen(args []string) int {
	if len(args) != 1 || args[0] != "posix" {
		fmt.Fprintln(os.Stderr, "usage: shr _gen posix")
		return 2
	}
	scope := loadScopeOrDie()
	fmt.Print(scope.Merged.GenPosixFuncsFor(scope.ProjectDir))
	return 0
}

// cmdToggle 开关复写：写入配置后 mtime 变化，下一个提示符前热加载重新生成
// wrapper 函数（开启→正常 case，关闭→透传），无需重启 shell。
func cmdToggle(args []string, enable bool) int {
	global, rest := parseGlobalFlag(args)
	if len(rest) > 0 {
		fmt.Fprintf(os.Stderr, "shr: unknown argument %q (see: shr help)\n", strings.Join(rest, " "))
		return 2
	}
	scope := loadScopeOrDie()
	cfg := scope.TargetConfig
	if global {
		cfg = scope.UserConfig
	}
	if cfg.Enabled == enable {
		if enable {
			fmt.Println("shr: rewriting already enabled")
		} else {
			fmt.Println("shr: rewriting already disabled")
		}
		return 0
	}
	cfg.Enabled = enable
	cfg.EnabledSet = true
	if err := saveScope(scope, global); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	if enable {
		fmt.Println("shr: rewriting enabled (takes effect at next prompt)")
	} else {
		fmt.Println("shr: rewriting disabled (takes effect at next prompt)")
	}
	return 0
}

func cmdStatus() int {
	scope := loadScopeOrDie()
	if scope.Merged.Enabled {
		fmt.Println("enabled")
	} else {
		fmt.Println("disabled")
	}
	return 0
}

// cmdDup 控制「允许重复」开关：
//   - on（默认）：add 对已存在缩写追加候选（去重），运行时命中多值弹 TUI 选择；
//   - off：add 命中已存在即报错，避免静默覆盖。
//
// 不带参数则打印当前状态（on/off）。与 on/off（复写总开关）正交，互不影响。
// 默认写项目文件（在项目内时），--global 写用户级。
func cmdDup(args []string) int {
	global, args := parseGlobalFlag(args)
	scope := loadScopeOrDie()
	cfg := scope.TargetConfig
	if global {
		cfg = scope.UserConfig
	}
	if len(args) == 0 {
		if cfg.AllowDuplicates {
			fmt.Println("on")
		} else {
			fmt.Println("off")
		}
		return 0
	}
	switch args[0] {
	case "on", "enable", "true", "yes":
		cfg.AllowDuplicates = true
	case "off", "disable", "false", "no":
		cfg.AllowDuplicates = false
	default:
		fmt.Fprintf(os.Stderr, "shr dup: unknown value %q (use: on|off)\n", args[0])
		return 2
	}
	cfg.AllowDuplicatesSet = true
	if err := saveScope(scope, global); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	if cfg.AllowDuplicates {
		fmt.Println("shr: duplicates allowed — multi-value abbrevs prompt at runtime")
	} else {
		fmt.Println("shr: duplicates disallowed — add rejects existing abbrev")
	}
	return 0
}

func cmdPath() int {
	fmt.Println(core.Path())
	if scope, err := core.LoadScoped(""); err == nil && scope.ProjectPath != "" {
		fmt.Println("project: " + scope.ProjectPath)
	}
	return 0
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}