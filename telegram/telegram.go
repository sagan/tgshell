package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"

	"github.com/sagan/tgshell/config"
	"github.com/sagan/tgshell/constants"
	"github.com/sagan/tgshell/executor"
	"github.com/sagan/tgshell/util"
)

type TgExecutorSession struct {
	Executor executor.Executor
	Chatid   int64
	Ready    bool
}

type TgCommad struct {
	ctx     context.Context // we do not store it, just pass the ctx to channel
	Name    string
	Payload string
	Chatid  int64
	C       tele.Context // telebot ctx
	Output  chan<- string
}

type TgGlobalMsg struct {
	// "global": global message which does NOT belong to a session.
	// "close": a open executor session closed.
	// "reply": reply to a user sent message, C is set
	//  Others: send data to user directly, Chatid is set
	Type     string
	Executor string // executor name
	Data     string
	Chatid   int64        // owning chatid
	C        tele.Context // telebot ctx
}

// chatid => sessionName
type TgActiveSessions map[int64](string)

// name, description, full explain, type.
// type : 0 - normal; 1 - pinned; 2 - hidden.
var commands = [][4]string{
	{"cancel", "Cancel the running command(s)", "", "1"},
	{"close", "Close (active) executor", "Usage: /close [name]", "1"},
	{"executor", "Display or use executor(s)", "Usage: /executor [name]", "1"},
	{"run", "Run cmdline in active executor", USAGE_RUN, "0"},
	{"addcmd", "Add a custom cmd", USAGE_ADDCMD, "0"},
	{"delcmd", "Delete a custom cmd", USAGE_DELCMD, "0"},
	{"addexecutor", "Add a executor", USAGE_ADDEXECUTOR, "0"},
	{"delexecutor", "Delete a executor", USAGE_DELEXECUTOR, "0"},
	{"setsecret", "Set the secret of a executor", USAGE_SETSECRET, "0"},
	{"addbtn", "Add a button to active executor", USAGE_ADDBTN, "0"},
	{"delbtn", "Delete a button of active executor", USAGE_DELBTN, "0"},
	{"clearbtn", "Delete all button of a executor", USAGE_CLEARBTN, "0"},
	{"getfile", "Download a file from server", USAGE_GETFILE, "0"},
	{"resetsecret", "Reset services secret", "", "0"},
	{"refresh", "Refresh bot", "", "0"},
	{"raw", "Send raw input", USAGE_RAW, "0"},
	{"pwd", "Get current working directory", "", "0"},
	{"cd", "Change current working directory", USAGE_CD, "0"},
	{"buttons", "Manage buttons", "", "0"},
	{"executors", "Manage executors", "", "0"},
	{"history", "Manage cmdline history", "", "0"},
	{"files", "Manage files in cwd of server", "Usage: /files [prefix]", "0"},
	{"services", "Access services", "", "0"},
	{"closeall", "Close all opened executors", "", "0"},
	{"help", "Show help", "", "0"},
	{"start", "Welcome", "", "2"},
}

func (as TgActiveSessions) GetActiveSessionName(chatid int64) string {
	if as.IsDefaultExecutorActive(chatid) {
		return config.DEFAULT_EXECUTOR
	}
	return as[chatid]
}

func (as TgActiveSessions) IsDefaultExecutorActive(chatid int64) bool {
	return chatid == 0 || as[chatid] == ""
}

func (as TgActiveSessions) IsActiveSession(chatid int64, sessionName string) bool {
	if as.IsDefaultExecutorActive(chatid) {
		return sessionName == config.DEFAULT_EXECUTOR
	}
	return sessionName == as[chatid]
}

func (tgm *TgGlobalMsg) GetSessionName() string {
	if tgm.Executor == "" || tgm.Executor == config.DEFAULT_EXECUTOR {
		return tgm.Executor
	}
	return fmt.Sprintf("%s_%d", tgm.Executor, tgm.Chatid)
}

func Start(ctx context.Context) {
	servicesProxy, err := NewServicesProxy(config.ConfigData.Services, config.ConfigData.ServicesAddr,
		config.ConfigData.ServicesPort, config.ConfigData.ServicesPublicPort,
		config.ConfigData.ServicesHttps, config.ConfigData.Secret)
	if err != nil {
		log.Fatalf("Failed to create services proxy: %v", err)
	}
	if config.ConfigData.ShellExecutor != "" {
		config.DefaultExecutorConfig.Config = config.ConfigData.ShellExecutor + " " + config.DefaultExecutorConfig.Config
		config.PtyExecutorConfig.Config = config.ConfigData.ShellExecutor + " " + config.PtyExecutorConfig.Config
	}
	shell, err := executor.Create(config.DefaultExecutorConfig, "")
	if err != nil {
		log.Fatalf("Failed to create shell executor: %v", err)
	}
	if err := shell.Open(); err != nil {
		log.Fatalf("Failed to open shell executor: %v", err)
	}
	var commander chan *TgCommad = make(chan *TgCommad, 5)       // global command handler
	var messenger chan *TgGlobalMsg = make(chan *TgGlobalMsg, 5) // global msg to current user
	// session_name => session. session_name is in "executor_chatid" format, excepts for the defaut executor,
	// which is shared between all chats.
	var executorSessions = map[string]*TgExecutorSession{
		config.DEFAULT_EXECUTOR: {
			Executor: shell,
			Ready:    true,
		},
	}
	// chatid => active executor session name
	var activeSessions = TgActiveSessions{}
	bot, err := tele.NewBot(tele.Settings{
		Token:  config.ConfigData.TelegramToken,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatalf("Failed to init bot: %v", err)
	}
	// check whitelist; Check and ignore belated msg
	bot.Use(middleware.Whitelist(config.ConfigData.Whitelist...),
		ignoreBelatedMiddleware(constants.TIMEOUT_MESSAGE, MSG_IGNORE_BELATED))

	var tgCommandHandle = func(c tele.Context) error {
		command, _ := util.SplitFirstAndOthers(c.Message().Text)
		payload := strings.TrimSpace(c.Message().Payload)
		return runCommand(ctx, c, commander, messenger, command, payload)
	}

	for _, command := range commands {
		bot.Handle("/"+command[0], tgCommandHandle)
	}
	bot.Handle(tele.OnCallback, func(c tele.Context) error {
		return runCommand(ctx, c, commander, messenger, "callback", "")
	})
	// receive document
	bot.Handle(tele.OnDocument, func(c tele.Context) error {
		return runCommand(ctx, c, commander, messenger, "document", "")
	})
	bot.Handle(tele.OnPhoto, unsupportedMsgHandle)
	bot.Handle(tele.OnMedia, unsupportedMsgHandle)

	// direct cmdline or custom tg cmd.
	bot.Handle(tele.OnText, func(c tele.Context) error {
		cmdline := strings.TrimSpace(c.Message().Text)
		command, payload := util.SplitFirstAndOthers(cmdline)
		if !strings.HasPrefix(command, "/executor_") {
			if strings.HasPrefix(command, "/") {
				if cmd := config.GetCmd(command[1:]); cmd != nil {
					cmdline = cmd.Cmd
					if payload != "" {
						cmdline += " " + payload
					}
				}
			}
			command = "/run"
			payload = cmdline
		}
		return runCommand(ctx, c, commander, messenger, command, payload)
	})

	if err := setCommands(bot, 0); err != nil {
		log.Printf("Failed to set commands: %v", err)
	}
	log.Printf("bot is now running")
	go event_loop(ctx, bot, servicesProxy, activeSessions, executorSessions, commander, messenger)
	bot.Start()
}

func unsupportedMsgHandle(c tele.Context) error {
	return c.Reply(MSG_UNSUPPORTED_TYPE)
}
