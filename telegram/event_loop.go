package telegram

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tele "gopkg.in/telebot.v3"

	"github.com/sagan/tgshell/config"
	"github.com/sagan/tgshell/constants"
	"github.com/sagan/tgshell/executor"
	"github.com/sagan/tgshell/util"
	"github.com/sagan/tgshell/version"
)

const TYPE_GLOBAL = "global"
const TYPE_REPLY = "reply"
const TYPE_CLOSE = "close"
const MSG_START = "Welcome & Congratulations! You have configured tgshell sucessfully.\nFor help, send /help\n"
const MSG_RESET_EXECUTOR = "Active executor changed to default"
const MSG_IGNORE_BELATED = "ignore message that arrived too late"
const MSG_UNSUPPORTED_TYPE = "Unsupported msg type. Send a cmdline text to run it in active executor; " +
	"Send a file (not gallery or music) to save it to the current working directory of server"
const MSG_EXECUTOR_NOT_FOUND_TPL = "Executor '%s' not found"
const MSG_RESETSECRET = "Services secret resetted. To gain access again, send /services"
const MSG_SUCCESS = "Success"
const MSG_INVALID = "Invalid"
const USAGE_ADDBTN = "Usage: /addbtn <cmdline>"
const USAGE_DELBTN = "Usage: /delbtn <cmdline_prefix>"
const USAGE_CLEARBTN = "Usage: /clearbtn <executor>"
const USAGE_SETSECRET = `Usage: /setsecret <executor> [secret]
If [secret] is empty, clear it`
const USAGE_ADDEXECUTOR = `Usage: /addexecutor <name> <type> [config]
E.g.: /addexecutor myssh ssh 1.2.3.4`
const USAGE_DELEXECUTOR = `Usage: /delexecutor <name>
E.g.: /delexecutor myssh`
const USAGE_RUN = `Usage: /run <cmdline>
E.g.: /run /usr/bin/ls -lh`
const USAGE_ADDCMD = `Usage: /addcmd <name> <cmdline>
E.g.: /addcmd ping ping -c 5 8.8.8.8`
const USAGE_DELCMD = `Usage: /delcmd <name>
E.g.: /delcmd ping`
const USAGE_GETFILE = "Usage: /getfile /path/to/file.txt"
const USAGE_CD = `Usage: /cd [dir]
[dir] default to user home dir`
const USAGE_RAW = `Usage: /raw <sequence>
<sequence> is a C-style escape string. E.g.:
\x03pwd\n : Send 0x03 (Ctrl-C) + "pwd" + "\n"`

var CTRL_SEQUENCE_REGEXP = regexp.MustCompile(`^(?i)(Ctrl[-\+]|\^)(?P<char>\S)$`)

func event_loop(ctx context.Context, bot *tele.Bot, servicesProxy *ServicesProxy,
	activeSessions TgActiveSessions, executorSessions map[string]*TgExecutorSession,
	commander chan *TgCommad, messenger chan *TgGlobalMsg) {
	globalCancelSign := make(chan struct{})
main:
	for {
		select {
		case tgcmd := <-commander:
			tgcmdName := tgcmd.Name
			tgcmdPayload := tgcmd.Payload
			if strings.HasPrefix(tgcmdName, "/executor_") {
				tgcmdPayload = strings.TrimPrefix(tgcmdName, "/executor_")
				if tgcmdPayload != "" && tgcmd.Payload != "" {
					tgcmdPayload += " "
				}
				tgcmdPayload += tgcmd.Payload
				tgcmdName = "/executor"
			}
			switch tgcmdName {
			case "/executors":
				{
					close(tgcmd.Output)
					executors := config.ConfigData.Executors
					data := fmt.Sprintf("Executors (%d) (user-defined)\n%s\n\n", len(executors), EXECUTORS_TIP)
					var inlineKeyboard [][]tele.InlineButton
					var inlineKeyboardRow []tele.InlineButton
					for i, executor := range executors {
						data += fmt.Sprintf("%-2d  %s  %s\n", i, executor.Name, executor.Desc())
						inlineKeyboardRow = append(inlineKeyboardRow, tele.InlineButton{
							Text: fmt.Sprintf("Del %d", i),
							Data: fmt.Sprintf("del_%s", executor.Name),
						})
						if len(inlineKeyboardRow) >= constants.TG_ROW_BUTTONS {
							inlineKeyboard = append(inlineKeyboard, inlineKeyboardRow)
							inlineKeyboardRow = nil
						}
					}
					if len(inlineKeyboardRow) > 0 {
						inlineKeyboard = append(inlineKeyboard, inlineKeyboardRow)
						inlineKeyboardRow = nil
					}
					menu := &tele.ReplyMarkup{InlineKeyboard: inlineKeyboard}
					tgcmd.C.Reply(data, menu, tele.NoPreview)
				}
			case "/files":
				{
					prefix := tgcmdPayload
					if cwd, err := os.Getwd(); err != nil {
						tgcmd.Output <- MSG_INVALID
					} else if files, err := os.ReadDir(cwd); err != nil {
						tgcmd.Output <- MSG_INVALID
					} else {
						no := 0
						data := fmt.Sprintf("Files - %s\nPrefix: %s\n%s\n\n", cwd, prefix, FILES_TIP)
						chars := utf8.RuneCountInString(data)
						var inlineKeyboard [][]tele.InlineButton
						var inlineKeyboardRow []tele.InlineButton
						for _, file := range files {
							if prefix != "" && !strings.HasPrefix(file.Name(), prefix) {
								if file.Name() < prefix {
									continue
								} else {
									break
								}
							}
							flag := "-"
							size := "?"
							if file.IsDir() {
								flag = "d"
								size = "-"
							} else if stat, err := os.Stat(path.Join(cwd, file.Name())); err == nil {
								size = util.BytesSizeAround(float64(stat.Size()))
							}
							filedata := fmt.Sprintf("%-2d %1s %4s  %s\n", no, flag, size, file.Name())
							if newchars := utf8.RuneCountInString(filedata) + chars; newchars > constants.TG_TEXT_LIMIT {
								break
							} else {
								data += filedata
								chars = newchars
							}
							if file.IsDir() {
								inlineKeyboardRow = append(inlineKeyboardRow, tele.InlineButton{
									Text: fmt.Sprintf("cd %d", no),
									Data: fmt.Sprintf("cd_%d", no),
								})
							} else {
								inlineKeyboardRow = append(inlineKeyboardRow, tele.InlineButton{
									Text: fmt.Sprintf("↓ %d", no),
									Data: fmt.Sprintf("get_%d", no),
								})
							}
							if len(inlineKeyboardRow) >= constants.TG_ROW_BUTTONS {
								inlineKeyboard = append(inlineKeyboard, inlineKeyboardRow)
								inlineKeyboardRow = nil
							}
							no++
							if no > constants.TG_FILES_MAX {
								break
							}
						}
						inlineKeyboardRow = append(inlineKeyboardRow, tele.InlineButton{
							Text: "cd .",
							Data: "cd_.",
						})
						inlineKeyboardRow = append(inlineKeyboardRow, tele.InlineButton{
							Text: "cd ..",
							Data: "cd_..",
						})
						inlineKeyboard = append(inlineKeyboard, inlineKeyboardRow)
						menu := &tele.ReplyMarkup{InlineKeyboard: inlineKeyboard}
						tgcmd.C.Reply(data, menu, tele.NoPreview)
					}
					close(tgcmd.Output)
				}
			case "/services":
				{
					services := config.ConfigData.Services
					data := fmt.Sprintf("Services (%d)\n%s\n\n", len(services), SERVICES_TIP)
					for _, service := range services {
						url, _ := servicesProxy.GetUrl(service.Hostname)
						data += fmt.Sprintf("- %s : %s\n\n", service.GetName(), url)
					}
					tgcmd.Output <- data
					close(tgcmd.Output)
				}
			case "/buttons":
				{
					close(tgcmd.Output)
					sessionName := activeSessions.GetActiveSessionName(tgcmd.Chatid)
					session := executorSessions[sessionName]
					buttons := config.GetExecutorButtons(session.Executor.Name())
					data := fmt.Sprintf("Buttons (%d) - %s\n%s\n\n", len(buttons), session.Executor.Name(), BUTTONS_TIP)
					var inlineKeyboard [][]tele.InlineButton
					var inlineKeyboardRow []tele.InlineButton
					for i, cmdline := range buttons {
						data += fmt.Sprintf("%d  %s\n", i, cmdline)
						inlineKeyboardRow = append(inlineKeyboardRow, tele.InlineButton{
							Text: fmt.Sprintf("Del %d", i),
							Data: fmt.Sprintf("del_%d", i),
						})
						if len(inlineKeyboardRow) >= constants.TG_ROW_BUTTONS {
							inlineKeyboard = append(inlineKeyboard, inlineKeyboardRow)
							inlineKeyboardRow = nil
						}
					}
					if len(inlineKeyboardRow) > 0 {
						inlineKeyboard = append(inlineKeyboard, inlineKeyboardRow)
						inlineKeyboardRow = nil
					}
					menu := &tele.ReplyMarkup{InlineKeyboard: inlineKeyboard}
					tgcmd.C.Reply(data, menu, tele.NoPreview)
				}
			case "/cmds":
				{
					close(tgcmd.Output)
					cmds := config.ConfigData.Cmds
					data := fmt.Sprintf("Commands (%d) - custom\n%s\n\n", len(cmds), CMDS_TIP)
					var inlineKeyboard [][]tele.InlineButton
					var inlineKeyboardRow []tele.InlineButton
					for i, cmd := range cmds {
						data += fmt.Sprintf("%d  %s  %s\n", i, cmd.Name, cmd.Cmd)
						inlineKeyboardRow = append(inlineKeyboardRow, tele.InlineButton{
							Text: fmt.Sprintf("Del %d", i),
							Data: fmt.Sprintf("del_%s", cmd.Name),
						})
						if len(inlineKeyboardRow) >= constants.TG_ROW_BUTTONS {
							inlineKeyboard = append(inlineKeyboard, inlineKeyboardRow)
							inlineKeyboardRow = nil
						}
					}
					if len(inlineKeyboardRow) > 0 {
						inlineKeyboard = append(inlineKeyboard, inlineKeyboardRow)
						inlineKeyboardRow = nil
					}
					menu := &tele.ReplyMarkup{InlineKeyboard: inlineKeyboard}
					tgcmd.C.Reply(data, menu, tele.NoPreview)
				}
			case "/history":
				{
					close(tgcmd.Output)
					sessionName := activeSessions.GetActiveSessionName(tgcmd.Chatid)
					session := executorSessions[sessionName]
					history := session.Executor.History()
					if len(history) > 20 {
						history = history[len(history)-20:]
					}
					data := fmt.Sprintf("History (%d) - %s\n%s\n\n", len(history), session.Executor.Name(), HISTORY_TIP)
					var inlineKeyboard [][]tele.InlineButton
					var inlineKeyboardRow []tele.InlineButton
					for i, cmdline := range history {
						data += fmt.Sprintf("%d  %s\n", i, cmdline)
						inlineKeyboardRow = append(inlineKeyboardRow, tele.InlineButton{
							Text: fmt.Sprintf("Run %d", i),
							Data: fmt.Sprintf("run_%d", i),
						})
						if len(inlineKeyboardRow) >= constants.TG_ROW_BUTTONS {
							inlineKeyboard = append(inlineKeyboard, inlineKeyboardRow)
							inlineKeyboardRow = nil
						}
					}
					for i := range history {
						inlineKeyboardRow = append(inlineKeyboardRow, tele.InlineButton{
							Text: fmt.Sprintf("Add %d", i),
							Data: fmt.Sprintf("add_%d", i),
						})
						if len(inlineKeyboardRow) >= constants.TG_ROW_BUTTONS {
							inlineKeyboard = append(inlineKeyboard, inlineKeyboardRow)
							inlineKeyboardRow = nil
						}
					}
					if len(inlineKeyboardRow) > 0 {
						inlineKeyboard = append(inlineKeyboard, inlineKeyboardRow)
						inlineKeyboardRow = nil
					}
					menu := &tele.ReplyMarkup{InlineKeyboard: inlineKeyboard}
					tgcmd.C.Reply(data, menu, tele.NoPreview)
				}
			case "callback":
				{
					result := ""
					doNotCloseOutput := false
					session := executorSessions[activeSessions.GetActiveSessionName(tgcmd.Chatid)]
					if parameters := strings.Split(tgcmd.C.Callback().Data, "_"); len(parameters) != 2 {
						result = MSG_INVALID
					} else if action, index := parameters[0], parameters[1]; action == "" || index == "" {
						result = MSG_INVALID
					} else if msg := tgcmd.C.Callback().Message; msg == nil {
						result = MSG_INVALID
					} else if strings.HasPrefix(msg.Text, "History ") {
						lines := strings.Split(msg.Text, "\n")
						if cmdline := util.FindLineDataByFirstField(lines, index); cmdline == "" {
							result = MSG_INVALID
						} else if action == "run" {
							doNotCloseOutput = true
							result = fmt.Sprintf("Run %s: %s", index, cmdline)
							tgcmd.Output <- cmdline
							command_run(tgcmd.ctx, session, tgcmd.Output, cmdline)
						} else if action == "add" {
							result = fmt.Sprintf("Add %s: %s", index, cmdline)
							config.AddExecutorButton(session.Executor.Name(), cmdline)
							tgcmd.Output <- fmt.Sprintf("Add '%s' to buttons", cmdline)
						}
					} else if strings.HasPrefix(msg.Text, "Buttons ") {
						lines := strings.Split(msg.Text, "\n")
						if cmdline := util.FindLineDataByFirstField(lines, index); cmdline == "" {
							result = MSG_INVALID
						} else if action == "del" {
							if err := config.DelExecutorButton(session.Executor.Name(), cmdline); err == nil {
								result = fmt.Sprintf("Del button '%s'", cmdline)
								tgcmd.Output <- result
							}
						}
					} else if strings.HasPrefix(msg.Text, "Commands ") {
						if err := config.DelCmd(index); err == nil {
							setCommands(bot, tgcmd.Chatid)
						}
						result = fmt.Sprintf("Del cmd '%s'", index)
					} else if strings.HasPrefix(msg.Text, "Executors ") {
						if action == "del" {
							if err := config.DelExecutor(index); err == nil {
								result = fmt.Sprintf("Del executor '%s'", index)
								setCommands(bot, tgcmd.Chatid)
							}
						}
					} else if strings.HasPrefix(msg.Text, "Files ") {
						lines := strings.Split(msg.Text, "\n")
						// first line: "Files - <filename>"
						dir := lines[0][8:]
						log.Printf("dir=%s, action=%s, index=%s", dir, action, index)
						if index == "." || index == ".." {
							if action == "cd" {
								filepath := path.Clean(path.Join(dir, index))
								tgcmd.Output <- fmt.Sprintf("cd %s", filepath)
								os.Chdir(filepath)
							} else {
								result = MSG_INVALID
							}
						} else if fileinfo := util.FindLineDataByFirstField(lines, index); fileinfo == "" || len(fileinfo) < 9 {
							// "%1s %4s  %s\n", 8 bytes before filename
							result = MSG_INVALID
						} else if filepath := path.Clean(path.Join(dir, fileinfo[8:])); filepath == "" {
							result = MSG_INVALID
						} else if action == "cd" {
							tgcmd.Output <- fmt.Sprintf("cd %s", filepath)
							os.Chdir(filepath)
						} else if action == "get" {
							go func(C tele.Context) {
								C.Reply(fmt.Sprintf("Sending %s", filepath))
								C.Reply(&tele.Document{File: tele.FromDisk(filepath), FileName: path.Base(filepath)})
							}(tgcmd.C)
						}
					} else {
						result = MSG_INVALID
					}
					tgcmd.C.Respond(&tele.CallbackResponse{Text: result})
					if !doNotCloseOutput {
						close(tgcmd.Output)
					}
				}
			case "/getfile":
				{
					if tgcmdPayload == "" {
						tgcmd.Output <- USAGE_GETFILE
					} else {
						filepath := ""
						if path.IsAbs(tgcmdPayload) {
							filepath = tgcmdPayload
						} else if cwd, err := os.Getwd(); err == nil {
							filepath = path.Clean(path.Join(cwd, tgcmdPayload))
						}
						if filepath == "" {
							tgcmd.Output <- MSG_INVALID
						} else if stat, err := os.Stat(filepath); err != nil {
							tgcmd.Output <- fmt.Sprintf("File '%s' does NOT exist", filepath)
						} else if !stat.Mode().IsRegular() {
							tgcmd.Output <- fmt.Sprintf("File '%s' is not a regular file", filepath)
						} else {
							go func(C tele.Context) {
								C.Reply(fmt.Sprintf("Sending %s (%s)", filepath, util.BytesSize(float64(stat.Size()))))
								C.Reply(&tele.Document{File: tele.FromDisk(filepath), FileName: path.Base(filepath)})
							}(tgcmd.C)
						}
					}
					close(tgcmd.Output)
				}
			// tg document
			case "document":
				{
					close(tgcmd.Output)
					savePath, err := os.Getwd()
					if err != nil {
						savePath = "."
					}
					if userpath := strings.TrimSpace(tgcmd.C.Message().Caption); userpath != "" {
						if path.IsAbs(userpath) {
							savePath = userpath
						} else {
							savePath = path.Clean(path.Join(savePath, userpath))
						}
					}
					go func(ctx context.Context, cancelSign <-chan struct{}, tgtoken string,
						chatid int64, savepath string, tgdocument *tele.Document) {
						ctx, cancel := util.ContextWithCancelSign(ctx, cancelSign)
						defer cancel()
						filename := tgdocument.FileName
						filepath := path.Join(savepath, filename)
						messenger <- &TgGlobalMsg{
							Type:   TYPE_GLOBAL,
							Chatid: chatid,
							Data: fmt.Sprintf("Saving file '%s' (%s) to '%s' . To cancel, send /cancel", filename,
								util.BytesSize(float64(tgdocument.FileSize)), savePath),
						}
						err := util.DownloadTgFileToLocal(ctx, tgtoken, tgdocument.FileID, filepath)
						if err != nil {
							messenger <- &TgGlobalMsg{
								Type:   TYPE_GLOBAL,
								Chatid: chatid,
								Data:   fmt.Sprintf("Failed to save file to '%s': %v", filepath, err),
							}
						} else {
							messenger <- &TgGlobalMsg{
								Type:   TYPE_GLOBAL,
								Chatid: chatid,
								Data:   "Successfully saved file to the below path:",
							}
							messenger <- &TgGlobalMsg{Type: TYPE_GLOBAL, Chatid: chatid, Data: filepath}
						}
					}(ctx, globalCancelSign, config.ConfigData.TelegramToken,
						tgcmd.Chatid, savePath, tgcmd.C.Message().Document)
				}
			case "/help":
				{
					msg := "tgshell is a telegram bot program which works as a terminal emulator and ssh client.\n"
					msg += "Version: " + version.Version + "\n"
					msg += "Commit: " + version.Commit + "\n"
					msg += "Built at: " + version.Date + "\n\n"

					msg += "Available commands:\n\n"
					for _, command := range commands {
						msg += fmt.Sprintf("/%s: %s\n", command[0], command[1])
						if command[2] != "" {
							msg += command[2] + "\n"
						}
						msg += "\n"
					}
					msg += "Internal executors:\n\n"
					for _, internalExecutor := range config.InternalExecutors {
						msg += fmt.Sprintf("- %s\n%s\n\n", internalExecutor.Name, internalExecutor.Comment)
					}
					msg += "Available executor types:\n\n"
					for _, regInfo := range executor.Registry {
						msg += fmt.Sprintf("- %s\n%s\n\n", regInfo.Name, regInfo.Usage)
					}
					tgcmd.Output <- msg
					tgcmd.Output <- HELP_TEXT
					close(tgcmd.Output)
				}
			case "/start":
				{
					setCommands(bot, tgcmd.Chatid)
					tgcmd.Output <- MSG_START
					close(tgcmd.Output)
				}
			case "/refresh":
				{
					setCommands(bot, tgcmd.Chatid)
					tgcmd.Output <- "Success"
					close(tgcmd.Output)
				}
			case "/close":
				{
					sessionName := tgcmdPayload
					if sessionName == "" {
						sessionName = activeSessions.GetActiveSessionName(tgcmd.Chatid)
					} else if newName := fmt.Sprintf("%s_%d", sessionName, tgcmd.Chatid); executorSessions[newName] != nil {
						sessionName = newName
					}
					if sessionName == config.DEFAULT_EXECUTOR {
						executorSessions[sessionName].Executor.Clear()
						tgcmd.Output <- "Using default executor"
					} else if executorSessions[sessionName] == nil {
						tgcmd.Output <- fmt.Sprintf(MSG_EXECUTOR_NOT_FOUND_TPL, sessionName)
					} else {
						executorSessions[sessionName].Executor.Close()
						delete(executorSessions, sessionName)
						if activeSessions.IsActiveSession(tgcmd.Chatid, sessionName) {
							delete(activeSessions, tgcmd.Chatid)
							tgcmd.Output <- MSG_RESET_EXECUTOR
						}
					}
					close(tgcmd.Output)
				}
			case "/closeall":
				{
					for name := range executorSessions {
						if name == config.DEFAULT_EXECUTOR {
							continue
						}
						executorSessions[name].Executor.Close()
						delete(executorSessions, name)
					}
					for chatid := range activeSessions {
						messenger <- &TgGlobalMsg{
							Chatid: chatid,
							Data:   MSG_RESET_EXECUTOR,
						}
					}
					clear(activeSessions)
					close(tgcmd.Output)
				}
			case "/reload":
				{
					if err := config.Reload(); err != nil {
						tgcmd.Output <- fmt.Sprintf("Failed to reload config: %v", err)
					} else {
						setCommands(bot, tgcmd.Chatid)
						tgcmd.Output <- MSG_SUCCESS
					}
					close(tgcmd.Output)
				}
			case "/addexecutor":
				{
					name, others := util.SplitFirstAndOthers(tgcmdPayload)
					executorType, executorConfig := util.SplitFirstAndOthers(others)
					if name == "" || executorType == "" {
						tgcmd.Output <- USAGE_ADDEXECUTOR
					} else if executor.GetRegInfo(executorType) == nil {
						tgcmd.Output <- fmt.Sprintf("'%s' is NOT a valid executor type", executorType)
					} else {
						executorConfig := &config.ConfigExecutorStruct{
							Name:   name,
							Type:   executorType,
							Config: executorConfig,
						}
						err := config.AddExecutor(executorConfig)
						if err != nil {
							tgcmd.Output <- fmt.Sprintf("Failed to add executor %s: %v", name, err)
						} else {
							setCommands(bot, tgcmd.Chatid)
							tgcmd.Output <- fmt.Sprintf("Successfully added executor %s (%s)\nTo use it, send /executor_%s",
								name, executorConfig.Desc(), name)
						}
					}
					close(tgcmd.Output)
				}
			case "/delexecutor":
				{
					name := tgcmdPayload
					if name == "" {
						tgcmd.Output <- USAGE_DELEXECUTOR
					} else if name == config.DEFAULT_EXECUTOR {
						tgcmd.Output <- "default executor can NOT be deleted"
					} else if err := config.DelExecutor(name); err != nil {
						tgcmd.Output <- fmt.Sprintf("Failed to delete executor '%s': %v", name, err)
					} else {
						setCommands(bot, tgcmd.Chatid)
						tgcmd.Output <- fmt.Sprintf("Successfully deleted executor %s", name)
						for sessionName := range executorSessions {
							if sessionName == name || strings.HasPrefix(sessionName, name+"_") {
								executorSessions[sessionName].Executor.Close()
								delete(executorSessions, sessionName)
							}
						}
						for chatid, sessionName := range activeSessions {
							if strings.HasPrefix(sessionName, name+"_") {
								delete(activeSessions, chatid)
								messenger <- &TgGlobalMsg{
									Chatid: chatid,
									Data:   MSG_RESET_EXECUTOR,
								}
							}
						}
					}
					close(tgcmd.Output)
				}
			case "/setsecret":
				{
					executorName, secret := util.SplitFirstAndOthers(tgcmdPayload)
					if executorName == "" {
						tgcmd.Output <- USAGE_SETSECRET
					} else if err := config.SetExecutorSecret(executorName, secret); err != nil {
						tgcmd.Output <- fmt.Sprintf("failed to set executor '%s' secret: %v", executorName, err)
					} else {
						if secret != "" {
							messenger <- &TgGlobalMsg{
								Type:   TYPE_GLOBAL,
								Chatid: tgcmd.Chatid,
								Data:   fmt.Sprintf("Successfully set executor '%s' secret.", executorName),
							}
							bot.Delete(tgcmd.C.Message())
						} else {
							tgcmd.Output <- fmt.Sprintf("Successfully clear executor '%s' secret.", executorName)
						}
					}
					close(tgcmd.Output)
				}
			case "/addbtn":
				{
					executorName := executorSessions[activeSessions.GetActiveSessionName(tgcmd.Chatid)].Executor.Name()
					if tgcmdPayload == "" {
						tgcmd.Output <- USAGE_ADDBTN
					} else if err := config.AddExecutorButton(executorName, tgcmdPayload); err != nil {
						tgcmd.Output <- fmt.Sprintf("Failed to add executor %s button: %v", executorName, err)
					} else {
						tgcmd.Output <- MSG_SUCCESS
					}
					close(tgcmd.Output)
				}
			case "/delbtn":
				{
					executorName := executorSessions[activeSessions.GetActiveSessionName(tgcmd.Chatid)].Executor.Name()
					if tgcmdPayload == "" {
						tgcmd.Output <- USAGE_DELBTN
					} else if err := config.DelExecutorButton(executorName, tgcmdPayload); err != nil {
						tgcmd.Output <- fmt.Sprintf("Failed to delete executor %s button: %v", executorName, err)
					} else {
						tgcmd.Output <- MSG_SUCCESS
					}
					close(tgcmd.Output)
				}
			case "/clearbtn":
				{
					executorName, _ := util.SplitFirstAndOthers(tgcmdPayload)
					if executorName == "" {
						tgcmd.Output <- USAGE_CLEARBTN
					} else if err := config.ClearExecutorButtons(executorName); err != nil {
						tgcmd.Output <- fmt.Sprintf("Failed to clear executor %s buttons: %v", executorName, err)
					} else {
						tgcmd.Output <- MSG_SUCCESS
					}
					close(tgcmd.Output)
				}
			case "/executor":
				{
					sessionName := activeSessions.GetActiveSessionName(tgcmd.Chatid)
					name, extraOption := util.SplitFirstAndOthers(tgcmdPayload)
					if name == "" {
						str := fmt.Sprintf("Active executor: %s\n", executorSessions[sessionName].Executor.Name())
						sessionNames := []string{}
						for sessionName := range executorSessions {
							sessionName = strings.TrimSuffix(sessionName, fmt.Sprintf("_%d", tgcmd.Chatid))
							sessionNames = append(sessionNames, sessionName)
						}
						slices.Sort(sessionNames)
						str += fmt.Sprintf("Opened executors: %s\n", strings.Join(sessionNames, ", "))
						str += "Actions: /close , /closeall\n\n"

						str += "All executors: (*: user-defined)\n"
						for _, internalExecutor := range config.InternalExecutors {
							str += fmt.Sprintf("- %s (%s)\n/executor_%s\n", internalExecutor.Name,
								internalExecutor.Desc(), internalExecutor.Name)
						}
						for _, executor := range config.ConfigData.Executors {
							str += fmt.Sprintf("- %s (%s)\n/executor_%s\n", executor.Name, executor.Desc(), executor.Name)
						}
						str += "\nTo manage, semd /executors"
						tgcmd.Output <- str
					} else if sessionName == name || strings.HasPrefix(sessionName, name+"_") {
						tgcmd.Output <- fmt.Sprintf("Already using %s executor", name)
					} else if name == config.DEFAULT_EXECUTOR {
						delete(activeSessions, tgcmd.Chatid)
						tgcmd.Output <- MSG_RESET_EXECUTOR
					} else if executorConfig := config.GetExecutor(name); executorConfig == nil {
						tgcmd.Output <- fmt.Sprintf(MSG_EXECUTOR_NOT_FOUND_TPL, name)
					} else {
						newSessionName := executorConfig.Name
						if !executorConfig.Global {
							newSessionName += fmt.Sprintf("_%d", tgcmd.Chatid)
						}
						executorSession := executorSessions[newSessionName]
						if executorSession == nil {
							if newExecutor, err := executor.Create(executorConfig, extraOption); err != nil {
								tgcmd.Output <- fmt.Sprintf("Failed to create executor '%s': %v", executorConfig.Name, err)
							} else {
								executorSession = &TgExecutorSession{
									Executor: newExecutor,
									Chatid:   tgcmd.Chatid,
									// Ready: false, // not ready yet
								}
								executorSessions[newSessionName] = executorSession
								if newExecutor.Chan() != nil {
									go func(newExecutor executor.Executor, chatid int64) {
										for {
											if data, ok := <-newExecutor.Chan(); !ok {
												messenger <- &TgGlobalMsg{Type: TYPE_CLOSE, Executor: newExecutor.Name(), Chatid: chatid}
												break
											} else {
												messenger <- &TgGlobalMsg{Executor: newExecutor.Name(), Data: data, Chatid: chatid}
											}
										}
									}(newExecutor, tgcmd.Chatid)
								}
								go func(executorSession *TgExecutorSession) {
									if err := executorSession.Executor.Open(); err != nil {
										messenger <- &TgGlobalMsg{Executor: newExecutor.Name(), Chatid: executorSession.Chatid,
											Data: fmt.Sprintf("Failed to open executor '%s': %v", executorSession.Executor.Name(), err)}
										return
									}
									executorSession.Ready = true
								}(executorSession)
							}
						}
						if executorSession != nil {
							activeSessions[tgcmd.Chatid] = newSessionName
							tgcmd.Output <- fmt.Sprintf("Active executor changed to '%s'", executorSession.Executor.Name())
						}
					}
					close(tgcmd.Output)
				}
			case "/addcmd":
				{
					name, cmdline := util.SplitFirstAndOthers(tgcmdPayload)
					if name == "" || cmdline == "" {
						tgcmd.Output <- USAGE_ADDCMD
					} else if slices.IndexFunc(commands, func(s [4]string) bool { return s[0] == name }) != -1 {
						tgcmd.Output <- fmt.Sprintf("Cann't override internal command '%s'", name)
					} else {
						err := config.AddCmd(&config.ConfigCmdStruct{
							Name: name,
							Cmd:  cmdline,
						})
						if err == nil {
							err = setCommands(bot, tgcmd.Chatid)
						}
						if err != nil {
							tgcmd.Output <- fmt.Sprintf("Failed to add cmd %s: %v", name, err)
						} else {
							tgcmd.Output <- fmt.Sprintf("Successfully added cmd %s=%s", name, cmdline)
						}
					}
					close(tgcmd.Output)
				}
			case "/delcmd":
				{
					if tgcmdPayload == "" {
						tgcmd.Output <- USAGE_DELCMD
					} else if err := config.DelCmd(tgcmdPayload); err != nil {
						tgcmd.Output <- fmt.Sprintf("Failed to delete cmd '%s': %v", tgcmdPayload, err)
					} else {
						tgcmd.Output <- fmt.Sprintf("Successfully deleted cmd %s", tgcmdPayload)
						setCommands(bot, tgcmd.Chatid)
					}
					close(tgcmd.Output)
				}
			case "/cancel":
				{
				cancel:
					for {
						select {
						case globalCancelSign <- struct{}{}:
						default:
							break cancel
						}
					}
					sessionName := activeSessions.GetActiveSessionName(tgcmd.Chatid)
					executorSessions[sessionName].Executor.Cancel()
					close(tgcmd.Output)
				}
			case "/resetsecret":
				{
					config.ConfigData.ResetSecret()
					servicesProxy.UpdateSecret(config.ConfigData.Secret)
					tgcmd.Output <- MSG_RESETSECRET
					close(tgcmd.Output)
				}
			case "/run":
				{
					if tgcmdPayload == "" {
						tgcmd.Output <- USAGE_RUN
						close(tgcmd.Output)
					} else {
						sessionName := activeSessions.GetActiveSessionName(tgcmd.Chatid)
						command_run(tgcmd.ctx, executorSessions[sessionName], tgcmd.Output, tgcmdPayload)
					}
				}
			case "/cd":
				{
					if cwd, err := util.Cd(tgcmdPayload); err == nil {
						tgcmd.Output <- fmt.Sprintf("cd %s", cwd)
					} else {
						tgcmd.Output <- fmt.Sprintf("Failed to cd %s: %v", tgcmdPayload, err)
					}
					close(tgcmd.Output)
				}
			case "/pwd":
				{
					if cwd, err := os.Getwd(); err == nil {
						tgcmd.Output <- cwd
					}
					close(tgcmd.Output)
				}
			case "/raw":
				{
					if tgcmdPayload == "" {
						tgcmd.Output <- USAGE_RAW
						close(tgcmd.Output)
					} else {
						sessionName := activeSessions.GetActiveSessionName(tgcmd.Chatid)
						command_run(tgcmd.ctx, executorSessions[sessionName], tgcmd.Output, "^|"+tgcmdPayload)
					}
				}
			}
		case msg := <-messenger:
			{
				sessionName := msg.GetSessionName()
				if sessionName == "" {
					sessionName = activeSessions.GetActiveSessionName(msg.Chatid)
				}
				isFromActiveSession := activeSessions.IsActiveSession(msg.Chatid, sessionName)
				datas := util.Chunks(msg.Data, constants.TG_TEXT_LIMIT)
				switch msg.Type {
				case TYPE_REPLY:
					{
						if session := executorSessions[sessionName]; session != nil {
							for _, data := range datas {
								if msg.C.Get("replied") == nil {
									msg.C.Reply(data, getExecutorMenu(session.Executor.Buttons()), tele.NoPreview)
									msg.C.Set("replied", true)
								} else {
									msg.C.Send(data, getExecutorMenu(session.Executor.Buttons()), tele.NoPreview)
								}
							}
						}
					}
				case TYPE_CLOSE:
					if executorSessions[sessionName] != nil {
						delete(executorSessions, sessionName)
						bot.Send(&tele.Chat{ID: msg.Chatid}, fmt.Sprintf("Executor '%s' closed", msg.Executor), tele.NoPreview)
						if isFromActiveSession {
							delete(activeSessions, msg.Chatid)
							sessionName = activeSessions.GetActiveSessionName(msg.Chatid)
							bot.Send(&tele.Chat{ID: msg.Chatid}, MSG_RESET_EXECUTOR,
								getExecutorMenu(executorSessions[sessionName].Executor.Buttons()), tele.NoPreview)
						}
					}
				case TYPE_GLOBAL:
					{
						for _, data := range datas {
							bot.Send(&tele.Chat{ID: msg.Chatid}, data, tele.NoPreview)
						}
					}
				default:
					{
						if isFromActiveSession {
							for _, data := range datas {
								bot.Send(&tele.Chat{ID: msg.Chatid}, data,
									getExecutorMenu(executorSessions[sessionName].Executor.Buttons()), tele.NoPreview)
							}
						}
					}
				}
			}
		case <-ctx.Done():
			time.Sleep(time.Second * 1)
			bot.Stop()
			break main
		}
	}
}

// Run cmdline using session's executor and pipe it's out to output.
// Will take over output and be responsible for closing it
func command_run(ctx context.Context, session *TgExecutorSession, output chan<- string, cmdline string) {
	cmdline = strings.TrimSpace(cmdline)
	if !session.Ready {
		output <- "The executor is not ready (still openning). To stop it, send /close"
		close(output)
	} else if cmdline == "" {
		output <- `You must pass a cmdline to /run. E.g.: "/run pwd"`
		close(output)
	} else {
		isRaw := false
		if strings.HasPrefix(cmdline, "^|") {
			isRaw = true
			rawdata := cmdline[2:]
			rawdata = strings.ReplaceAll(rawdata, "\n", `\n`)
			if !strings.Contains(rawdata, `"`) {
				rawdata = `"` + rawdata + `"`
			}
			var err error
			if cmdline, err = strconv.Unquote(rawdata); err != nil {
				output <- "Invalid raw input"
				close(output)
				return
			}
		} else if args := CTRL_SEQUENCE_REGEXP.FindStringSubmatch(cmdline); args != nil {
			char := strings.ToUpper(args[CTRL_SEQUENCE_REGEXP.SubexpIndex("char")])
			// https://marcelfischer.eu/blog/2020/terminal-control-keys/
			charByte := char[0] & 0b0111111
			cmdline = string([]byte{charByte})
			isRaw = true
		}
		if cmdOut := session.Executor.Exec(ctx, cmdline, isRaw); cmdOut == nil {
			close(output)
		} else {
			go func() {
				defer close(output)
				for {
					if data, ok := <-cmdOut; !ok {
						break
					} else {
						output <- data
					}
				}
			}()
		}
	}
}
