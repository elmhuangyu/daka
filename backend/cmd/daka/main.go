// Command daka is the entry point of the Daka habit tracker backend.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/elmhuangyu/daka/backend/lib/config"
)

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "daka: %v\n", err)
		os.Exit(1)
	}
}

// run loads and validates the configuration. The HTTP server is wired in later.
func run(args []string, getenv func(string) string, stderr io.Writer) error {
	fs := flag.NewFlagSet("daka", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("config", "", "path to the YAML config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*path, getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	if cfg.Auth.Issuer == "" {
		_, _ = fmt.Fprintln(stderr, "daka: single-user mode (no auth.issuer): all requests use default_user")
	}
	return nil
}
