// Package cli 实现 shr 的命令行界面（零依赖手写 argv 解析）。
package cli

import (
	"bufio"
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

// shortCommands 顶层命令的短别名（shr -i → shr init）。
// 已占用的顶层短参：-v（version）、-h（help）；-g 保留给子命令级 --global，故都不占用。
var shortCommands = map[string]string{
	"-i":  "init",
	"-a":  "add",
	"-rm": "remove",
	"-l":  "list",
	"-e":  "expand",
	"-s":  "status",
	"-p":  "path",
	"-c":  "config",
	"-u":  "update",
}

// Run 执行 CLI，返回进程退出码。
func Run(args []string) int {
	if len(args) < 2 {
		fmt.Print(helpText.Usage)
		return 2
	}
	if cmd, ok := shortCommands[args[1]]; ok {
		args[1] = cmd
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
	case "config":
		return cmdConfig(args[2:])
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
		fmt.Print(helpText.Usage)
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

func cmdInit(args []string) int {
	if len(args) == 0 {
		return cmdInitGuided()
	}
	if args[0] == "project" {
		return cmdInitProject(args[1:])
	}
	shell := args[0]
	if strings.HasPrefix(shell, "-") {
		fmt.Fprintf(os.Stderr, helpText.InitUnknown, shell)
		return 2
	}
	if len(args) > 1 {
		fmt.Fprintf(os.Stderr, helpText.InitUnexpected, args[1])
		return 2
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
		fmt.Fprintf(os.Stderr, helpText.InitTTYHint, shell, rcFile(shell))
		return 1
	}
	fmt.Print(code)
	return 0
}

// cmdInitGuided 无参 `shr init`：在当前项目中引导式配置项目规则目录
// （写入 ~/.shr/projects.toml 注册表并创建规则文件）。
func cmdInitGuided() int {
	if !isTerminal(os.Stdin) {
		fmt.Fprintln(os.Stderr, "shr init:", helpText.InitNonTTY)
		return 1
	}
	scope := loadScopeOrDie()
	if scope.ProjectRoot == "" {
		fmt.Fprintln(os.Stderr, "shr init:", helpText.InitNoProject)
		return 1
	}
	cur := scope.ProjectDir
	if d, ok := core.GetProjectReg(scope.ProjectRoot); ok {
		cur = d
	}
	fmt.Printf("项目根: %s\n", scope.ProjectRoot)
	if scope.HasProjectFile {
		fmt.Printf("项目规则文件（已存在）: %s\n", scope.ProjectPath)
	}
	fmt.Printf("当前规则目录: %s\n", cur)
	fmt.Print("输入项目规则子目录（回车用默认 .shr，如 .vscode/shr）: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	dir := strings.TrimSpace(strings.TrimRight(line, "/"))
	if dir == "" {
		dir = core.DefaultProjectDir
	}
	if !core.ValidProjectDir(dir) {
		fmt.Fprintf(os.Stderr, "shr: 非法项目目录 %q（需为相对路径，且不含 . / .. 段或空白）\n", line)
		return 1
	}
	if err := core.SetProjectReg(scope.ProjectRoot, dir); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	p := filepath.Join(scope.ProjectRoot, filepath.FromSlash(dir), "rules.toml")
	if err := writeProjectRulesFile(p); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	fmt.Printf("已注册项目规则目录: %s\n", dir)
	fmt.Printf("项目规则文件: %s\n", p)
	fmt.Println("接下来：")
	fmt.Println("  shr add git co checkout             # 项目级规则（默认）")
	fmt.Println("  shr add git lg \"log --oneline\" -g   # 全局规则")
	return 0
}

// cmdInitProject 在当前项目生成一份项目级规则文件（非交互），
// 位置取注册表/全局默认/探测结果；之后项目内的 shr add 默认写到这里。
func cmdInitProject(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, helpText.InitProject)
		return 2
	}
	scope := loadScopeOrDie()
	p := scope.ProjectPath
	if p == "" {
		p = filepath.Join(scope.CWD, filepath.FromSlash(scope.ProjectDir), "rules.toml")
	}
	if _, err := os.Stat(p); err == nil {
		fmt.Println("exists: " + p)
		return 0
	}
	if err := writeProjectRulesFile(p); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	fmt.Println("created: " + p)
	return 0
}

const projectRulesTemplate = "# shr 项目级规则：只在本项目目录树下生效，与用户级规则（~/.config/shr/rules.toml）合并，\n" +
	"# 同名键以项目级优先；项目内执行 shr add 会写到这里（-g 写用户级）。\n" +
	"#\n# 示例：\n# [git]\n# co = \"checkout\"\n"

func writeProjectRulesFile(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(projectRulesTemplate), 0o644)
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

	// 项目内但尚无规则文件 → 引导先 shr init，避免用户误以为规则已写入项目
	if !global && scope.ProjectRoot != "" && !scope.HasProjectFile {
		fmt.Fprintf(os.Stderr, "shr: 当前项目还没有规则文件（%s）\n", scope.ProjectPath)
		fmt.Fprintln(os.Stderr, "    "+helpText.ProjectNotInit)
		return 1
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
		fmt.Fprintln(os.Stderr, helpText.Add)
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
		fmt.Fprintln(os.Stderr, helpText.Remove)
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
		fmt.Fprintln(os.Stderr, helpText.Expand)
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
		fmt.Fprintln(os.Stderr, helpText.Gen)
		return 2
	}
	scope := loadScopeOrDie()
	fmt.Print(scope.Merged.GenPosixFuncsFor(scope.ProjectPath))
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

// cmdConfig 查看/设置 shr 配置：
//
//	shr config set-path <dir> [-g]     设置规则存放目录：默认当前项目（注册到
//	                                  ~/.shr/projects.toml）；-g 写全局默认（用户配置
//	                                  [__shr].project_dir），如 .vscode 或 .vscode/shr
//	shr config get-path                 显示当前生效的规则目录及来源、规则文件路径
//	shr config unset-path [-g]          删除当前项目注册（-g：删除全局默认）
func cmdConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, helpText.Config)
		return 2
	}
	switch args[0] {
	case "set-path":
		return cmdConfigSetPath(args[1:])
	case "get-path":
		return cmdConfigGetPath(args[1:])
	case "unset-path":
		return cmdConfigUnsetPath(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "shr config: unknown subcommand %q (see: shr help)\n", args[0])
		return 2
	}
}

func cmdConfigSetPath(args []string) int {
	global, dirs := parseGlobalFlag(args)
	if len(dirs) != 1 {
		fmt.Fprintln(os.Stderr, helpText.ConfigSet)
		return 2
	}
	dir := strings.TrimRight(dirs[0], "/")
	if !core.ValidProjectDir(dir) {
		fmt.Fprintf(os.Stderr, "shr: 非法项目目录 %q（需为相对路径，且不含 . / .. 段或空白）\n", dirs[0])
		return 1
	}
	scope := loadScopeOrDie()
	if global {
		scope.UserConfig.ProjectDir = dir
		if err := scope.SaveGlobal(); err != nil {
			fmt.Fprintln(os.Stderr, "shr:", err)
			return 1
		}
		fmt.Printf("shr: 全局规则目录设为 %q (%s)\n", dir, scope.UserPath)
		if v := os.Getenv("SHR_PROJECT_DIR"); v != "" {
			fmt.Printf("shr: note: $SHR_PROJECT_DIR=%q 优先级更高（env > 用户配置），此刻生效值仍为 %q\n", v, v)
		}
		return 0
	}
	if scope.ProjectRoot == "" {
		fmt.Fprintln(os.Stderr, "shr: 当前目录不在项目中（需要 .git / project_dir 目录 / 注册表条目）")
		fmt.Fprintln(os.Stderr, "        可用: shr config set-path <dir> -g   设置全局默认")
		return 1
	}
	if err := core.SetProjectReg(scope.ProjectRoot, dir); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	fmt.Printf("shr: 当前项目规则目录设为 %q (%s，注册于 ~/.shr/projects.toml)\n",
		dir, filepath.Join(scope.ProjectRoot, filepath.FromSlash(dir), "rules.toml"))
	fmt.Println("       用 shr init 可引导式创建规则文件；或直接 shr add 写规则（需先有规则文件）")
	return 0
}

func cmdConfigGetPath(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, helpText.ConfigGet)
		return 2
	}
	scope := loadScopeOrDie()
	if scope.ProjectRoot != "" {
		if d, ok := core.GetProjectReg(scope.ProjectRoot); ok {
			fmt.Printf("project: %s\n", d)
			fmt.Printf("root:    %s\n", scope.ProjectRoot)
		} else {
			fmt.Printf("project: %s (uses global)\n", scope.ProjectDir)
			fmt.Printf("root:    %s\n", scope.ProjectRoot)
		}
	} else {
		fmt.Printf("project: (not in a project)\n")
		fmt.Printf("global:  %s\n", scope.ProjectDir)
	}
	switch {
	case os.Getenv("SHR_PROJECT_DIR") != "":
		fmt.Println("source:  $SHR_PROJECT_DIR")
	case scope.UserConfig.ProjectDir != "":
		fmt.Println("source:  " + scope.UserPath + " [__shr].project_dir")
	default:
		fmt.Println("source:  default .shr")
	}
	if scope.ProjectPath != "" {
		fmt.Println("rules:   " + scope.ProjectPath)
	}
	return 0
}

func cmdConfigUnsetPath(args []string) int {
	global, rest := parseGlobalFlag(args)
	if len(rest) > 0 {
		fmt.Fprintln(os.Stderr, helpText.ConfigUnset)
		return 2
	}
	scope := loadScopeOrDie()
	if global {
		scope.UserConfig.ProjectDir = ""
		if err := scope.SaveGlobal(); err != nil {
			fmt.Fprintln(os.Stderr, "shr:", err)
			return 1
		}
		fmt.Println("shr: 全局规则目录已恢复默认 (.shr)")
		return 0
	}
	if scope.ProjectRoot == "" {
		fmt.Fprintln(os.Stderr, "shr: 当前目录不在项目中")
		return 1
	}
	if err := core.UnsetProjectReg(scope.ProjectRoot); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	fmt.Printf("shr: 当前项目注册已删除，回退全局规则目录 %q\n", scope.ProjectDir)
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
