// Package cli: shr setup — 交互式配置向导（bubbletea TUI）。
//
// 交互式（默认）：单屏多选，↑↓ 移动、space 勾选、enter 应用、q 取消。
// 默认按 $SHELL 预勾选对应 shell 集成；shr 所在目录不在 PATH 时预勾选"加入 PATH"。
//
// 非交互式（--yes）：按探测结果直接应用，用于安装脚本 / CI。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// cmdSetup 启动配置向导。
//
//	usage:
//	  shr setup                       交互式 TUI
//	  shr setup --yes                 非交互：按探测结果直接应用（脚本/CI 用）
//	  shr setup --bin-dir <dir>       覆盖探测到的 shr 目录（用于 PATH 守卫）
//	  shr setup --uninstall           移除 rc 中的集成块
func cmdSetup(args []string) int {
	yes := false
	uninstall := false
	binDirOverride := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--yes" || a == "-y":
			yes = true
		case a == "--uninstall" || a == "--remove":
			uninstall = true
		case a == "--bin-dir":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "shr setup: --bin-dir requires a path")
				return 2
			}
			i++
			binDirOverride = args[i]
		case strings.HasPrefix(a, "--bin-dir="):
			binDirOverride = strings.TrimPrefix(a, "--bin-dir=")
		case strings.HasPrefix(a, "-") && a != "-":
			fmt.Fprintf(os.Stderr, "shr setup: unknown flag %q (see: shr help)\n", a)
			return 2
		default:
			fmt.Fprintf(os.Stderr, "shr setup: unexpected argument %q\n", a)
			return 2
		}
	}
	if yes && uninstall {
		fmt.Fprintln(os.Stderr, "shr setup: --yes and --uninstall are mutually exclusive")
		return 2
	}

	binDir := binDirOverride
	if binDir == "" {
		binDir, _ = shrBinDir()
	}
	detected := detectShell()

	// --uninstall：非交互，对常见 rc 移除集成块。
	if uninstall {
		for _, sh := range []string{"zsh", "bash"} {
			rc := defaultRcPath(sh)
			if rc == "" {
				continue
			}
			msg, err := applyUninstall(rc)
			if err != nil {
				fmt.Fprintf(os.Stderr, "shr: %s: %v\n", rc, err)
				continue
			}
			fmt.Printf("shr: %s\n", msg)
		}
		fmt.Println("shr: restart your shell for changes to take effect")
		return 0
	}

	// --yes：非交互，按探测结果应用。
	if yes {
		// shr 所在目录不在 PATH 时，写入运行时幂等的 PATH 守卫
		pathDir := ""
		if binDir != "" && !dirInPath(binDir) {
			pathDir = binDir
		}
		// 无 SHELL 探测结果时默认 zsh
		sh := detected
		if sh != "zsh" && sh != "bash" {
			sh = "zsh"
		}
		rc := defaultRcPath(sh)
		msg, err := applyInstall(sh, rc, pathDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "shr:", err)
			return 1
		}
		fmt.Printf("shr: %s\n", msg)
		return 0
	}

	// 交互式 TUI：需要 TTY。
	if !isTerminal(os.Stdin) {
		fmt.Fprintln(os.Stderr, "shr: setup requires an interactive terminal.")
		fmt.Fprintln(os.Stderr, "     non-interactive? use: shr setup --yes")
		return 1
	}

	pathInPATH := dirInPath(binDir)
	m := newSetupModel(detected, binDir, pathInPATH)
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	res, ok := final.(setupModel)
	if !ok || !res.applied {
		fmt.Println("shr: setup cancelled")
		return 0
	}
	if res.err != nil {
		fmt.Fprintln(os.Stderr, "shr:", res.err)
		return 1
	}
	fmt.Print(res.result)
	return 0
}

// ---- 探测辅助 ----

// shrBinDir 返回当前 shr 可执行文件所在目录（用于 PATH 守卫）。
func shrBinDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Dir(exe), nil
}

// dirInPath 判断 dir 是否已在 PATH 中。
func dirInPath(dir string) bool {
	if dir == "" {
		return false
	}
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == dir {
			return true
		}
	}
	return false
}

func detectShell() string {
	return filepath.Base(os.Getenv("SHELL"))
}

// ---- 模型 ----

type itemKind int

const (
	itemShell itemKind = iota // shell 集成（勾选）
	itemPath                  // 加入 PATH（勾选）
	itemConfirm               // 应用
	itemCancel                // 取消
)

type setupItem struct {
	kind    itemKind
	shell   string // 仅 itemShell 用
	label   string
	checked bool
}

type setupModel struct {
	items      []setupItem
	cursor     int
	binDir     string
	pathInPATH bool

	applied  bool
	quitting bool
	result   string
	err      error
}

func newSetupModel(detected, binDir string, pathInPATH bool) setupModel {
	m := setupModel{binDir: binDir, pathInPATH: pathInPATH}
	m.items = append(m.items, setupItem{
		kind: itemShell, shell: "zsh",
		label:   "zsh integration  — write eval line to ~/.zshrc",
		checked: detected == "zsh",
	})
	m.items = append(m.items, setupItem{
		kind: itemShell, shell: "bash",
		label:   "bash integration — write eval line to ~/.bashrc",
		checked: detected == "bash",
	})
	if !pathInPATH && binDir != "" {
		m.items = append(m.items, setupItem{
			kind:    itemPath,
			label:   fmt.Sprintf("add %s to PATH (in rc)", binDir),
			checked: true,
		})
	}
	m.items = append(m.items, setupItem{kind: itemConfirm, label: "confirm and apply"})
	m.items = append(m.items, setupItem{kind: itemCancel, label: "cancel"})
	// 默认光标停在 confirm 行，方便直接回车应用
	for i, it := range m.items {
		if it.kind == itemConfirm {
			m.cursor = i
			break
		}
	}
	return m
}

func (m setupModel) Init() tea.Cmd { return nil }

func (m setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case " ":
			if m.cursor < len(m.items) {
				k := m.items[m.cursor].kind
				if k == itemShell || k == itemPath {
					m.items[m.cursor].checked = !m.items[m.cursor].checked
				}
			}
		case "enter":
			if m.cursor < len(m.items) && m.items[m.cursor].kind == itemCancel {
				m.quitting = true
				return m, tea.Quit
			}
			m.apply()
			return m, tea.Quit
		}
	}
	return m, nil
}

// apply 落地：对勾选的 shell 写 rc（按需带 PATH 守卫）。
func (m *setupModel) apply() {
	m.applied = true

	pathDir := ""
	for _, it := range m.items {
		if it.kind == itemPath && it.checked {
			pathDir = m.binDir
		}
	}

	var sb strings.Builder
	any := false
	for _, it := range m.items {
		if it.kind == itemShell && it.checked {
			any = true
			rc := defaultRcPath(it.shell)
			msg, err := applyInstall(it.shell, rc, pathDir)
			if err != nil {
				m.err = fmt.Errorf("%s: %w", rc, err)
				return
			}
			fmt.Fprintf(&sb, "shr: %s\n", msg)
		}
	}
	if !any {
		sb.WriteString("shr: no shell selected — nothing written\n")
	} else {
		sb.WriteString("shr: done. restart your shell, or load now:  eval \"$(shr init <shell>)\"\n")
	}
	m.result = sb.String()
}

// ---- 视图 ----

var (
	setupTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	setupCurStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	setupDimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	setupOkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("36")).Bold(true)
)

func (m setupModel) View() string {
	if m.applied || m.quitting {
		return ""
	}
	var b strings.Builder
	b.WriteString(setupTitleStyle.Render("shr setup"))
	b.WriteString("\n\n")
	b.WriteString(setupDimStyle.Render("↑↓ move · space toggle · enter apply · q cancel"))
	b.WriteString("\n\n")

	for i, it := range m.items {
		cur := "  "
		if i == m.cursor {
			cur = "► "
		}
		var box string
		switch it.kind {
		case itemShell, itemPath:
			if it.checked {
				box = setupOkStyle.Render("[x]")
			} else {
				box = "[ ]"
			}
		case itemConfirm:
			box = "→"
		case itemCancel:
			box = "✕"
		}
		line := fmt.Sprintf("%s%s %s", cur, box, it.label)
		if i == m.cursor {
			line = setupCurStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	if m.binDir != "" {
		b.WriteString("\n")
		if m.pathInPATH {
			b.WriteString(setupDimStyle.Render(fmt.Sprintf("shr location: %s (already in PATH)", m.binDir)))
		} else {
			b.WriteString(setupDimStyle.Render(fmt.Sprintf("shr location: %s (not in PATH)", m.binDir)))
		}
		b.WriteString("\n")
	}
	return b.String()
}
