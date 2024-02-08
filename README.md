# tgshell

tgshell 是一个运行于 [telegram bot](https://core.telegram.org/bots) 里的终端(shell)模拟器和 ssh 客户端。其主要特性：

- 使用系统 shell 运行用户发送的命令行(cmdline)，并输出命令执行结果。
- 支持使用 pty 模式运行命令行。
- 内置完整功能的 ssh 客户端。
- 内置简易的文件管理功能。支持从服务器下载或上传文件。
- 集成 http 反向代理功能，可以安全地访问内网部署的私有应用。

本程序支持部署在 Windows 和 Linux 系统里。但在 Windows 环境下 pty 等部分功能不可用。以下假定使用 Linux 系统部署本程序。

## 安装 & 配置

本程序使用 Go 开发，所以只有单个可执行程序 "tgshell"，将其放到任意目录然后直接运行即可。程序使用 `~/.config/tgshell/config.yaml` 文件存储配置信息，首次运行本程序时，会自动创建这个文件然后退出。手动编辑这个文件：

~/.config/tgshell/config.yaml

```
telegramtoken: ""
shellexecutor: ""
shellexecutorbuttons: []
whitelist:
  - 0
```

必须修改的地方有 2 处：

- `telegramtoken` : 设置为你的 telegram bot 的 token。参考 telegram 的[文档](https://core.telegram.org/bots)私聊 [@BotFather](https://t.me/botfather) 创建 telegram bot 并获取 token。
- `whitelist` : 将 "0" 修改为你的 telegram 账户的 id。本程序设计用于私有化部署自己的 telegram bot，其唯一的鉴权机制是只接受指定 telegram 里用户发送的消息。不在白名单里的用户发送给 bot 的消息会被直接丢弃，没有任何响应。注意 telegram 账户的 id 与用户名(username)不同；用户 id 是纯数字，不可修改。可以私聊 [@userinfobot](https://t.me/userinfobot) 这个机器人获取自己 tg 账户的用户 id。

然后再次运行程序即可：`tgshell`。如果看到打印 "bot is now running"，表示程序已经成功启动。可以在 telegram 里向本程序的 bot 发送指令了。

## 使用

### 基本用法

本程序使用方法是在 telegram 里聊天界面向本程序发送指令。点击界面底部输入框左侧的 "Menu" 按钮可以看到本程序的 telegram 指令(commands) 列表。tg 指令都以 "/" 字符开始 ，例如 `/cancel`。如果用户发送的消息前缀没有与任何本程序的 tg 指令匹配，它将作为一个命令行(cmdline) 被执行。

发送 `/cancel` 指令停止当前正在运行的 cmdline 进程。

示例：

![screenshot_shell.jpg](https://raw.githubusercontent.com/sagan/tgshell/master/docs/shell.jpg)

### 执行器 (Executor)

本程序使用"执行器"(executor) 运行用户输入的 cmdline。程序内置了 2 个执行器：

- shell : 默认执行器。使用系统的 shell 解释器 (Linux: "bash -c"; Windows: "cmd /C") 执行 cmdline。每个 cmdline 都会启动 1 个单独的 shell 进程。
- pty : 终端(Terminal)执行器。启动一个完整的 pty (默认 "bash")进程负责执行用户输入的所有 cmdline。不支持 Windows 环境。

用户输入的 cmdline 由当前执行器(Active executor)运行。发送 `/executor_<name>` 指令更改当前执行器。例如 `/executor_pty` 会将当前执行器切换为 pty。发送 `/close` 指令关闭当前执行器并恢复使用默认的 shell 执行器。发送 `/executor` 指令查询当前执行器和所有可用的执行器信息。

用户也可以使用 `/addexecutor` 指令添加自定义的执行器，格式为 `/addexecutor <name> <type> [option]`，其中 `<name>` 是添加的执行器的名称。`<type>` 是执行器的类型；本程序目前支持 "ssh" 和 "shell" 两种执行器类型(executor type)。`[option]` 是创建的执行器的参数。用户创建的自定义执行器也会在 telegram 的 Menu 按钮指令列表里显示相应的 `/executor_<name>` 指令。

pty 执行器示例：

![screenshot_pty.jpg](https://raw.githubusercontent.com/sagan/tgshell/master/docs/pty.jpg)

### "ssh" 执行器类型

使用 "ssh" 作为执行器类型创建一个 ssh 连接，例如：

```
/addexecutor myssh ssh example.com
```

以上指令创建了一个名称为 "myssh" 的访问 "example.com" 这个 ssh 服务器的执行器。发送 `/executor_myssh` 连接该 ssh 服务器，之后发送的所有 cmdline 都会在 ssh 服务器上执行。

ssh 执行器默认仅支持公钥认证，自动使用 OpenSSH 的私钥文件(`~/.ssh/id_rsa` 等)。如果需要使用密码方式登录，使用 `/setsecret <name> <secret>` 指令设置 ssh 服务器的密码，其中 `<name>` 为创建的 ssh 执行器的名称。

ssh 执行器会校验 ssh 服务器的公钥文件并与 `~/.ssh/known_hosts` 匹配。如果之前从未连接过该 ssh 服务器，需要先连接一次并接受其公钥，有两种方式：

- a. 在运行本程序的服务器的终端上手动执行 `ssh example.com`，然后接受公钥。
- b. 在本程序的 bot 的 `pty` 执行器里发送 `ssh example.com`，然后发送 "yes" 以接受公钥。

### "shell" 执行器类型

使用 "shell" 作为执行器类型，可以创建一个指向自定义程序的本地执行器，例如：

```
/addexecutor python shell python3
```

创建了一个名为 "python" 的执行器，发送 `/executor_python` 会启动 python3 的交互式环境以执行用户输入的 cmdline。

### 快捷按钮 (Buttons)

本程序会在 telegram bot 聊天界面底部显示一些“快捷按钮”，点击即可直接发送其内容。显示的快捷按钮对应于当前执行器，包括：

- 当前执行器最近的几条 cmdline 历史记录。点击 `/history` 按钮查看完整历史记录。
- 固定显示的一些常用命令按钮。例如内置的 pty 以及自定义的 ssh 类型执行器里会显示 `^C`, `^Z` 快捷按钮，点击即可发送 Ctrl-C 或 Ctrl-Z。
- 用户自己添加的执行器快捷按钮。发送 `/addbtn <cmdline>` 在当前执行器下添加 1 个快捷按钮。发送 `/buttons` 管理当前执行器已添加的快捷按钮。

### 自定义 telegram 指令

发送 `/addcmd <name> <cmdline>` 创建一个任意 cmdline 的自定义 telegram 指令，可以在 telegram 的 Menu 按钮指令列表里看到。自定义指令任何时候均始终可用，而快捷按钮只显示对应当前执行器的。发送 `/cmds` 管理当前已添加的自定义指令。

### 文件管理

本程序集成了简易的文件管理功能，可以管理 bot 运行的服务器上的文件。

- 发送 `/files` 显示当前目录(cwd)下的所有文件。点击消息内容下的 "cd" 按钮进入对应文件夹；点击 "↓" 按钮下载对应文件到 telegram。`/files` 指令也会在快捷按钮里显示。
- 当前目录默认为用户主目录(`~`)。也可以通过发送 `/cd <dir>` 指令改变。发送 `/pwd` 查询当前目录。
- 在 telegram 里发送一个文件(File)给 bot，会自动保存到当前目录下。

### http 反向代理

本程序集成了一个 http 反向代理功能，可用于安全地访问内网发布的服务。在 `config.yaml` 配置 http 服务(services)信息。例如：

```
services:
    - backend: http://127.0.0.1:8086
      hostname: filebrowser.example.com
      name: filebrowser
#serviceport: 8085 # 反向代理默认监听 0.0.0.0 的 8085 端口
```

反向代理使用域名(http 请求的 host) 区分不同服务。上面示例配置中，http://127.0.0.1:8086 是本机部署的仅限本地访问的 [filebrowser](https://github.com/filebrowser/filebrowser) 应用。将域名 "filebrowser.example.com" 解析到服务器的外部 IP 地址。然后在 telegram 中向 bot 发送 `/services` 指令，程序将生成一个服务访问链接并发送给用户：

```
http://filebrowser.example.com:8085/__auth__/<token>
```

如果在本程序外部额外部署了一层 http 或 https 代理，在 config.yaml 里相应增加配置：

```
serviceshttps: true
servicespublicport: 443
```

则程序生成的服务访问链接为 `https://filebrowser.example.com/__auth__/<token>`

打开 `/__auth__/` 链接即可安全地访问内部服务。点击该链接会设置 Cookie 然后重定向到服务首页。反向代理会检查访问请求，只有在存在有效 Cookie 时才会将其转发给后端服务。如果需要重置 Cookie 密钥（使所有之前设置的 Cookie 立即失效），在 telegram 里发送 `/resetsecret`。

### 其它功能

在 telegram 里发送 `/help` 查看本程序所有支持的指令列表和其他说明。
