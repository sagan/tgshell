package shell

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/acarl005/stripansi"
	"github.com/creack/pty"
	"github.com/google/shlex"
	"github.com/jessevdk/go-flags"

	"github.com/sagan/tgshell/config"
	"github.com/sagan/tgshell/constants"
	"github.com/sagan/tgshell/executor"
	"github.com/sagan/tgshell/util"
)

const USAGE = `option: shell [flags] [interpreter]
[interpreter] is the cmdline interpreter, default to 'cmd' on windows, SHELL env value on other platforms.
Flags:
* --ts-oneshot : Create new process for every cmdline, passing cmdline to interpreter as single arg
* --ts-shell : Force treat interpreter as a system shell
* --ts-parse : Valid in oneshot mode. Split cmdline to tokens instead of as single arg when executing it
E.g.:
/addexecutor ps shell powershell
/addexecutor python shell python3`

var known_shells = []string{"cmd", "cmd.exe", "powershell", "powershell.exe",
	"bash", "sh", "ash", "fish", "ksh", "tcsh", "zsh", "dash"}

// These interpreters are not shell, but also have an (default) interactive (REPL) mode,
// and have a "-c" flag to accept first arg as cmdline
var known_interpreters = []string{"python", "python.exe", "python3", "python3.exe"}
var ptyButtons = []string{"^C", "^Z"}
var shellButtons = []string{"pwd", "/files"}

type optionsStruct struct {
	// Only valid in non-pty mode:
	// if true, split cmdline to tokens and use them as args when Exec it using executor.
	// otherwise parse the whole cmdline as single arg to executor
	ParseArgs bool `long:"ts-parse"`
	// force treat it as shell
	ForceShell bool `long:"ts-shell"`
	// Oneshot mode: no pty, create a new process each time exeuting a cmdline
	Oneshot bool `long:"ts-oneshot"`
}

type Shell struct {
	executorConfig *config.ConfigExecutorStruct
	TimeoutSecond  int
	executor       string
	executorArgs   []string
	cancelSignal   chan struct{}
	history        []string
	isShell        bool // if isShell, add some common buttons like "cd", "pwd"
	output         chan string
	pty            bool
	ptmx           *os.File
	options        *optionsStruct
}

// History implements executor.Executor.
func (s *Shell) History() []string {
	return s.history
}

func init() {
	executor.Register(&executor.RegInfo{
		Name:    "shell",
		Usage:   USAGE,
		Creator: NewExecutor,
	})
}

// Clear implements executor.Executor.
func (s *Shell) Clear() {
	s.history = nil
}

// Buttons implements executor.Executor.
func (s *Shell) Buttons() (buttons []string) {
	historyBtns := util.Filter(s.history, func(cmdline string) bool {
		return (!s.pty || slices.Index(ptyButtons, cmdline) == -1) &&
			(!s.isShell || slices.Index(shellButtons, cmdline) == -1)
	})
	if len(historyBtns) > constants.TG_ROW_BUTTONS {
		historyBtns = historyBtns[len(historyBtns)-constants.TG_ROW_BUTTONS:]
	}
	buttons = append(buttons, historyBtns...)
	buttons = append(buttons, "")
	if s.pty {
		buttons = append(buttons, ptyButtons...)
	}
	if s.isShell {
		buttons = append(buttons, shellButtons...)
	}
	buttons = append(buttons, executor.GlobalButtons...)
	buttons = append(buttons, s.executorConfig.Buttons...)
	return
}

// Chan implements executor.Executor.
func (s *Shell) Chan() <-chan string {
	return s.output
}

// Open implements executor.Executor.
func (s *Shell) Open() error {
	if s.pty {
		var err error
		c := exec.Command(s.executor, s.executorArgs...)
		if s.ptmx, err = pty.Start(c); err != nil {
			close(s.output)
			return fmt.Errorf("failed to create pty: %v", err)
		}
		if err = pty.Setsize(s.ptmx, &pty.Winsize{Rows: constants.PTY_H, Cols: constants.PTY_W}); err != nil {
			close(s.output)
			return fmt.Errorf("failed to set pty size: %v", err)
		}
		go func() {
			defer close(s.output)
			defer s.ptmx.Close()
			buf := make([]byte, 10240)
			for {
				i, err := s.ptmx.Read(buf)
				if err != nil {
					break
				}
				s.output <- stripansi.Strip(string(buf[:i]))
			}
		}()
	}
	return nil
}

func NewExecutor(executorConfig *config.ConfigExecutorStruct, extraOption string) (executor.Executor, error) {
	var executor string
	var executorArgs []string
	var err error
	executorConfigStr := executorConfig.Config
	if executorConfigStr != "" && extraOption != "" {
		executorConfigStr += " "
	}
	executorConfigStr += extraOption
	if executorConfig.Config != "" {
		if executorArgs, err = shlex.Split(executorConfigStr); err != nil {
			return nil, fmt.Errorf("failed to parse executor as tokens: %s", err)
		}
	}
	options := &optionsStruct{}
	executorArgs, err = flags.NewParser(options, flags.IgnoreUnknown).ParseArgs(executorArgs)
	if err != nil {
		return nil, fmt.Errorf("invalid config: flags error=%v, args=%v", err, executorArgs)
	}
	if len(executorArgs) > 0 {
		executor, executorArgs = executorArgs[0], executorArgs[1:]
	} else {
		if runtime.GOOS == "windows" {
			executor = "cmd"
		} else {
			executor = os.Getenv("SHELL")
			if executor == "" {
				executor = "/bin/bash"
			}
		}
	}
	isKnownShell := false
	isKnownInterpreter := false
	if slices.Index(known_shells, path.Base(executor)) != -1 {
		isKnownShell = true
	} else if slices.Index(known_interpreters, path.Base(executor)) != -1 {
		isKnownInterpreter = true
	}
	if (isKnownShell || isKnownInterpreter) && options.Oneshot {
		// Known problem: it may NOT work for bash on Windows, or, powershell on Linux
		if isKnownShell && runtime.GOOS == "windows" {
			if slices.Index(executorArgs, "/C") == -1 {
				executorArgs = append(executorArgs, "/C")
			}
		} else {
			if slices.Index(executorArgs, "-c") == -1 {
				executorArgs = append(executorArgs, "-c")
			}
		}
	}
	log.Printf("executor: %s, args: %v, options: %v", executor, executorArgs, options)
	var output chan string
	if !options.Oneshot {
		output = make(chan string, 5)
	}
	return &Shell{
		executorConfig: executorConfig,
		TimeoutSecond:  30,
		options:        options,
		executor:       executor,
		executorArgs:   executorArgs,
		output:         output,
		pty:            !options.Oneshot,
		isShell:        options.ForceShell || isKnownShell,
		cancelSignal:   make(chan struct{}),
	}, nil
}

func (s *Shell) Exec(ctx context.Context, cmdline string, isRaw bool) (output chan string) {
	if !isRaw {
		s.history = util.AppendUniqueCapSlice(s.history, constants.MAX_HISTORY, cmdline)
		if !s.pty {
			if data, handled := s.runBuiltin(ctx, cmdline); handled {
				output = make(chan string, 1)
				output <- data
				close(output)
				return
			}
		} else {
			cmdline += "\n"
		}
	}
	return s.exec(ctx, cmdline, true)
}

func (s *Shell) exec(ctx context.Context, cmdline string, outputMeta bool) (output chan string) {
	if s.pty {
		s.ptmx.Write([]byte(cmdline))
		return
	}
	cmdline = strings.TrimSpace(cmdline)
	output = make(chan string)

	go func(shell *Shell, cmdline string, output chan<- string) {
		ctx, timeoutCancel := context.WithTimeout(ctx, time.Second*time.Duration(s.TimeoutSecond))
		ctx, cancel := context.WithCancel(ctx)
		defer timeoutCancel()
		defer cancel()
		defer close(output)
		var args []string
		args = append(args, shell.executorArgs...)
		if !s.options.ParseArgs {
			args = append(args, cmdline)
		} else {
			if cmdargs, err := shlex.Split(cmdline); err != nil {
				args = append(args, cmdline)
			} else {
				args = append(args, cmdargs...)
			}
		}
		cmd := exec.CommandContext(ctx, shell.executor, args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			if outputMeta {
				output <- fmt.Sprintf("Failed to pipe process output, error=%v", err)
			}
			return
		}
		cmd.Stderr = cmd.Stdout
		cmd.Start()
		go func(signal <-chan struct{}) {
			select {
			case <-signal:
				cancel()
			case <-ctx.Done():
			}
		}(shell.cancelSignal)
		buf := make([]byte, 10240)
		for {
			i, err := stdout.Read(buf)
			if err != nil {
				break
			}
			output <- string(buf[:i])
		}
		err = cmd.Wait()
		if outputMeta {
			output <- fmt.Sprintf("Process '%s' exitted, error=%v", cmdline, err)
		}
	}(s, cmdline, output)

	return
}

func (s *Shell) runBuiltin(ctx context.Context, cmdline string) (output string, handled bool) {
	if i := strings.Index(cmdline, ";"); i != -1 && i < len(cmdline)-1 {
		return
	}
	builtinName, builtinParameters := util.SplitFirstAndOthers(cmdline)
	if builtinName == "cd" {
		if cwd, err := util.Cd(builtinParameters); err == nil {
			output = fmt.Sprintf("cd %s", cwd)
		} else {
			output = fmt.Sprintf("Failed to cd %s: %v", builtinParameters, err)
		}
		handled = true
	} else if builtinName == "pwd" {
		output, _ = os.Getwd()
		handled = true
	}
	return
}

func (s *Shell) Name() string {
	return s.executorConfig.Name
}

func (s *Shell) Cancel() {
	if s.pty {
		s.exec(context.Background(), string([]byte{3}), false)
		return
	}
cancel:
	for {
		select {
		case s.cancelSignal <- struct{}{}:
		default:
			break cancel
		}
	}
}

func (s *Shell) Close() {
	if s.pty {
		s.ptmx.Close()
	}
}

var _ executor.Executor = (*Shell)(nil)
