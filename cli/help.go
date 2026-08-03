// Package cli: 文本类帮助（help / usage / 引导提示）集中定义。
// 所有"输出给用户看该输入什么"的文案统一收拢到 helpText 字段中，
// 各命令通过 helpText.xxx 引用，避免散落在实现里。
package cli

// helpTexts 聚合全部文本帮助。带注释的字段为模板（fmt.Sprintf 格式化用）。
type helpTexts struct {
	Usage       string // 主帮助：shr 无参数 / help / 未知命令提示
	InitTTYHint string // init <shell> 在交互式终端直接运行时的引导（%[1]s=shell, %[2]s=rc 名）

	InitProject    string // usage: shr init project
	InitUnknown    string // init 未知 flag（%q）
	InitUnexpected string // init 多余参数（%q）
	InitNonTTY     string // init 项目引导在非交互环境下的提示
	InitNoProject  string // init 项目引导在非项目目录下的提示

	Add         string // usage: shr add ...
	Remove      string // usage: shr remove ...
	Expand      string // usage: shr expand ...
	Gen         string // usage: shr _gen posix
	Pick        string // usage: shr _pick ...
	Config      string // usage: shr config ...
	ConfigSet   string // usage: shr config set-path ...
	ConfigGet   string // usage: shr config get-path
	ConfigUnset string // usage: shr config unset-path ...

	ProjectNotInit string // add 在项目内但尚无规则文件时的引导
}

var helpText = helpTexts{
	Usage: `shr — shell command shortener: rewrite commands by rules before execution

Usage:
  shr init, -i                           guided project setup: configure rules dir in current project
  shr init [zsh|bash]                    print shell integration code (eval "$(shr init zsh)")
  shr init project                       non-interactive: scaffold project rules file
  shr setup                              interactive setup wizard (TUI, writes rc)
  shr add, -a <cmd> <path...> <expansion>  register a rule (3+ args: subcommand abbrev; default = project, -g = global)
  shr add <abbr> <target>                register a top-level command alias (2 args)
  shr remove, -rm <cmd> <path...>         remove a rule or namespace (2+ args)
  shr remove <abbr>                      remove a top-level alias (1 arg)
  shr list, -l                           show all rules as a tree
  shr expand, -e <argv...>               show what a command expands to
  shr doctor                             check rules for conflicts
  shr path, -p                           print user rules path; project rules path if inside a project
  shr config, -c set-path <dir> [-g]     set rules subdir (e.g. .vscode): current project, or -g for global default
  shr config get-path                    show effective rules subdir, source and file path
  shr config unset-path [-g]             remove per-project registration (-g: clear global default)
  shr on | shr off [-g]                  enable/disable rewriting
  shr status, -s                         show whether rewriting is enabled
  shr dup on|off [-g]                    allow/disallow multi-value abbrevs (default on)
  shr update, -u [version] [--check]     self-update from GitHub Releases
  shr version, -v                        print version

Project-level rules:
  git 仓库（或含 project_dir 目录）即视为项目：<项目根>/<规则目录>/rules.toml，
  与用户级规则合并（同名键项目级优先）。规则目录默认 .shr：
  首次使用在项目内运行 "shr init" 引导式配置；"shr config set-path <dir>" 为当前
  项目单独指定（注册到 ~/.shr/projects.toml），"-g" 设全局默认（用户配置）。
  shr add 默认写项目规则文件（项目尚无规则文件时提示先 shr init），"-g" 写全局。

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
`,

	InitTTYHint: `shr: 'init' prints shell code that must be evaluated into your shell.

You ran it directly — the code was NOT loaded. Enable shr with one of:

  shr setup                                  # interactive wizard (writes rc, recommended)
  eval "$(shr init %[1]s)"                   # load for the current shell only
  echo 'eval "$(shr init %[1]s)"' >> ~/%[2]s # or append manually (then: source ~/%[2]s)

To just inspect the generated code without loading, pipe it:

  shr init %[1]s | less
  shr init %[1]s > /tmp/shr-init.sh
`,

	InitProject:    "usage: shr init project",
	InitUnknown:    "shr init: unknown flag %q（无参数时为项目引导；可用: zsh|bash|project）\n",
	InitUnexpected: "shr init: unexpected argument %q（可用: zsh|bash|project）\n",
	InitNonTTY: `项目引导需要交互式终端。
非交互请用: shr init project  或  shr config set-path <dir>`,
	InitNoProject: `当前目录不在任何项目中（未找到 .git / project_dir 目录）。
请进入项目目录后重试，或用: shr config set-path <dir>`,

	Add:         "usage: shr add <cmd> <path...> <expansion>  |  shr add <abbr> <target>",
	Remove:      "usage: shr remove <cmd> <path...>  |  shr remove <abbr>",
	Expand:      "usage: shr expand <argv...>",
	Gen:         "usage: shr _gen posix",
	Pick:        "usage: shr _pick <label> <cand...>",
	Config:      "usage: shr config set-path <dir> [-g] | get-path | unset-path [-g]",
	ConfigSet:   "usage: shr config set-path <dir> [-g]",
	ConfigGet:   "usage: shr config get-path",
	ConfigUnset: "usage: shr config unset-path [-g]",

	ProjectNotInit: `请先运行: shr init 或: shr config set-path <dir>
如需写全局规则: shr add ... -g`,
}
