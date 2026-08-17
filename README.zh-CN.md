<img src="build/appicon/icon-128.png" width="96" align="right" alt="AgentMux">

# AgentMux

**管理跑在别人机器上的 AI 编程 agent 的桌面控制台。**

[English](README.md) · 中文

![AgentMux](docs/demo.gif)

---

## 它解决什么

编程 agent 真正干活的地方是真机——构建服务器、GPU 机器、预发环境。于是你 SSH 上去，
起一个 `claude` 或 `codex` 或 `aider`，然后就被它拴住了：合上笔记本，连接一断，活也
跟着没了。三台机器上跑五个，你就变成了调度器——一张 tmux 速查表、一堆 alias、一个
记着"谁在哪台机器上干什么"的备忘文件。

AgentMux 是一扇窗，看向**已经在别处运行**的工作。它启动的每个 agent 都跑在服务器上的
`tmux` 会话里，应用本身从不持有那个进程。所以关掉它、断网、把电脑塞进包里，agent 该干
什么还干什么。

## 为什么用它

**关掉窗口是分离，不是杀掉。** 整个设计就是从这一条长出来的。打开 agent 的终端，你连
的是它自己的 tmux pane；关掉标签页，它照常工作。你不用再小心翼翼地对待窗口。

![连到 agent 的真终端](docs/terminal.png)

**是真终端，不是套了终端皮肤的聊天框。** 颜色、鼠标、选中、搜索，而且就是 agent 正在
敲字的那个 pane——所以你可以中途接手、纠正它、再交回去，全程不用重启任何东西。

**一条指令，每个 agent 回一张回执。** 勾二十个，发一次，收到二十条"到底送没送到"的
答复。无法验证的群发只是许愿。

![群发回执](docs/broadcast.png)

**本地 orchestrator，而且它必须先问。** 给它一个目标——*查清楚 payment agent 为什么
卡住*——它会读集群状态、翻出记得的东西，一次一个工具调用地推进。任何会改动服务器的
动作都先停下来问你，卡片上有工具名、参数、目标机器，以及它自己写的那句"为什么是现在"。
杀 session、删文件这类动作在**每一台**服务器上都要确认，包括你标成信任的那些。它跑在
通过 Ollama 的本地模型上：目标、日志、记忆，没有一样离开这台机器。而且默认是关着的。

**它会记住，而且让你看见记住了什么。** 项目的事实、你说过的偏好、agent 干过的事——
按原文找，或者按意思找。凭据在入库前就被抹掉：真正的风险不是数据库文件被偷，而是同一个
token 在之后每次命中检索时被重新递给模型。

**你批准过的做法，会被复用。** 把"payment 服务在高负载下变慢时该怎么办"写下来，下次
遇到它会被匹配到并照做。orchestrator 自己总结出的技能以草案进来，在你批准之前影响不了
任何决策。

**开荒一台新机器不再是苦差事。** 安装面板会看一眼服务器，只提供它真能装的东西——机器上
没有 npm，基于 npm 的 agent 就不出现，并且告诉你原因。安装过程本身跑在 tmux 里，掉线
不会留给你半个装了一半的包树。

![安装面板](docs/install.png)

**看起来像是这台机器上原本就该有的东西。** 默认是 Apple 的系统配色，分段控件、sheet、
拨动开关，而不是网页的那一套；另外还有六套主题，包括 Nord、Solarized 和一套正经的
亮色主题。

![主题](docs/themes.png)

## 什么场景下值得用

- **长任务，看不住的那种。** 四小时的迁移或重构，穿过合盖、隧道和一顿午饭继续跑。
- **agent 多到脑子装不下。** 三台到五十台机器、每台都有活在跑，全在一棵树里：绿点表示
  运行中，agent 下面那行是它最后打印的内容。
- **全集群下指令。** "停下、拉 main、重跑测试"一次发给所有人，并且拿到送达凭证。
- **开荒。** 新服务器，没有 agent CLI，没有运行时：在列表里挑一个装上。
- **无人值守的巡检。** 定时看一圈，报告卡了一小时的 agent。巡检**只能读**，无论那台
  服务器的信任级别是什么。
- **数据不能出门的活。** 规划模型、embedding、记忆库全在本地。AgentMux 不代理你的 agent
  的模型流量，也不读它的 API key。

## 安装

每个 release 都带三个平台的构建，由 GitHub Actions 从打了 tag 的那个 commit 编出来，
旁边有对应的 `.sha256`。

| 文件 | 是什么 |
|---|---|
| [`agentmux-macos-universal.zip`](../../releases/latest) | `AgentMux.app`，一个二进制同时跑 Intel 和 Apple silicon（macOS 11+） |
| [`agentmux-windows-amd64.zip`](../../releases/latest) | `agentmux.exe` 加安装脚本 |
| [`agentmux-linux-amd64.tar.gz`](../../releases/latest) | 二进制、图标、`.desktop` 文件和 `install.sh` |

远端需要 `tmux` 和一个 SSH 账号；没有 tmux 的话 AgentMux 可以帮你装。

**Linux** 运行时还需要 GTK3 和 WebKitGTK 4.1——Debian/Ubuntu 上是 `libwebkit2gtk-4.1-0`，
Fedora 上是 `webkit2gtk4.1`。大多数桌面本来就装了；真缺了的话 `install.sh` 会说清楚
缺什么，而不是留给你一个一声不吭就退出的二进制。

**这些构建没有做公证和代码签名**——那需要 Apple 开发者账号和微软的证书。Windows 上点
"更多信息"再"仍要运行"。macOS 要看版本：

| 你的 macOS | 怎么开 |
|---|---|
| 15 Sequoia 及以上 | 先双击让它被拦下，然后 系统设置 → 隐私与安全性 → 安全性 → **仍要打开** |
| 14 Sonoma 及以下 | 右键（Control 点按）应用 → **打开** → 再点"打开" |
| 任何版本 | `xattr -dr com.apple.quarantine /Applications/AgentMux.app`，之后正常双击 |

那个隔离标记是**浏览器**打上的，不在压缩包里：用 `curl -L -O <链接>` 下载就没有东西
要清。

## 第一次用

1. **加一台服务器。** 主机、用户，以及 ssh-agent / 密钥 / 密码三选一。支持跳板机。
   主机密钥在第一次连接时被固定。
2. **建项目和工作区**——工作区就是那台服务器上的一个目录。
3. **加一个 agent**：一个名字，加上你实际会敲的命令（`claude`、`codex`，你用什么写
   什么）。按 Start。
4. `Ctrl+K` 打开命令面板，这是附加 agent、开 shell、装 CLI、换主题最快的路子。

密码和密钥口令在落盘之前用 AES-256-GCM 加密，主密钥存在系统钥匙串里——Windows 用凭据
管理器，macOS 用 Keychain，Linux 用 Secret Service。如果钥匙串连不上，AgentMux 会退回
到一个 `0600` 的文件，并在状态栏里说出来：更弱的保证绝不应该是悄悄发生的。

## 它刻意不做的事

它不替代你的终端、编辑器或 agent，也不横在 agent 和它的模型之间。它是控制平面：启动、
盯着、对话，然后让开。

在应用里删任何东西都不会杀掉远端的工作。唯一会摧毁运行中会话的按钮叫 Kill，而且它会
先问你。

## 大致怎么工作

每台服务器一条 SSH 连接，复用：所有终端、命令和文件传输都是同一条连接上的 channel，
所以一台机器上开十个终端只登录一次。没人看着的连接十分钟后自动断开。

agent 跑在 tmux 里的登录 shell 中，而不是作为会话本身的进程——所以 agent 退出后留给你
的是一个待在正确目录里的 shell，而不是一个被销毁的会话。

状态就是应用数据目录里的一个 SQLite 文件。没有服务端，没有守护进程，不需要注册账号。

## 构建、测试、发版

见 [docs/development.md](docs/development.md)（英文）。

Orchestrator 的完整设计——工具闸门、信任级别、记忆与技能层，以及每一条的理由——写在
[docs/orchestrator-blueprint.md](docs/orchestrator-blueprint.md)。

## 许可

MIT，见 [LICENSE](LICENSE)。
