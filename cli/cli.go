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
)

// 版本信息：Version 来自 VERSION 文件（go:embed 嵌入），Commit/Date 由 goreleaser 注入。
var (
	Version = version.Version
	Commit  = "none"
	Date    = "unknown"
)

const usageText = `shr — shell command shortener: rewrite commands by rules before execution

Usage:
  shr init [zsh|bash]                   print shell integration code
  shr add <cmd> <path...> <expansion>   register a rule
  shr remove <cmd> <path...>            remove a rule or namespace
  shr list                              show all rules as a tree
  shr expand <argv...>                  show what a command expands to
  shr doctor                            check rules for conflicts
  shr path                              print rules file path
  shr update [version] [--check]        self-update from GitHub Releases
  shr version                           print version

Examples:
  shr add git co checkout               # git co        → git checkout
  shr add git lg "log --oneline --graph"
  shr add colink data u upload          # colink data u → colink data upload
  shr add colink d data                 # colink d u    → colink data upload (drill-through)
  shr add git b+ branch                 # git b / br / bra / bran / branc → git branch (prefix)

Setup (zsh):  echo 'eval "$(shr init zsh)"' >> ~/.zshrc
Setup (bash): echo 'eval "$(shr init bash)"' >> ~/.bashrc
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
	case "path":
		fmt.Println(core.Path())
		return 0
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

func loadOrDie() *core.Config {
	cfg, err := core.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		os.Exit(1)
	}
	return cfg
}

const initTTYHintTpl = `shr: 'init' prints shell code that must be evaluated into your shell.

You ran it directly — the code was NOT loaded. Enable shr with one of:

  eval "$(shr init %[1]s)"                     # load for the current shell only
  echo 'eval "$(shr init %[1]s)"' >> ~/%[2]s   # install permanently (then: source ~/%[2]s)

To just inspect the generated code without loading, pipe it:

  shr init %[1]s | less
  shr init %[1]s > /tmp/shr-init.sh
`

func cmdInit(args []string) int {
	shell := ""
	if len(args) > 0 {
		shell = args[0]
	}
	if shell == "" {
		shell = filepath.Base(os.Getenv("SHELL"))
	}
	cfg := loadOrDie()
	code, err := cfg.GenInit(shell)
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

// rcFile 返回 shell 对应的 rc 文件名（用于 init 引导提示）。
func rcFile(shell string) string {
	switch shell {
	case "zsh":
		return ".zshrc"
	default:
		return ".bashrc"
	}
}

// isTerminal 通过 os.File 的模式判断是否为字符设备（交互式终端），
// 仅用标准库、零依赖；管道/重定向时返回 false。
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func cmdAdd(args []string) int {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: shr add <cmd> <path...> <expansion>")
		return 2
	}
	cmd := args[0]
	path := args[1 : len(args)-1]
	expansion := args[len(args)-1]

	cfg := loadOrDie()
	overwritten, err := cfg.Add(cmd, path, expansion)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	if err := cfg.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}

	lhs := strings.Join(append([]string{cmd}, path...), " ")
	rhsParts := append([]string{cmd}, path[:len(path)-1]...)
	rhsParts = append(rhsParts, strings.Fields(expansion)...)
	verb := "added:  "
	if overwritten {
		verb = "updated:"
	}
	fmt.Printf("%s %s → %s\n", verb, lhs, strings.Join(rhsParts, " "))
	return 0
}

func cmdRemove(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: shr remove <cmd> <path...>")
		return 2
	}
	cfg := loadOrDie()
	if !cfg.Remove(args[0], args[1:]) {
		fmt.Fprintf(os.Stderr, "shr: rule not found: %s\n", strings.Join(args, " "))
		return 1
	}
	if err := cfg.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	fmt.Printf("removed: %s\n", strings.Join(args, " "))
	return 0
}

func cmdList() int {
	cfg := loadOrDie()
	if len(cfg.Roots) == 0 {
		fmt.Println("no rules yet — try: shr add git co checkout")
		return 0
	}
	for _, cmd := range cfg.SortedRoots() {
		fmt.Println(cmd)
		printNode(cfg.Roots[cmd], "  ")
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
		if strings.HasSuffix(k, "+") {
			word := strings.Fields(v)[0]
			fmt.Printf("%s%-*s → %s  (%s)\n", pad, width, k, v,
				strings.Join(core.Prefixes(strings.TrimSuffix(k, "+"), word), ", "))
			continue
		}
		fmt.Printf("%s%-*s → %s\n", pad, width, k, v)
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
	cfg := loadOrDie()
	fmt.Println(strings.Join(cfg.Expand(args), " "))
	return 0
}

func cmdDoctor() int {
	cfg := loadOrDie()
	issues := cfg.Doctor()
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
	cfg := loadOrDie()
	fmt.Print(cfg.GenPosixFuncs())
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
