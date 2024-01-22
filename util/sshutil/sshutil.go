package sshutil

import (
	"fmt"
	"net"
	"os"
	"path"
	"time"

	"github.com/blacknon/go-sshlib"
	"github.com/sagan/tgshell/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/net/proxy"
)

// OpenSSH default order
var defaultSshIdentityFiles = []string{"~/.ssh/id_rsa", "~/.ssh/id_dsa", "~/.ssh/id_ecdsa",
	"~/.ssh/id_ecdsa_sk", "~/.ssh/id_ed25519", "~/.ssh/id_ed25519_sk", "~/.ssh/id_xmss"}

// A modified version of func (*sshlib.Connect) CreateClient.
// When checking ssh server public key, return error if encounter an dismatch or unknown host,
// instead of asking user to confirm from tty.
func CreateSshClient(c *sshlib.Connect, host, port, user, pass string, identityFiles []string) (err error) {
	var authMethods []ssh.AuthMethod
	if len(identityFiles) == 0 {
		identityFiles = defaultSshIdentityFiles
	}
	for _, identityFile := range identityFiles {
		if publickeyAuthMethod, err := sshlib.CreateAuthMethodPublicKey(identityFile, ""); err == nil {
			authMethods = append(authMethods, publickeyAuthMethod)
		}
	}
	if pass != "" {
		authMethods = append(authMethods, sshlib.CreateAuthMethodPassword(pass))
	}
	if len(authMethods) == 0 {
		return fmt.Errorf("no available auth method")
	}

	uri := net.JoinHostPort(host, port)

	timeout := 20
	if c.ConnectTimeout > 0 {
		timeout = c.ConnectTimeout
	}

	// Create new ssh.ClientConfig{}
	sshConfig := &ssh.ClientConfig{
		User:              user,
		Auth:              authMethods,
		Timeout:           time.Duration(timeout) * time.Second,
		HostKeyAlgorithms: config.ConfigData.SshHostKeyAlgorithms,
	}

	if len(c.KnownHostsFiles) == 0 {
		if userHomeDir, err := os.UserHomeDir(); err == nil {
			c.KnownHostsFiles = append(c.KnownHostsFiles, path.Join(userHomeDir, ".ssh/known_hosts"))
		}
	}
	sshConfig.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) (err error) {
		// check count KnownHostsFiles
		if c.CheckKnownHosts && len(c.KnownHostsFiles) == 0 {
			return fmt.Errorf("there is no knownhosts file")
		}
		knownHostsFiles := c.KnownHostsFiles
		// get hostKeyCallback
		hostKeyCallback, err := knownhosts.New(knownHostsFiles...)
		if err != nil {
			return
		}
		// check hostkey
		err = hostKeyCallback(hostname, remote, key)
		if err == nil {
			return nil
		}
		// check error
		keyErr, ok := err.(*knownhosts.KeyError)
		if !ok || len(keyErr.Want) > 0 {
			return fmt.Errorf("host key (%s:%s) do not match with entry in known_hosts",
				key.Type(), ssh.FingerprintSHA256(key))
		} else if c.CheckKnownHosts {
			return fmt.Errorf("host key (%s:%s) does NOT exists in known_hosts", key.Type(), ssh.FingerprintSHA256(key))
		} else {
			return nil
		}
	}

	// check Dialer
	if c.ProxyDialer == nil {
		c.ProxyDialer = proxy.Direct
	}
	// Dial to host:port
	netConn, err := c.ProxyDialer.Dial("tcp", uri)
	if err != nil {
		return
	}
	// Create new ssh connect
	sshCon, channel, req, err := ssh.NewClientConn(netConn, uri, sshConfig)
	if err != nil {
		return
	}
	// Create *ssh.Client
	c.Client = ssh.NewClient(sshCon, channel, req)

	return
}
