# AgentMux

**A desktop control room for AI agents that live on other people's computers.**

[English](#english) · [中文](#中文)

![AgentMux](docs/demo.gif)

*Recorded against a local test box. Nothing is sped up.*
*录制于本地测试机，没有加速。*

---

![Terminal attached to an agent](docs/terminal.png)

**A real terminal, attached to the agent's own tmux pane.** Close the tab and you
have detached, not killed.
**真终端，直接连到 agent 自己的 tmux pane。** 关掉标签页是分离，不是杀掉。

![Broadcast receipts](docs/broadcast.png)

**One instruction, one receipt per agent.** A fan-out you cannot verify is just
hope.
**一条指令，每个 agent 回一张回执。** 无法验证的群发只是许愿。

![Install panel](docs/install.png)

**The install panel only offers what the box can actually run.** No npm on the
server means the npm-based agents stay hidden, and it tells you why.
**安装面板只提供这台机器真能装的东西。** 服务器上没有 npm，基于 npm 的 agent 就
不会出现，并且告诉你原因。

---

## English

### The problem

If you run coding agents on remote machines, the day starts the same way every
time. Open a terminal. SSH in. Try to remember whether the checkout was under
`~/work` or `~/src`. Start the agent. Do it again for the second box. And the
third.

Then you close the laptop to catch a train, and everything you started dies with
the connection.

The workarounds are familiar: a `tmux` cheat sheet taped to the wall, a folder
of shell aliases, a notes file listing which agent is doing what on which host.
It works, right up until you have thirty projects and eight servers, at which
point you are the scheduler, and you are not a good one.

### What AgentMux does

AgentMux is a desktop window onto work that is already running somewhere else.

Everything it starts, it starts inside a `tmux` session on the server. The
desktop app never owns the process. Close the app, lose the wifi, put the laptop
in a bag — the agent keeps working, and when you come back the terminal picks up
exactly where it was, scrollback and all.

Around that one idea sits the thing you actually wanted: a single tree with
every server, every project and every agent in it, colour-coded by what is
running, searchable when the list gets long.

### What it is like to use

**One tree for everything.** Servers, projects, workspaces, agents. A green dot
means running, grey means idle, and the line underneath an agent is the last
thing it printed. Hundreds of rows stay fast because the tree only renders what
is on screen.

**Closing a terminal is not killing it.** Open an agent's terminal and you are
attached to its tmux pane. Close the tab and you have detached. The distinction
sounds small and it changes how you work: you stop being careful with windows.

**It is a real terminal.** Not a chat box with a terminal theme. Colours, mouse,
copy and paste, search. Anything you would type over SSH you type here, in the
same pane the agent is working in, which means you can take over mid-task
without disturbing it.

**Talk to one agent, or twenty.** Tick the agents you want, type an instruction,
send. Each one comes back with a receipt saying whether the message actually
landed. A fan-out you cannot verify is just hope.

**A new box is not a chore.** The Install panel looks at a server and tells you
what is already there and what is missing — Claude Code, Codex, Gemini CLI,
OpenCode, Aider, Cursor CLI, plus the runtimes they need. One click installs
the right one, and the install itself runs inside tmux, so a dropped connection
cannot leave you with half a package tree.

**Six themes.** Midnight, Graphite, Nord, Solarized Dark, Gruvbox Dark and a
proper light theme. `Ctrl+K` and start typing.

### Things it deliberately does not do

It does not replace your terminal, your editor, or your agent. It does not
proxy the agent's model traffic or read its API keys. It is a control plane:
it starts things, watches them, talks to them, and gets out of the way.

Deleting anything in the app never kills remote work. The only button that
destroys a running session is called Kill, and it asks first.

### Getting started

You need Go 1.25+ and Node 20+. On the remote side you need `tmux` and an SSH
account; AgentMux can install tmux for you if it is missing.

```sh
git clone git@github.com:tan-zhuo/AgentMux.git
cd AgentMux
cd frontend && npm install && npm run build && cd ..
go build -o agentmux .
./agentmux
```

The frontend is embedded in the binary, so `npm run build` has to happen before
`go build`.

Add a server, point a workspace at a directory on it, give an agent a command
(`claude`, `opencode`, whatever you run), and press Start.

Keyboard: `Ctrl+K` opens the command palette, which is the fastest way to attach
to an agent, open a shell, switch theme or install a CLI. `Ctrl+B` toggles the
sidebar.

### Where your secrets live

Passwords and key passphrases are encrypted with AES-256-GCM before they touch
disk. The key lives in the OS keychain — Credential Manager on Windows, Keychain
on macOS, Secret Service on Linux. If the keychain cannot be reached, AgentMux
falls back to a `0600` file and says so in the status bar, because a weaker
guarantee should never be a silent one.

Host keys are pinned on first connection. A later mismatch aborts with a plain
explanation rather than a shrug; clearing the pin is something you do on purpose.

Secrets never cross into the UI. The frontend only ever learns whether a secret
is set, never what it is.

### How it is put together

```
main.go                Wails app, service registration, window
internal/store/        SQLite, AES-256-GCM secrets, OS keychain
internal/sshx/         SSH connection pool, auth, PTY manager
internal/tmuxx/        tmux CLI wrapper
internal/agentkit/     catalogue and detection for agent CLIs
internal/app/          the services the frontend calls
frontend/src/          React 19 + TypeScript + Tailwind 4 + xterm.js
```

Go and Wails 3 on the back, React on the front. Around 4,300 lines of Go and
4,000 of TypeScript.

**One connection per server.** Every terminal and every command multiplexes over
a single SSH connection as separate channels — the same idea as OpenSSH's
`ControlMaster`. Idle links are dropped after ten minutes and status polling only
touches servers that are already connected, which is why a hundred configured
machines cost nothing until you use them.

**Agents run in a login shell, not as the process.** Starting an agent creates
`agentmux/{project}/{agent}` and types the command into it. If the agent exits,
the pane survives with its scrollback, and you can take over by typing.

Two things that cost real debugging time, written down so they do not cost yours:

*tmux escapes control characters in its output.* When `tmux -F` output goes to a
client that is not a terminal — an SSH exec channel is not — control bytes get
escaped. A tab comes back as `_`; `0x1f` comes back as the literal text `\037`.
Using a tab as a field separator silently collapsed every row into one
unparseable field. The wrapper uses a printable separator and puts free-form
fields last.

*Terminal bytes are base64 across the IPC boundary.* PTY output is not valid
UTF-8 at arbitrary chunk boundaries, so both directions are encoded and xterm is
fed a `Uint8Array`. The backend also keeps 256 KB of scrollback per shell and
replays it on attach, so switching tabs never shows you a blank terminal.

### Testing

`go test ./...` is safe offline; the integration suite skips unless you point it
at a real host.

```sh
AGENTMUX_TEST_HOST=127.0.0.1 AGENTMUX_TEST_PORT=2222 \
AGENTMUX_TEST_USER=you AGENTMUX_TEST_KEY=/path/to/key \
go test ./internal/integration -v
```

It covers connecting, host key mismatch, connection reuse, the tmux session
lifecycle, scrollback replay, toolchain detection, and the one that matters
most: closing a terminal detaches without killing the remote process.

`AGENTMUX_DATA_DIR` points the app at a different profile, which is how the demo
recording was made without touching a real setup.

### Not there yet

The orchestrator AI from the original design is not built. The plumbing it needs
exists — broadcast already returns per-agent receipts, which was the hard part —
but there is no model panel and no tool-permission model yet.

Folders exist in the schema but the tree renders projects flat. There is no file
sync, no resource graphs, no config import/export, no plugin system, and no
split panes inside a tab.

The install commands are the vendors' documented ones as of writing. Check them
against current docs before trusting them on a machine you care about.

---

## 中文

### 起因

在远程机器上跑编码 agent 的人，每天开头都是同一套动作。开终端，SSH 上去，想
半天代码是在 `~/work` 还是 `~/src`，启动 agent。换第二台再来一遍。第三台再来
一遍。

然后合上笔记本赶地铁，刚才起的东西跟着连接一起没了。

绕过去的办法大家都熟：墙上贴一张 tmux 速查表，一堆 shell 别名，一个记着"哪台
机器上哪个 agent 在干什么"的笔记文件。这套东西能用，直到项目变成三十个、服务器
变成八台——那时候调度器就是你本人，而你干得并不好。

### AgentMux 做什么

AgentMux 是一扇窗，看向已经在别处运行的工作。

它启动的一切都跑在服务器的 tmux 会话里，桌面端从不持有进程。关掉应用、断网、
把电脑塞进包里——agent 照常干活；等你回来，终端接着上次的位置继续，连滚动历史
都在。

围绕这一点，才是你真正想要的东西：一棵树装下所有服务器、项目和 agent，按运行
状态着色，列表长了还能搜。

### 用起来是什么感觉

**所有东西在一棵树里。** 服务器、项目、工作区、agent。绿点表示运行中，灰点表示
空闲，agent 下面那行就是它最后打印的内容。几百行也不卡，因为树只渲染屏幕上能
看见的部分。

**关掉终端不等于杀掉它。** 打开 agent 的终端，你就是附加到了它的 tmux pane 上；
关掉标签页，你只是分离了。这个区别听着小，但它会改变你的习惯——你不用再小心翼翼
地对待窗口。

**这是真终端。** 不是套了终端皮肤的聊天框。颜色、鼠标、复制粘贴、搜索都在。任何
你会在 SSH 里敲的命令都能在这儿敲，而且就在 agent 正在工作的那个 pane 里，所以
你可以中途接手而不打断它。

**对一个 agent 说话，或者对二十个。** 勾选要发的 agent，输入指令，发送。每个都
会回一张回执，告诉你消息到底送到没有。无法验证的群发只是许愿。

**新机器不再是苦差事。** 安装面板会看一眼服务器，告诉你哪些已经装了、哪些没有
——Claude Code、Codex、Gemini CLI、OpenCode、Aider、Cursor CLI，以及它们依赖的
运行时。一键装上，而且安装过程本身跑在 tmux 里，所以掉线不会留给你半个装了一半
的包树。

**六套主题。** Midnight、Graphite、Nord、Solarized Dark、Gruvbox Dark，外加一套
正经的亮色主题。按 `Ctrl+K` 直接输名字。

### 它刻意不做的事

它不取代你的终端、编辑器或 agent。它不代理 agent 的模型流量，也不碰它的 API
key。它是控制面：负责启动、观察、对话，然后让开。

在应用里删任何东西都不会杀掉远端的工作。唯一会摧毁运行中会话的按钮叫 Kill，而且
它会先问你。

### 上手

需要 Go 1.25+ 和 Node 20+。远端需要 `tmux` 和一个 SSH 账号；如果没有 tmux，
AgentMux 可以帮你装。

```sh
git clone git@github.com:tan-zhuo/AgentMux.git
cd AgentMux
cd frontend && npm install && npm run build && cd ..
go build -o agentmux .
./agentmux
```

前端会被嵌进二进制，所以 `npm run build` 必须在 `go build` 之前跑。

加一台服务器，给工作区指一个它上面的目录，给 agent 一条命令（`claude`、
`opencode`，你跑什么就写什么），按 Start。

快捷键：`Ctrl+K` 打开命令面板，这是附加 agent、开 shell、换主题、装 CLI 最快的
路子。`Ctrl+B` 收起侧栏。

### 你的密钥放在哪

密码和私钥口令在落盘之前用 AES-256-GCM 加密。主密钥存在系统钥匙串里——Windows
用凭据管理器，macOS 用 Keychain，Linux 用 Secret Service。如果钥匙串连不上，
AgentMux 会退回到 `0600` 权限的文件，并在状态栏明确告诉你，因为更弱的保证不该
悄无声息。

主机密钥在首次连接时固定。之后不匹配会直接中止并给出清楚的解释，而不是含糊带过；
清除固定值是一件需要你主动去做的事。

密钥从不进入界面层。前端只知道某个密钥"设了没有"，永远不知道它是什么。

### 大致结构

```
main.go                Wails 应用、服务注册、窗口
internal/store/        SQLite、AES-256-GCM 加密、系统钥匙串
internal/sshx/         SSH 连接池、认证、PTY 管理
internal/tmuxx/        tmux 命令封装
internal/agentkit/     agent CLI 的目录与探测
internal/app/          前端调用的服务层
frontend/src/          React 19 + TypeScript + Tailwind 4 + xterm.js
```

后端 Go + Wails 3，前端 React。大约 4300 行 Go、4000 行 TypeScript。

**每台服务器一条连接。** 所有终端和命令都以独立 channel 复用同一条 SSH 连接，
和 OpenSSH 的 `ControlMaster` 是一个思路。空闲连接十分钟后回收，状态轮询只碰
已经连上的服务器——所以配一百台机器，在你真正用到之前几乎不花代价。

**agent 跑在登录 shell 里，而不是作为进程本身。** 启动 agent 会创建
`agentmux/{项目}/{agent}` 会话，然后把命令敲进去。agent 退出时 pane 还在，滚动
历史还在，你可以直接接手继续敲。

两个花了真金白银调试时间的坑，写下来省你的：

*tmux 会转义输出中的控制字符。* 当 `tmux -F` 的输出送给一个非终端的客户端时
（SSH exec 通道就不是终端），控制字节会被转义：制表符变成 `_`，`0x1f` 变成字面量
`\037`。用制表符做字段分隔符会让每一行静默塌成一个无法解析的字段。封装层改用了
可打印分隔符，并把自由文本字段排在最后。

*终端字节在 IPC 边界上走 base64。* PTY 输出在任意分块边界上都不是合法 UTF-8，
所以两个方向都编码，xterm 拿到的是 `Uint8Array`。后端还为每个 shell 保留 256 KB
滚动缓冲并在附加时回放，所以切标签页永远不会看到一片空白。

### 测试

`go test ./...` 离线安全；集成测试除非你指向一台真实主机，否则会跳过。

```sh
AGENTMUX_TEST_HOST=127.0.0.1 AGENTMUX_TEST_PORT=2222 \
AGENTMUX_TEST_USER=you AGENTMUX_TEST_KEY=/path/to/key \
go test ./internal/integration -v
```

覆盖连接、主机密钥不匹配、连接复用、tmux 会话生命周期、滚动回放、工具链探测，
以及最要紧的那条：关闭终端只会分离，不会杀掉远端进程。

`AGENTMUX_DATA_DIR` 可以把应用指向另一份配置，演示录制就是这么做到不碰真实环境的。

### 还没做的

原设计里的主控 AI 还没实现。它需要的管道已经在了——广播已经返回每个 agent 的
回执，这是最难的部分——但还没有模型面板，也没有工具权限模型。

文件夹在数据库里有，但树目前是平铺项目的。没有文件同步、没有资源曲线、没有配置
导入导出、没有插件系统，标签页内也还不能分屏。

安装命令取自各厂商当前的官方文档。在你在意的机器上用之前，请对照最新文档再确认
一遍。

---

## License

MIT
