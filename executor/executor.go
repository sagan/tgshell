package executor

import (
	"context"
	"fmt"

	"github.com/sagan/tgshell/config"
)

// Flow: New() -> Open() -> | Exec() / Cancel() / Clear() (support parallel) | -> Close().
type Executor interface {
	// Available after New(). If return nil channel, should use seperated command output channel. Always return same.
	// Will automatically close when executor is closed
	Chan() <-chan string
	Open() error // if return non-nil error, the executor will have been closed. May take some time to return
	Name() string
	History() []string // return cmdline history, last is the latest cmdline
	Buttons() []string // executor level buttons
	Exec(ctx context.Context, cmdline string, isRaw bool) (output chan string)
	Cancel() // cancel currently running commands
	Clear()
	Close() // Chan() may close async after Close() return.
}

type RegInfo struct {
	Name    string
	Usage   string
	Creator func(executorConfig *config.ConfigExecutorStruct, extraOption string) (Executor, error)
}

var (
	Registry      = []*RegInfo{}
	GlobalButtons = []string{"/history"}
)

func Register(regInfo *RegInfo) {
	Registry = append(Registry, regInfo)
}

// The created Executor should hold the executorConfig pointer and ALWAYS use the latest data in it, if possible.
func Create(executorConfig *config.ConfigExecutorStruct, extraOption string) (Executor, error) {
	for _, reg := range Registry {
		if reg.Name == executorConfig.Type {
			return reg.Creator(executorConfig, extraOption)
		}
	}
	return nil, fmt.Errorf("no executor of type '%s' found", executorConfig.Type)
}

func GetRegInfo(executorType string) *RegInfo {
	for _, reg := range Registry {
		if reg.Name == executorType {
			return reg
		}
	}
	return nil
}
