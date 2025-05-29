package config

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/sagan/tgshell/constants"
	"github.com/spf13/viper"
)

type ConfigCmdStruct struct {
	Name string
	Cmd  string
}

type ConfigExecutorStruct struct {
	Name     string
	Type     string
	Config   string
	Secret   string
	Buttons  []string // cmdline shortcuts
	Comment  string
	Internal bool
	Global   bool // one instance can be shared accross all users
}

// Securely publish intranet (e.g.: 127.0.0.1) service to tg user
type ConfigServiceStruct struct {
	Name     string
	Hostname string
	Backend  string
	Headers  [][2]string
	Comment  string
}

type ConfigStruct struct {
	ShellExecutor        string // by default, use "cmd /C" on windows, "/bin/bash -c" on other platforms.
	ShellExecutorButtons []string
	TelegramToken        string // tg bot token
	Cmds                 []*ConfigCmdStruct
	Executors            []*ConfigExecutorStruct
	Services             []*ConfigServiceStruct
	Whitelist            []int64
	// should be same as server's OpenSSH HostKeyAlgorithms. Default values can be found using `man ssh_config`.
	// Note it's not same as `ssh -Q HostKeyAlgorithms`,
	// which outputs all available algorithms, not actual used algorithms.
	SshHostKeyAlgorithms []string
	ServicesPort         int
	ServicesAddr         string // 0.0.0.0
	ServicesPublicPort   int
	ServicesHttps        bool
	Secret               string
}

const DEFAULT_EXECUTOR = "shell"
const PTY_EXECUTOR = "pty"

// Default values of recent OpenSSH
var defaultSshHostKeyAlgorithms = []string{
	"ecdsa-sha2-nistp256-cert-v01@openssh.com", "ecdsa-sha2-nistp384-cert-v01@openssh.com", "ecdsa-sha2-nistp521-cert-v01@openssh.com",
	"sk-ecdsa-sha2-nistp256-cert-v01@openssh.com", "ssh-ed25519-cert-v01@openssh.com", "sk-ssh-ed25519-cert-v01@openssh.com",
	"rsa-sha2-512-cert-v01@openssh.com", "rsa-sha2-256-cert-v01@openssh.com", "ssh-rsa-cert-v01@openssh.com", "ecdsa-sha2-nistp256",
	"ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521", "sk-ecdsa-sha2-nistp256@openssh.com", "ssh-ed25519", "sk-ssh-ed25519@openssh.com",
	"rsa-sha2-512", "rsa-sha2-256", "ssh-rsa",
}

var InternalExecutors = []*ConfigExecutorStruct{
	{
		Name:     DEFAULT_EXECUTOR,
		Type:     "shell",
		Config:   "--ts-oneshot",
		Internal: true,
		Global:   true,
		Comment:  "The default executor. Exec cmdline using system shell. It's always open and can not be closed.",
	},
	{
		Name:     PTY_EXECUTOR,
		Type:     "shell",
		Internal: true,
		Comment:  "The system shell pty (pseudo terminal) executor.",
	},
}

var DefaultExecutorConfig = InternalExecutors[0]
var PtyExecutorConfig = InternalExecutors[1]
var executorConfigMap = map[string]*ConfigExecutorStruct{}
var cmdConfigMap = map[string]*ConfigCmdStruct{}

//go:embed default
var emptyfs embed.FS

var (
	ConfigData *ConfigStruct
	ConfigPath string
)

// get a descriptive string of it's type and config combined
func (ecs *ConfigExecutorStruct) Desc() (desc string) {
	desc = ecs.Type
	if ecs.Config != "" {
		desc += " " + ecs.Config
	}
	if !ecs.Internal {
		desc += " *"
	}
	return
}

func (sc *ConfigServiceStruct) GetName() string {
	if sc.Name != "" {
		return sc.Name
	}
	return sc.Hostname
}

func (cs *ConfigStruct) ResetSecret() error {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return fmt.Errorf("failed to generate secret: %v", err)
	} else {
		cs.Secret = base64.StdEncoding.EncodeToString(secret)
		viper.Set("secret", cs.Secret)
		return viper.WriteConfig()
	}
}

func (cs *ConfigStruct) sideeffect() {
	clear(cmdConfigMap)
	for _, c := range cs.Cmds {
		cmdConfigMap[c.Name] = c
	}
	clear(executorConfigMap)
	for _, c := range InternalExecutors {
		executorConfigMap[c.Name] = c
	}
	for _, c := range cs.Executors {
		executorConfigMap[c.Name] = c
	}
}

func InitConfig() error {
	if ConfigPath == "" {
		return fmt.Errorf("ConfigPath can not be empty")
	}
	log.Printf("Read config from %s", ConfigPath)
	if err := os.MkdirAll(ConfigPath, 0600); err != nil {
		return err
	}
	files, err := emptyfs.ReadDir("default")
	if err != nil {
		return err
	}
	for _, file := range files {
		if !file.Type().IsRegular() {
			continue
		}
		filename := path.Join(ConfigPath, file.Name())
		if _, err := os.Stat(filename); !os.IsNotExist(err) {
			continue
		}
		file, err := emptyfs.Open(path.Join("default", file.Name()))
		if err != nil {
			return err
		}
		data, err := io.ReadAll(file)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filename, data, 0600); err != nil {
			return err
		}
	}

	viper.AddConfigPath(ConfigPath)
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	if err := viper.ReadInConfig(); err != nil {
		return err
	}
	if err := viper.Unmarshal(&ConfigData); err != nil {
		return err
	}
	if ConfigData.TelegramToken == "" {
		return fmt.Errorf("telegram_token must be configed")
	}
	if len(ConfigData.Whitelist) == 0 {
		return fmt.Errorf("whitelist must be configed")
	}
	for _, uid := range ConfigData.Whitelist {
		if uid == 0 {
			return fmt.Errorf("whitelist uid can not be 0")
		}
	}
	if ConfigData.Secret == "" {
		if err := ConfigData.ResetSecret(); err != nil {
			return fmt.Errorf("failed to reset empty secret: %v", err)
		}
		log.Printf("Config file found empty secret, set to a random value")
	}
	if ConfigData.ServicesPort == 0 {
		ConfigData.ServicesPort = constants.DEFAULT_SERVICES_PORT
	}
	if ConfigData.ServicesAddr == "" {
		ConfigData.ServicesAddr = constants.DEFAULT_SERVICES_ADDR
	}
	if len(ConfigData.SshHostKeyAlgorithms) == 0 {
		ConfigData.SshHostKeyAlgorithms = defaultSshHostKeyAlgorithms
	}
	ConfigData.sideeffect()
	DefaultExecutorConfig.Buttons = ConfigData.ShellExecutorButtons
	PtyExecutorConfig.Buttons = ConfigData.ShellExecutorButtons
	return nil
}

func GetCmd(name string) *ConfigCmdStruct {
	return cmdConfigMap[name]
}

func GetExecutor(name string) *ConfigExecutorStruct {
	return executorConfigMap[name]
}

func AddCmd(cmd *ConfigCmdStruct) error {
	if GetCmd(cmd.Name) != nil {
		return fmt.Errorf("'%s' cmd already exists", cmd.Name)
	}
	ConfigData.Cmds = append(ConfigData.Cmds, cmd)
	sort.SliceStable(ConfigData.Cmds, func(i, j int) bool {
		if ConfigData.Cmds[i].Name != ConfigData.Cmds[j].Name {
			return ConfigData.Cmds[i].Name < ConfigData.Cmds[j].Name
		}
		return ConfigData.Cmds[i].Cmd < ConfigData.Cmds[j].Cmd
	})
	ConfigData.sideeffect()
	viper.Set("cmds", ConfigData.Cmds)
	return viper.WriteConfig()
}

// Reload config from config file
func Reload() error {
	if ConfigPath == "" {
		return fmt.Errorf("ConfigPath can not be empty")
	}
	log.Printf("Reload config from %s", ConfigPath)
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config: %v", err)
	}
	if err := viper.Unmarshal(&ConfigData); err != nil {
		return fmt.Errorf("failed to unmarshal config: %v", err)
	}
	ConfigData.sideeffect()
	DefaultExecutorConfig.Buttons = ConfigData.ShellExecutorButtons
	PtyExecutorConfig.Buttons = ConfigData.ShellExecutorButtons
	return nil
}

func AddExecutor(executor *ConfigExecutorStruct) error {
	if GetExecutor(executor.Name) != nil {
		return fmt.Errorf("'%s' executor already exists", executor.Name)
	}
	ConfigData.Executors = append(ConfigData.Executors, executor)
	sort.SliceStable(ConfigData.Executors, func(i, j int) bool {
		if ConfigData.Executors[i].Name != ConfigData.Executors[j].Name {
			return ConfigData.Executors[i].Name < ConfigData.Executors[j].Name
		}
		return ConfigData.Executors[i].Config < ConfigData.Executors[j].Config
	})
	ConfigData.sideeffect()
	viper.Set("executors", ConfigData.Executors)
	return viper.WriteConfig()
}

func DelCmd(name string) error {
	if GetCmd(name) == nil {
		return fmt.Errorf("'%s' cmd does NOT exist", name)
	}
	var cmds []*ConfigCmdStruct
	for _, cmd := range ConfigData.Cmds {
		if cmd.Name != name {
			cmds = append(cmds, cmd)
		}
	}
	ConfigData.Cmds = cmds
	ConfigData.sideeffect()
	viper.Set("cmds", ConfigData.Cmds)
	return viper.WriteConfig()
}

func DelExecutor(name string) error {
	if executor := GetExecutor(name); executor == nil {
		return fmt.Errorf("'%s' executor does NOT exist", name)
	} else if executor.Internal {
		return fmt.Errorf("'%s' is a internal executor and can NOT be deleted", name)
	}
	var executors []*ConfigExecutorStruct
	for _, executor := range ConfigData.Executors {
		if executor.Name != name {
			executors = append(executors, executor)
		}
	}
	ConfigData.Executors = executors
	ConfigData.sideeffect()
	viper.Set("executors", ConfigData.Executors)
	return viper.WriteConfig()
}

// Set or update the secret of executor
func SetExecutorSecret(name string, secret string) error {
	if executor := GetExecutor(name); executor == nil {
		return fmt.Errorf("'%s' executor does NOT exist", name)
	} else if executor.Internal {
		return fmt.Errorf("'%s' executor does NOT support secret because it's an internal executor)", name)
	}
	var executors []*ConfigExecutorStruct
	for _, executor := range ConfigData.Executors {
		if executor.Name == name {
			executor.Secret = secret
		}
		executors = append(executors, executor)
	}
	ConfigData.Executors = executors
	viper.Set("executors", ConfigData.Executors)
	return viper.WriteConfig()
}

func AddExecutorButton(name string, button string) error {
	if name == DEFAULT_EXECUTOR || name == PTY_EXECUTOR {
		if slices.Index(ConfigData.ShellExecutorButtons, button) == -1 {
			ConfigData.ShellExecutorButtons = append(ConfigData.ShellExecutorButtons, button)
			DefaultExecutorConfig.Buttons = ConfigData.ShellExecutorButtons
			PtyExecutorConfig.Buttons = ConfigData.ShellExecutorButtons
		}
		viper.Set("shellexecutorbuttons", ConfigData.ShellExecutorButtons)
	} else {
		var executors []*ConfigExecutorStruct
		for _, executor := range ConfigData.Executors {
			if executor.Name == name && slices.Index(executor.Buttons, button) == -1 {
				buttons := append(executor.Buttons, button)
				slices.Sort(buttons)
				executor.Buttons = buttons
			}
			executors = append(executors, executor)
		}
		ConfigData.Executors = executors
		viper.Set("executors", ConfigData.Executors)
	}
	return viper.WriteConfig()
}

func DelExecutorButton(name string, payload string) error {
	index, indexError := strconv.Atoi(payload)
	if indexError == nil && index < 0 {
		return fmt.Errorf("invalid payload '%s'", payload)
	}
	if name == DEFAULT_EXECUTOR || name == PTY_EXECUTOR {
		if indexError != nil {
			index = slices.IndexFunc(ConfigData.ShellExecutorButtons, func(s string) bool {
				return s == payload || strings.HasPrefix(s, payload+" ")
			})
		}
		if index >= 0 && len(ConfigData.ShellExecutorButtons) > index {
			buttons := []string{}
			buttons = append(buttons, ConfigData.ShellExecutorButtons[:index]...)
			buttons = append(buttons, ConfigData.ShellExecutorButtons[index+1:]...)
			ConfigData.ShellExecutorButtons = buttons
		}
		DefaultExecutorConfig.Buttons = ConfigData.ShellExecutorButtons
		PtyExecutorConfig.Buttons = ConfigData.ShellExecutorButtons
		viper.Set("shellexecutorbuttons", ConfigData.ShellExecutorButtons)
	} else {
		var executors []*ConfigExecutorStruct
		for _, executor := range ConfigData.Executors {
			if executor.Name == name {
				if indexError != nil {
					index = slices.IndexFunc(executor.Buttons, func(s string) bool {
						return s == payload || strings.HasPrefix(s, payload+" ")
					})
				}
				if index >= 0 && len(executor.Buttons) > index {
					buttons := []string{}
					buttons = append(buttons, executor.Buttons[:index]...)
					buttons = append(buttons, executor.Buttons[index+1:]...)
					executor.Buttons = buttons
				}
			}
			executors = append(executors, executor)
		}
		ConfigData.Executors = executors
		viper.Set("executors", ConfigData.Executors)
	}
	return viper.WriteConfig()
}

func GetExecutorButtons(name string) (buttons []string) {
	if name == DEFAULT_EXECUTOR || name == PTY_EXECUTOR {
		buttons = ConfigData.ShellExecutorButtons
	} else if executor := GetExecutor(name); executor != nil {
		buttons = executor.Buttons
	}
	return
}

func ClearExecutorButtons(name string) error {
	if name == DEFAULT_EXECUTOR || name == PTY_EXECUTOR {
		ConfigData.ShellExecutorButtons = nil
		DefaultExecutorConfig.Buttons = nil
		PtyExecutorConfig.Buttons = nil
		viper.Set("shellexecutorbuttons", ConfigData.ShellExecutorButtons)
	} else {
		var executors []*ConfigExecutorStruct
		for _, executor := range ConfigData.Executors {
			if executor.Name == name {
				executor.Buttons = nil
			}
			executors = append(executors, executor)
		}
		ConfigData.Executors = executors
		viper.Set("executors", ConfigData.Executors)
	}
	return viper.WriteConfig()
}
