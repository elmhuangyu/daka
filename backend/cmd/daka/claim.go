package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// runUserClaim implements `daka user claim --from default_user --to <iss|sub>`:
// it migrates all data of one user to another (typically the first OIDC user)
// in one transaction. --to is either the literal "iss|sub" pair of the OIDC
// identity or an already derived user ID ("oidc_..."). The target must have
// logged in once. Stop the server first for SQLite deployments.
func runUserClaim(args []string, getenv func(string) string, out io.Writer) error {
	fs := flag.NewFlagSet("daka user claim", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("config", "", "path to the YAML config file")
	from := fs.String("from", auth.DefaultUserID, "source user ID")
	to := fs.String("to", "", `target OIDC identity as "<iss>|<sub>" (or a derived "oidc_..." user ID)`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" {
		return errors.New("--to is required")
	}
	cfg, err := config.Load(*path, getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	target := *to
	if !strings.HasPrefix(target, "oidc_") {
		if !strings.Contains(target, "|") {
			return errors.New(`--to must look like "<iss>|<sub>"`)
		}
		target = auth.OIDCUserIDFromKey(target)
	}
	d, err := db.Open(cfg.DB)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := dbmodels.AutoMigrate(d.Write()); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := dbmodels.ClaimUser(d.Write(), *from, target); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "claimed all data of %q for %q\n", *from, target)
	return nil
}
