// Package cli: shr _pick — 多值缩写运行时 TUI 选择器。
//
// 由 shell wrapper 的 _shr_pick 函数回调：
//
//	shr _pick "<label>" <cand1> <cand2> ...
//
// TUI 渲染到 /dev/tty（不被 $(...) 命令替换捕获），选中候选写到 stdout 供
// wrapper 捕获后 command 执行；取消则返回非零，wrapper 据 return 中止执行。
// 非 TTY（无 /dev/tty，如脚本/管道）退化为取首个候选并警告，避免卡住非交互执行。
package cli

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// cmdPick 实现 `shr _pick <label> <cand...>`：TUI 选择器，选中值输出到 stdout。
func cmdPick(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: shr _pick <label> <cand...>")
		return 2
	}
	label := args[0]
	cands := args[1:]

	// TUI 必须有 /dev/tty：渲染与输入都走它，stdout 留给选中值。
	// 无 /dev/tty（非交互）→ 取首个候选并警告，保证脚本不卡。
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shr: 多值缩写 %q 在非交互环境下取首个候选 %q\n", label, cands[0])
		fmt.Println(cands[0])
		return 0
	}
	defer tty.Close()

	// 强制色彩档位为 TrueColor，绕开 lipgloss 的自动探测。
	//
	// 背景：wrapper 用 $(shr _pick ...) 捕获本进程的 stdout，命令替换会把
	// os.Stdout 变成管道（非终端），而 lipgloss 的默认探测正是基于 os.Stdout
	// 判断"是否终端 / 是否支持颜色"，因此永远判定为无色。
	//
	// 之前尝试过 lipgloss.NewRenderer(os.Stderr) + SetDefaultRenderer 来"换一个
	// renderer"，但没有效果：包级 SetDefaultRenderer 只是让*包变量* renderer
	// 指向新对象，而本文件下方 var 块里的 pickBoxStyle 等 Style 在包初始化时
	// （早于 cmdPick 执行）就已经用 lipgloss.NewStyle() 捕获了*当时*的 renderer
	// 指针；后续重新赋值包变量不会回填到这些已经持有旧指针的 Style 上，颜色探测
	// 依然发生在最初那个基于 os.Stdout 的 renderer 上。
	//
	// lipgloss.SetColorProfile 则是在已存在的单例 renderer 对象上原地写字段
	// （r.colorProfile / r.explicitColorProfile），不换指针，因此所有早已持有
	// 该指针的 Style 都会立即感知到——这才是真正生效的修法。
	// 渲染目标 /dev/tty 已知支持颜色（截图可见方框字符正常显示），直接给最高档
	// 位即可，无需再探测。
	lipgloss.SetColorProfile(termenv.TrueColor)

	// 选择记忆：光标默认停在上次选中的候选上（按值匹配，候选已变动则回退首个）。
	cursor := 0
	if last := pickLast(loadPicks(), label); last != "" {
		for i, c := range cands {
			if c == last {
				cursor = i
				break
			}
		}
	}

	m := newPickModel(label, cands, cursor)
	p := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty))
	final, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	res, ok := final.(pickModel)
	if !ok || res.cancelled {
		return 130 // 类似 SIGINT 退出码，wrapper 会 return 中止
	}
	chosen := res.cands[res.cursor]
	savePick(label, chosen) // 记住本次选择，下次默认停在此处
	fmt.Println(chosen)
	return 0
}

type pickModel struct {
	label     string
	cands     []string
	cursor    int
	cancelled bool
}

func newPickModel(label string, cands []string, cursor int) pickModel {
	if cursor < 0 || cursor >= len(cands) {
		cursor = 0
	}
	return pickModel{label: label, cands: cands, cursor: cursor}
}

func (m pickModel) Init() tea.Cmd { return nil }

func (m pickModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.cands)-1 {
				m.cursor++
			}
		// 数字键 1-9 直选：跳过逐行移动，适合候选多时快选
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			n := int(msg.String()[0] - '0')
			if n-1 < len(m.cands) {
				m.cursor = n - 1
				return m, tea.Quit
			}
		case "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

// 复用 setup.go 声明的样式（同属 cli 包），与 shr setup 的向导视觉风格保持
// 一致：无边框、无反色背景块，纯前景色——标题粉色粗体，光标行整行变粉，
// 帮助/说明文字暗灰。不再用 pick 专属的一套（圆角框/背景高亮/分隔线）。
func (m pickModel) View() string {
	numWidth := len(fmt.Sprintf("%d.", len(m.cands)))

	var b strings.Builder
	b.WriteString(setupTitleStyle.Render("shr: " + m.label + " 有多个候选"))
	b.WriteString("\n\n")
	b.WriteString(setupDimStyle.Render("↑↓ move · 1-9 jump · enter confirm · esc cancel"))
	b.WriteString("\n\n")

	for i, c := range m.cands {
		cur := "  "
		if i == m.cursor {
			cur = "► "
		}
		num := fmt.Sprintf("%-*s", numWidth, fmt.Sprintf("%d.", i+1))
		line := fmt.Sprintf("%s%s %s", cur, num, c)
		if i == m.cursor {
			line = setupCurStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
