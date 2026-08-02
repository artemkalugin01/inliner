package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aokalugin/inliner/inliner-core/internal/app"
	"github.com/aokalugin/inliner/inliner-core/internal/config"
	"github.com/aokalugin/inliner/inliner-core/internal/diagnostic"
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
	case "version":
		fmt.Println(version.Core)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: inliner-core <stdio|test-ollama|version>")
}
