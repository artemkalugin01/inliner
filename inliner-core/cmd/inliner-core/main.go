package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/aokalugin/inliner/inliner-core/internal/app"
	"github.com/aokalugin/inliner/inliner-core/internal/config"
	"github.com/aokalugin/inliner/inliner-core/internal/diagnostic"
	"github.com/aokalugin/inliner/inliner-core/internal/diagnostics"
	"github.com/aokalugin/inliner/inliner-core/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "stdio":
		if err := app.Run(context.Background(), os.Stdin, os.Stdout); err != nil {
			log.Printf("inliner-core stopped: %v", err)
			os.Exit(1)
		}
	case "test-ollama":
		cfg, err := config.LoadEnv()
		if err != nil {
			log.Printf("config error: %v", err)
			os.Exit(1)
		}
		if err := diagnostic.TestOllama(context.Background(), cfg, os.Stdout); err != nil {
			log.Printf("ollama test failed: %v", err)
			os.Exit(1)
		}
	case "debug":
		verbose, err := parseDebugArgs(os.Args[2:])
		if err != nil {
			log.Printf("debug arguments: %v", err)
			os.Exit(2)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runDebug(ctx, verbose, os.Stdout); err != nil {
			log.Printf("diagnostic server stopped: %v", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println(version.Core)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: inliner-core <stdio|debug|test-ollama|version>")
}

func parseDebugArgs(args []string) (bool, error) {
	verbose := false
	for _, arg := range args {
		if arg != "--verbose" {
			return false, fmt.Errorf("unknown option %q; usage: inliner-core debug [--verbose]", arg)
		}
		verbose = true
	}
	return verbose, nil
}

func runDebug(ctx context.Context, verbose bool, output io.Writer) error {
	server, err := diagnostics.ListenDefault(verbose, output)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "diagnostic server listening at %s\n", diagnostics.DefaultSocketPath())
	<-ctx.Done()
	return server.Close()
}
