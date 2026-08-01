# shr

**shr**（shell rewrite）是一个命令缩写工具：在执行前按规则改写命令行。

```bash
git co -b feat          →  git checkout -b feat
colink d u --file x     →  colink data upload --file x
git lg                  →  git log --oneline --graph --all
```

与 shell alias 的区别：alias 只能替换命令首词，shr 能改写**任意层级的子命令**，且参数原样透传。

## 特性

- **命令名缩写**：`c` → `clear`、`g` → `git`，一级命令名也可缩写，目标有规则树时自动下钻
- **多层缩写**：`colink data u` → `colink data upload`，层级任意嵌套
- **下钻组合**：中间层也可定义缩写，`colink d u` 与 `colink data u` 等价
- **前缀模式**：`"b+" = "branch"` 一条规则覆盖 `b`、`br`、`bra`、`bran`、`branc`
- **零运行时开销**：规则编译成 shell 嵌套 `case` 函数，执行时不fork外部进程
- **展开回显**：缩写命中时以暗色打印展开后的完整命令（`SHR_ECHO=0` 关闭，非交互终端自动禁用）
- **热加载**：每次渲染提示符前检测配置变更，自动重新编译（zsh/bash）
- **安全透传**：token 失配即停止匹配，剩余参数原样保留，flag 值永远不会被误展开

## 安装

### 方式一：一键脚本（推荐，无需 Go）

```bash
# 安装后启动交互式配置向导（选 shell / 加 PATH / 写 rc），重启 shell 生效：
curl -fsSL https://raw.githubusercontent.com/havoc-rao/shell-rewrite/main/scripts/install.sh | sh

# 或安装 + 非交互写 rc，并立即在当前 shell 生效（无需重启）：
eval "$(curl -fsSL https://raw.githubusercontent.com/havoc-rao/shell-rewrite/main/scripts/install.sh | sh)"
```

自动检测平台、下载 GitHub Releases 中最新的预编译二进制，安装到 `/usr/local/bin`（无写权限时落到 `~/.local/bin` 并自动把它加入 PATH）。`curl | sh` 形式会在装完后启动 `shr setup` 交互向导；`eval "$(...)"` 形式则非交互写入 rc 并让当前会话立即生效。

> 已安装后可直接 `shr update` 自更新到最新版（原地替换当前二进制）。

### 方式二：go install（需 Go 环境）

```bash
go install github.com/havoc-rao/shell-rewrite/cmd/shr@latest   # 安装/更新同一条命令
go install github.com/havoc-rao/shell-rewrite/cmd/shr@v0.1.0   # 指定版本
```

### 方式三：手动下载

前往 [Releases](https://github.com/havoc-rao/shell-rewrite/releases) 下载对应平台的压缩包（`shr_<版本>_<os>_<arch>.tar.gz`），解压后将 `shr` 放入 `PATH`。

### 从源码构建

```bash
git clone https://github.com/havoc-rao/shell-rewrite && cd shell-rewrite
make build                 # 产物输出到 dist/shr
cp dist/shr /usr/local/bin   # 或任意 PATH 中的目录
```

> 若目录不在 git 仓库中，Go 会因 VCS 标记报错，改用
> `go build -buildvcs=false -o shr .`，或执行 `go env -w GOFLAGS=-buildvcs=false` 永久关闭。
> 也可直接 `go install github.com/havoc-rao/shell-rewrite/cmd/shr@latest`。

## 接入 shell

```bash
# 交互式向导（推荐）：自动探测 shell 与安装目录，勾选后一键写入 rc
shr setup

# 非交互（脚本/CI）：按探测结果直接应用
shr setup --yes

# 卸载集成行
shr setup --uninstall
```

> 也可手动：`echo 'eval "$(shr init zsh)"' >> ~/.zshrc`（`shr init` 仅打印集成代码，供 eval 使用）。

> 注意：已有同名 alias 优先于函数，若规则不生效请先 `unalias`。

## 使用

```bash
shr add git co checkout               # git co → git checkout
shr add git lg "log --oneline --graph --all"
shr add colink data u upload          # 多层：colink data u → colink data upload
shr add colink d data                 # 下钻：colink d u → colink data upload
shr add git b+ branch                 # 前缀：git b / br / bra / bran / branc → git branch
shr add npm build "run build"         # 纠错：npm build → npm run build（习惯漏 run，参数原样透传）

shr add c clear                       # 命令名缩写：c → clear（参数透传）
shr add g git                         # g → git（继续下钻 git 的子命令规则）
shr add c+ clear                      # 前缀：c / cl / cle / clea → clear

shr list                              # 树形查看全部规则
shr expand colink d u --file x        # 预览展开结果（调试）
shr remove colink data u              # 删除规则（也可删整个命名空间）
shr doctor                            # 校验规则冲突
shr off                               # 临时关闭复写（命令直通，规则保留）
shr on                                # 恢复复写
```

## 规则语义

匹配采用**逐 token 下钻**，每个 token 的优先级为：**精确规则 > 命名空间下钻 > 前缀规则 > 唯一前缀推断 > 透传**。并遵循三条边界规则：

1. **替换值不二次展开** —— 天然防环；
2. **失配即整体透传** —— 某 token 无任何命中时，从它开始剩余全部原样保留（`git commit -m "co fix"` 中的 `co` 不会被展开）；
3. **下钻用展开后的名字** —— `d = "data"` 命中后可继续匹配 `[colink.data]` 下的规则。

**前缀规则**：key 以 `+` 结尾（`"b+" = "branch"`）表示从 `b` 开始的所有前缀都命中目标；多个前缀规则重叠时取最长 base（如 `br+` 比 `b+` 更具体）；前缀规则的目标有子命名空间时同样支持下钻（`"d+" = "data"` 后 `colink da u` 可用）。

**唯一前缀推断**：多值缩写（`git p` = `push` | `pull`）的候选中，若输入 token 能**唯一**确定为某个候选的严格前缀，则直接补全，不再弹 TUI——`git pus` → `git push`、`git pul` → `git pull`；而 `git p` / `git pu` 是两者公共前缀，仍弹 TUI 选择或透传。该推断排在显式前缀规则之后、透传之前，只针对多值候选，不影响失配透传的安全性。

## 配置文件

`~/.config/shr/rules.toml`（遵循 `XDG_CONFIG_HOME`），嵌套表即规则树：

```toml
[colink]
d  = "data"            # 缩写 → 命名空间名（下钻）
st = "status"          # 普通叶子

[colink.data]          # 命名空间
u = "upload"
d = "download"

[git]
co = "checkout"
"b+" = "branch"                      # 前缀模式：b、br、bra、bran、branc 均可
lg = "log --oneline --graph --all"   # 展开值可带参数（作为终点）

[git.submodule]
up = "update --init"

[__shr.aliases]                       # 一级命令名缩写（argv[0] → target）
c = "clear"                           # c → clear
g = "git"                             # g → git（下钻 [git] 规则树）
"c+" = "clear"                        # 前缀：c / cl / cle / clea → clear
```

可直接手工编辑，之后运行 `shr doctor` 校验。文件变更会在下一个提示符前自动生效，无需重启 shell。

## 工作原理

`shr init zsh` 把规则树机械编译成嵌套 `case` 的 wrapper 函数：

```sh
colink() {
  case "$1" in
    d|data)
      shift
      case "$1" in
        u) shift; command "colink" "data" "upload" "$@" ;;
        d) shift; command "colink" "data" "download" "$@" ;;
        *) command "colink" "data" "$@" ;;
      esac ;;
    st) shift; command "colink" "status" "$@" ;;
    *) command "colink" "$@" ;;
  esac
}
```

三条边界规则在编译产物中自然成立：缩写与原词合并分支（`d|data`）、失配走 `*)` 透传、命中后直接 `command` 执行不再回查。热加载通过 `precmd`（zsh）/ `PROMPT_COMMAND`（bash）比对配置 mtime，变化时 `eval "$(shr _gen posix)"` 重新定义函数。

**一级命令名缩写**（`shr add <abbr> <target>`，2 参数）生成更简单的 wrapper：目标有规则树时以函数调用下钻（`g() { git "$@"; }`，由 `git` 的 case 继续匹配子命令），无规则树时直接 `command` 执行并回显（`c() { _shr_echo clear "$@"; command clear "$@"; }`）。`shr off` 时别名仍保留命令名替换（`command <target> "$@"`），仅关闭子命令下钻——否则缩写名会变成找不到的命令。

## 命令参考

| 命令 | 说明 |
|---|---|
| `shr init [zsh\|bash]` | 输出 shell 集成代码（默认按 `$SHELL` 探测） |
| `shr setup` | 交互式配置向导（TUI，自动探测 shell 与 PATH） |
| `shr setup --yes` | 非交互应用（脚本/CI）；`--uninstall` 移除 |
| `shr add <cmd> <path...> <expansion>` | 注册规则（3+ 参数：子命令缩写）；2 参数时为一级命令名缩写（`add c clear`） |
| `shr remove <cmd> <path...>` | 删除规则或命名空间（2+ 参数）；1 参数删一级命令名缩写 |
| `shr list` | 树形列出全部规则 |
| `shr expand <argv...>` | 显示命令行的展开结果 |
| `shr doctor` | 校验规则冲突 |
| `shr path` | 打印配置文件路径 |
| `shr on` / `shr off` | 开启/关闭复写（关闭后 wrapper 退化为透传，下一个提示符生效） |
| `shr status` | 查看复写开关状态 |
| `shr update [version] [--check]` | 从 GitHub Releases 自更新到最新（或指定）版本 |

## 路线图

- [ ] fish 支持（生成原生 `abbr` + function wrapper）
- [ ] TUI：树形规则浏览 / 交互式添加（增量冲突检测）/ 展开试玩台
- [ ] 展开模式：zsh widget 实现敲击时原地展开（类似 fish abbr）
