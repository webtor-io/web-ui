package main

import (
	"context"
	"errors"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/services/adminauth"
)

// makeAdminCMD groups maintenance for the single self-hosted administrator.
// Setting the password here rather than through ADMIN_PASSWORD keeps the
// secret out of `docker inspect` and out of shell history.
func makeAdminCMD() cli.Command {
	adminCMD := cli.Command{
		Name:  "admin",
		Usage: "Self-hosted administrator management commands",
	}
	configureAdmin(&adminCMD)
	return adminCMD
}

func configureAdmin(c *cli.Command) {
	setPasswordCmd := cli.Command{
		Name:      "set-password",
		Usage:     "Sets the administrator password",
		ArgsUsage: "<password>",
		Action:    setAdminPassword,
	}
	setPasswordCmd.Flags = cs.RegisterPGFlags(setPasswordCmd.Flags)
	c.Subcommands = []cli.Command{setPasswordCmd}
}

func setAdminPassword(c *cli.Context) error {
	password := c.Args().First()
	if password == "" {
		return errors.New("usage: web-ui admin set-password <password>")
	}

	pg := cs.NewPG(c)
	defer pg.Close()

	if pg.Get() == nil {
		return errors.New("db is nil")
	}

	// An empty env password is passed deliberately: this command writes to the
	// database, and ADMIN_PASSWORD would refuse the write with ErrManagedByEnv.
	store := adminauth.NewStore("", adminauth.NewPGRepo(pg))
	if err := store.Set(context.Background(), password); err != nil {
		return err
	}

	log.Info("administrator password updated")
	return nil
}
