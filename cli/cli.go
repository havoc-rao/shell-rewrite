// Package cli 实现 shr 的命令行界面（零依赖手写 argv 解析）。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/havoc420/shr/core"
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
	case "version", "-version", "--version", "-v":
		fmt.Println("shr 0.1.0")
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
	fmt.Print(code)
	return 0
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
