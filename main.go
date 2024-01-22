package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path"

	"github.com/sagan/tgshell/config"
	_ "github.com/sagan/tgshell/executor/all"
	"github.com/sagan/tgshell/telegram"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func init() {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	os.Chdir(userHomeDir)
	flag.StringVar(&config.ConfigPath, "config", path.Join(userHomeDir, ".config", "tgshell"), "config dir path")
}

func main() {
	fmt.Printf("tgshell %s, commit %s, built at %s\n", version, commit, date)
	flag.Parse()
	log.Printf("configPath: %s", config.ConfigPath)
	if err := config.InitConfig(); err != nil {
		log.Fatalf("Failed to init config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	defer func() {
		signal.Stop(c)
		cancel()
	}()
	go func() {
		select {
		case <-c:
			cancel()
			log.Printf("signal received, exitting")
		case <-ctx.Done(): // clean up the goroutine
		}
	}()
	telegram.Start(ctx)
}
