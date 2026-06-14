package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rendaman0215/flakehound/internal/app"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv, version); err != nil {
		fmt.Fprintf(os.Stderr, "flakehound: %v\n", err)
		os.Exit(1)
	}
}
