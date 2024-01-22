package ssh

import (
	"context"
	"fmt"
	"io"
	"log"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/acarl005/stripansi"
	sshlib "github.com/blacknon/go-sshlib"
	"github.com/google/shlex"
	"github.com/jessevdk/go-flags"
	"golang.org/x/crypto/ssh"

	"github.com/sagan/tgshell/config"
	"github.com/sagan/tgshell/constants"
	"github.com/sagan/tgshell/executor"
	"github.com/sagan/tgshell/util"
	"github.com/sagan/tgshell/util/sshutil"
)

const USAGE = `option: [flags] [user@]hostname [command]
Flags (Most are same as OpenSSH 'ssh' command)
* -p int : SSH server port (Default 22)
* -R string, -L string, -D string : Set up port forwarding
* -T : Disable pseudo-terminal allocation
* -i string : The identity (private key) (default to ~/.ssh/id_*)
* -o string : SSH option
* --ts-insecure : Accept unknown ssh server
E.g.: /addexecutor myssh ssh example.com
By default it only allows public-key authentication and uses OpenSSH ~/.ssh/id_* identity files.` + "\n" +
	"To use password authentication, type '/setsecret <name> <secret>' to set the password. " +
	"The public key of the ssh server will be checked against ~/.ssh/known_hosts file."

var permanentButtons = []string{"^C", "^Z", "pwd"}

// Most flags use OpenSSH "ssh" command flags. See "man ssh"
type optionsStruct struct {
	IdentityFiles   []string `short:"i"`          // -i identity_file : The default is ~/.ssh/id_dsa
	Port            int      `short:"p"`          // -p port : Port to connect to on the remote host.
	NoPty           bool     `short:"T"`          // -T : Disable pseudo-terminal allocation.
	LocalForwards   []string `short:"L"`          // -L [bind_address:]port:host:hostport...
	RemoteForwards  []string `short:"R"`          // -R [bind_address:]port:host:hostport...
	DynamicForwards []string `short:"D"`          // -D [bind_address:]port
	SshOptions      []string `short:"o"`          // -o option : only some ssh options are supported
	Insecure        bool     `long:"ts-insecure"` // Skip server public key verification
}

type Ssh struct {
	executorConfig *config.ConfigExecutorStruct
	username       string
	hostname       string
	password       string
	command        string
	options        *optionsStruct
	session        *ssh.Session
	stdin          io.WriteCloser
	history        []string
	pty            bool
	out            chan string // ssh stdout+stderr
}

// History implements executor.Executor.
func (s *Ssh) History() []string {
	return s.history
}

func init() {
	executor.Register(&executor.RegInfo{
		Name:    "ssh",
		Usage:   USAGE,
		Creator: NewExecutor,
	})
}

// Clear implements executor.Executor.
func (s *Ssh) Clear() {
	s.history = nil
}

// Buttons implements executor.Executor.
func (s *Ssh) Buttons() (buttons []string) {
	if s.command != "" {
		return
	}
	historyBtns := util.Filter(s.history, func(cmdline string) bool {
		return slices.Index(permanentButtons, cmdline) == -1
	})
	if len(historyBtns) > constants.TG_ROW_BUTTONS {
		historyBtns = historyBtns[len(historyBtns)-constants.TG_ROW_BUTTONS:]
	}
	buttons = append(buttons, historyBtns...)
	buttons = append(buttons, "")
	if s.pty {
		buttons = append(buttons, permanentButtons...)
	}
	buttons = append(buttons, executor.GlobalButtons...)
	buttons = append(buttons, s.executorConfig.Buttons...)
	return
}

func (s *Ssh) Chan() <-chan string {
	return s.out
}

// Open implements executor.Executor.
func (s *Ssh) Open() error {
	serverAliveInterval := 15
	serverAliveCountMax := 5
	connectionTimeout := 20
	for _, sshOption := range s.options.SshOptions {
		var err error
		args := strings.Split(sshOption, "=")
		if len(args) < 2 || args[0] == "" || args[1] == "" {
			err = fmt.Errorf("invalid ssh option '%s'", args[0])
		} else {
			switch args[0] {
			case "ServerAliveInterval":
				serverAliveInterval, err = strconv.Atoi(args[1])
			case "ServerAliveCountMax":
				serverAliveCountMax, err = strconv.Atoi(args[1])
			case "ConnectTimeout":
				connectionTimeout, err = strconv.Atoi(args[1])
			default:
				err = fmt.Errorf("unsupported ssh option '%s'", args[0])
			}
		}
		if err != nil {
			close(s.out)
			return fmt.Errorf("invalid -o option: %v", err)
		}
	}
	con := &sshlib.Connect{
		ForwardX11:      false,
		ForwardAgent:    false,
		CheckKnownHosts: !s.options.Insecure,
		ConnectTimeout:  connectionTimeout,
	}
	err := sshutil.CreateSshClient(con, s.hostname, fmt.Sprint(s.options.Port),
		s.username, s.password, s.options.IdentityFiles)
	if err != nil {
		close(s.out)
		return fmt.Errorf("failed to create ssh client: %v", err)
	}

	for _, localForward := range s.options.LocalForwards {
		args := strings.Split(localForward, ":")
		if len(args) == 1 {
			err = fmt.Errorf("invalid local_forward '%s'", localForward)
		} else if len(args) == 2 {
			// -L local_socket:remote_socket
			// 80:80
			err = con.TCPLocalForward(args[0], args[1])
		} else if len(args) == 3 {
			// 80:1.2.3.4:80
			err = con.TCPLocalForward(args[0], strings.Join(args[1:], ":"))
		} else {
			// 127.0.0.1:80:1.2.3.4:80
			err = con.TCPLocalForward(strings.Join(args[:2], ":"), strings.Join(args[2:], ":"))
		}
		if err != nil {
			break
		}
	}
	if err == nil {
		for _, remoteForward := range s.options.RemoteForwards {
			args := strings.Split(remoteForward, ":")
			if len(args) == 1 {
				// -R [bind_address:]port
				// 80
				err = con.TCPReverseDynamicForward("0.0.0.0", args[0])
			} else if len(args) == 2 {
				// -R [bind_address:]port
				// 127.0.0.1:80
				err = con.TCPReverseDynamicForward("0.0.0.0", args[1])
			} else if len(args) == 3 {
				// -R [bind_address:]port:host:hostport
				// 888:1.2.3.4:80
				err = con.TCPRemoteForward(strings.Join(args[1:], ":"), args[0])
			} else {
				// -R [bind_address:]port:host:hostport
				// 0.0.0.0:80:0.0.0.0:80
				err = con.TCPRemoteForward(strings.Join(args[2:], ":"), strings.Join(args[:2], ":"))
			}
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		for _, dynamicForward := range s.options.DynamicForwards {
			args := strings.Split(dynamicForward, ":")
			if len(args) == 1 {
				// 80
				err = con.TCPDynamicForward("0.0.0.0", args[0])
			} else if len(args) == 2 {
				// localhost:80
				err = con.TCPDynamicForward(args[0], args[1])
			} else {
				err = fmt.Errorf("invalid dynamic_forward '%s'", dynamicForward)
			}
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		close(s.out)
		return fmt.Errorf("failed to create port forward: %v", err)
	}

	session, err := con.CreateSession()
	if err != nil {
		close(s.out)
		return fmt.Errorf("failed to create ssh session: %v", err)
	}
	s.stdin, err = session.StdinPipe()
	if err != nil {
		session.Close()
		close(s.out)
		return fmt.Errorf("failed to pipe stdin: %v", err)
	}
	session.Stdout = s
	session.Stderr = s

	if !s.options.NoPty {
		modes := ssh.TerminalModes{
			ssh.ECHO:          0,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}
		if err := session.RequestPty("xterm", constants.PTY_H, constants.PTY_W, modes); err != nil {
			s.out <- fmt.Sprintf("Warning: failed to request pty: %v\n", err)
		} else {
			s.pty = true
		}
	}

	s.session = session
	go func(session *ssh.Session, command string, aliveInterval int, aliveMax int) {
		defer close(s.out)
		if command != "" {
			err := session.Run(command)
			log.Printf("ssh session run %s, err=%v", command, err)
			session.Close()
			return
		}
		err := session.Shell()
		log.Printf("ssh session start, err=%v", err)
		if err == nil && aliveInterval > 0 {
			go func(session *ssh.Session) {
				i := 0
				for {
					_, err := session.SendRequest("keepalive", true, nil)
					if err == nil {
						i = 0
					} else {
						i += 1
					}
					if aliveMax <= i {
						session.Close()
						return
					}
					time.Sleep(time.Duration(aliveInterval) * time.Second)
				}
			}(session)
		}
		err = session.Wait()
		log.Printf("ssh session exit, err=%v", err)
	}(s.session, s.command, serverAliveInterval, serverAliveCountMax)
	return nil
}

func (s *Ssh) Cancel() {
	if s.pty {
		// 0x03 : Ctrl + C
		s.exec(context.Background(), string([]byte{3}))
	}
}

func (s *Ssh) Close() {
	s.session.Close()
}

func (s *Ssh) Exec(ctx context.Context, cmdline string, isRaw bool) (output chan string) {
	if !isRaw {
		s.history = util.AppendUniqueCapSlice(s.history, constants.MAX_HISTORY, cmdline)
		cmdline += "\n"
	}
	s.exec(ctx, cmdline)
	return
}

func (s *Ssh) exec(ctx context.Context, cmdline string) {
	s.stdin.Write([]byte(cmdline))
}

func (s *Ssh) Name() string {
	return s.executorConfig.Name
}

// ssh stdout+stderr io.Writer
func (s *Ssh) Write(p []byte) (n int, err error) {
	text := string(p)
	// If request pty, the output will be in "escape sequence" format.
	// See https://en.wikipedia.org/wiki/ANSI_escape_code
	if s.pty {
		text = stripansi.Strip(text)
	}
	s.out <- text
	return len(p), nil
}

func NewExecutor(executorConfig *config.ConfigExecutorStruct, extraOption string) (executor.Executor, error) {
	executorConfigStr := executorConfig.Config
	if executorConfigStr != "" && extraOption != "" {
		executorConfigStr += " "
	}
	executorConfigStr += extraOption
	args, err := shlex.Split(executorConfigStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config as tokens: %v", err)
	}
	options := &optionsStruct{}
	args, err = flags.NewParser(options, flags.IgnoreUnknown).ParseArgs(args)
	if err != nil || len(args) == 0 || len(args) > 2 {
		return nil, fmt.Errorf("invalid config: flags error=%v, args=%v", err, args)
	}
	command := ""
	username := ""
	hostname := ""
	destination := args[0]
	if len(args) > 1 {
		command = args[1]
	}
	if i := strings.Index(destination, "@"); i != -1 {
		username = destination[:i]
		hostname = destination[i+1:]
	} else {
		hostname = destination
		if u, err := user.Current(); err != nil {
			username = "root"
		} else {
			username = u.Username
		}
	}
	if i := strings.Index(hostname, ":"); i != -1 {
		options.Port, err = strconv.Atoi(hostname[i+1:])
		if err != nil || options.Port <= 0 || options.Port > 65535 {
			return nil, fmt.Errorf("invalid port '%s'", hostname[i+1:])
		}
		hostname = hostname[:i]
	} else if options.Port == 0 {
		options.Port = 22
	}
	if username == "" || hostname == "" || options.Port <= 0 || options.Port > 65535 {
		return nil, fmt.Errorf("username ('%s'), host ('%s') or port ('%d') is empty or invalid",
			username, hostname, options.Port)
	}
	log.Printf("Ssh host=%s,username=%s,options=%v", hostname, username, options)

	return &Ssh{
		executorConfig: executorConfig,
		username:       username,
		password:       executorConfig.Secret,
		hostname:       hostname,
		command:        command,
		options:        options,
		session:        nil,
		out:            make(chan string, 1),
	}, nil
}

var _ executor.Executor = (*Ssh)(nil)
