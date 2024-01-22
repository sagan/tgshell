package telegram

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/sagan/tgshell/config"
	"github.com/sagan/tgshell/constants"
	tele "gopkg.in/telebot.v3"
)

func getExecutorMenu(buttons []string) *tele.ReplyMarkup {
	menu := &tele.ReplyMarkup{ResizeKeyboard: true}
	var menuRows []tele.Row
	hasMenu := false
	if len(buttons) > 0 {
		var menuBtns []tele.Btn
		for _, text := range buttons {
			if text != "" {
				hasMenu = true
				menuBtns = append(menuBtns, menu.Text(text))
			}
			if len(menuBtns) >= constants.TG_ROW_BUTTONS || text == "" {
				menuRows = append(menuRows, menu.Row(menuBtns...))
				menuBtns = nil
			}
		}
		if len(menuBtns) > 0 {
			menuRows = append(menuRows, menu.Row(menuBtns...))
		}
	}
	if hasMenu {
		menu.Reply(menuRows...)
	} else {
		menu.RemoveKeyboard = true
	}
	return menu
}

// Set the Chatid and Output of tgcmd, send it to commander, read Output and send back to user
func runCommand(ctx context.Context, c tele.Context, commander chan *TgCommad,
	messenger chan<- *TgGlobalMsg, command string, payload string) error {
	log.Printf("Command name=%s, payload=%s", command, payload)
	output := make(chan string, 5)
	commander <- &TgCommad{
		ctx:     ctx,
		C:       c,
		Output:  output,
		Chatid:  c.Chat().ID,
		Name:    command,
		Payload: payload,
	}
	go func(c tele.Context) {
		for {
			if data, ok := <-output; ok {
				messenger <- &TgGlobalMsg{
					Type:   TYPE_REPLY,
					Chatid: c.Chat().ID,
					C:      c,
					Data:   data,
				}
			} else {
				break
			}
		}
	}(c)
	return nil
}

// ignore messages that arrived too late, or too early (which means server time may be incorrect)
func ignoreBelatedMiddleware(seconds int64, msg string) tele.MiddlewareFunc {
	return func(next tele.HandlerFunc) tele.HandlerFunc {
		return func(ctx tele.Context) error {
			// callback is sync manner. it's message could be very old timestamp
			if ctx.Callback() == nil {
				if diff := time.Now().Unix() - ctx.Message().Time().Unix(); diff >= seconds || diff <= -1*seconds {
					return fmt.Errorf(msg)
				}
			}
			return next(ctx)
		}
	}
}

// set tg commands for global and current chat
func setCommands(bot *tele.Bot, chatid int64) error {
	var tgcommands []tele.Command
	// commands order: pinned internal cmds, user-defined cmds, executors, other internal cmds
	for _, command := range commands {
		if command[3] != "1" {
			continue
		}
		tgcommands = append(tgcommands, tele.Command{Text: command[0], Description: command[1]})
	}
	// user-defined cmds
	for _, cmd := range config.ConfigData.Cmds {
		tgcommands = append(tgcommands, tele.Command{Text: cmd.Name, Description: cmd.Cmd + " *"})
	}
	for _, internalExecutor := range config.InternalExecutors {
		tgcommands = append(tgcommands, tele.Command{Text: "/executor_" + internalExecutor.Name,
			Description: fmt.Sprintf("Executor %s (%s)", internalExecutor.Name, internalExecutor.Desc())})
	}
	for _, executor := range config.ConfigData.Executors {
		tgcommands = append(tgcommands, tele.Command{Text: "/executor_" + executor.Name,
			Description: fmt.Sprintf("Executor %s (%s)", executor.Name, executor.Desc())})
	}
	for _, command := range commands {
		if command[3] != "0" {
			continue
		}
		tgcommands = append(tgcommands, tele.Command{Text: command[0], Description: command[1]})
	}
	// see https://stackoverflow.com/questions/66053613/updating-telegram-bot-commands-in-realtime
	if chatid > 0 {
		return bot.SetCommands(tgcommands, tele.CommandScope{
			Type:   "chat",
			ChatID: chatid,
		})
	} else {
		return bot.SetCommands(tgcommands)
	}
}
