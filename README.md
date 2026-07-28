# shr

**shr**（shell rewrite）是一个命令缩写工具：在执行前按规则改写命令行。

```bash
git co -b feat          →  git checkout -b feat
colink d u --file x     →  colink data upload --file x
git lg                  →  git log --oneline --graph --all
```

与 shell alias 的区别：alias 只能替换命令首词，shr 能改写**任意层级的子命令**，且参数原样透传。

## 特性

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
curl -fsSL https://raw.githubusercontent.com/havoc-rao/shell-rewrite/main/scripts/install.sh | sh
```

自动检测平台、下载 GitHub Releases 中最新的预编译二进制，安装到 `/usr/local/bin`（无写权限时落到 `~/.local/bin`）。覆盖执行即更新。

> 已安装后可直接 `shr update` 自更新到最新版（等价于重新执行上述脚本，但原地替换当前二进制，无需 sudo 时无需提权）。

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
# zsh
echo 'eval "$(shr init zsh)"' >> ~/.zshrc

# bash
echo 'eval "$(shr init bash)"' >> ~/.bashrc
```

> 注意：已有同名 alias 优先于函数，若规则不生效请先 `unalias`。

## 使用

```bash
shr add git co checkout               # git co → git checkout
shr add git lg "log --oneline --graph --all"
shr add colink data u upload          # 多层：colink data u → colink data upload
shr add colink d data                 # 下钻：colink d u → colink data upload
shr add git b+ branch                 # 前缀：git b / br / bra / bran / branc → git branch

shr list                              # 树形查看全部规则
shr expand colink d u --file x        # 预览展开结果（调试）
shr remove colink data u              # 删除规则（也可删整个命名空间）
shr doctor                            # 校验规则冲突
```

## 规则语义

匹配采用**逐 token 下钻**，每个 token 的优先级为：**精确规则 > 命名空间下钻 > 前缀规则 > 透传**。并遵循三条边界规则：

1. **替换值不二次展开** —— 天然防环；
2. **失配即整体透传** —— 某 token 无任何命中时，从它开始剩余全部原样保留（`git commit -m "co fix"` 中的 `co` 不会被展开）；
3. **下钻用展开后的名字** —— `d = "data"` 命中后可继续匹配 `[colink.data]` 下的规则。

**前缀规则**：key 以 `+` 结尾（`"b+" = "branch"`）表示从 `b` 开始的所有前缀都命中目标；多个前缀规则重叠时取最长 base（如 `br+` 比 `b+` 更具体）；前缀规则的目标有子命名空间时同样支持下钻（`"d+" = "data"` 后 `colink da u` 可用）。

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

## 命令参考

| 命令 | 说明 |
|---|---|
| `shr init [zsh\|bash]` | 输出 shell 集成代码（默认按 `$SHELL` 探测） |
| `shr add <cmd> <path...> <expansion>` | 注册规则，最后一个参数是展开值 |
| `shr remove <cmd> <path...>` | 删除规则或整个命名空间 |
| `shr list` | 树形列出全部规则 |
| `shr expand <argv...>` | 显示命令行的展开结果 |
| `shr doctor` | 校验规则冲突 |
| `shr path` | 打印配置文件路径 |
| `shr update [version] [--check]` | 从 GitHub Releases 自更新到最新（或指定）版本 |

## 路线图

- [ ] fish 支持（生成原生 `abbr` + function wrapper）
- [ ] TUI：树形规则浏览 / 交互式添加（增量冲突检测）/ 展开试玩台
- [ ] 展开模式：zsh widget 实现敲击时原地展开（类似 fish abbr）
