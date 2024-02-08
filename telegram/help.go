package telegram

// For help text paragraph, which may contains too-long lines.

const HELP_TEXT = `Other messages sent to bot:

- /<cmd_name> : User-defined shortcut cmd added via /addcmd. It's cmdline is executed by active executor. Marked with '*' in commands list
- /executor_<name> : Shortcut cmd for switching executor. Marked with '*' in commands list
- Send a file: save the file to cwd (Use caption to change savepath)
- ^<char> : Send a control key stroke. <char> is a single char. E.g.: ^C (Ctrl-C), ^Z (Ctrl-Z)
- <cmdline> : Any other text message sent to bot is treated as cmdline and executed by active executor

Topics:

Executors : Including internal and user-defined executors. Internal executors are pre-defined and fixed. User-defined executors are added via /addexecutor. To view all, send /executor

Active executor: Default is 'shell' (internal executor). Use "/executor_<name>" or "/executor <name>" to change it

Cwd: Current working directory. Initial to user home dir. Use /cd or /pwd to change or display it. Sending "cd <dir>" in default executor also changes it

Buttons : Buttons are shortcuts for sending cmdline. They appear in keyboard area of telegram chat interface. Latest cmdline activity in active executor are displayed automatically, they are cleared when executor closed. To manage permanent (always-display) shortcuts, use /addbtn, /delbtn or /clearbtn

Config data : All config data (including user-defined cmds, executors and buttons) are stored in ~/.config/tgshell/config.yaml file. If you manually edit the file, restart the program to make changes take effect

For more help, visit https://github.com/sagan/tgshell`

const HISTORY_TIP = `- Click 'Run' to execute
- Click 'Add' to save to buttons
- It works in active executor
- To del buttons, send /buttons`

const BUTTONS_TIP = `- Click 'Del' to delete
- It works in active executor
- To add from history, send /history`

const CMDS_TIP = `- Click 'Del' to delete
- To add new, use /addcmd`

const FILES_TIP = `- Click '↓' to get
- To narrow, use /files <prefix>`

const EXECUTORS_TIP = `- Click 'Del' to delete
- To refresh, send /executors
- To add new, use /addexecutor`

const SERVICES_TIP = `- Click link to open in browser
- Links are valid for 15 minutes
- To refresh, send /services
- To manage, edit config.yaml
- To revoke, send /resetsecret`
