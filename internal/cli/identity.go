package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/identity"
	"github.com/iAghaTraker/InfraPilot/internal/storage"
)

func runSK(ctx context.Context, env Env, args []string, out IO) error {
	if len(args) == 0 {
		return errors.New(errors.KindUsage, "cli.sk", "secure identity subcommand is required")
	}
	switch args[0] {
	case "create":
		if len(args) != 1 {
			return errors.New(errors.KindUsage, "cli.sk", "sk create takes no arguments")
		}
		i, token, err := identity.Create(identityDir(env))
		if err != nil {
			return err
		}
		fmt.Fprintf(out.Out, "InfraPilot Secure Identity\n\nDevice ID: %s\nPublic Key: %s\nPairing Token: %s\n\nPrivate key: stored securely\n", i.DeviceID, base64.RawURLEncoding.EncodeToString(i.PublicKey), token)
	case "replace":
		if len(args) != 2 {
			return errors.New(errors.KindUsage, "cli.sk", "usage: infrapilot sk replace <pairing-token>")
		}
		db, err := openIdentityDB(ctx, env)
		if err != nil {
			return err
		}
		defer db.Close()
		device, err := identity.NewRepository(db).Replace(ctx, args[1], time.Now().UTC())
		if err != nil {
			return err
		}
		fmt.Fprintf(out.Out, "Paired device %s\n", device.DeviceID)
	case "list":
		if len(args) != 1 {
			return errors.New(errors.KindUsage, "cli.sk", "sk list takes no arguments")
		}
		db, err := openIdentityDB(ctx, env)
		if err != nil {
			return err
		}
		defer db.Close()
		devices, err := identity.NewRepository(db).List(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintln(out.Out, "InfraPilot Trusted Devices\n\nDEVICE ID\tSTATUS")
		for _, d := range devices {
			fmt.Fprintf(out.Out, "%s\t%s\n", d.DeviceID, d.Status)
		}
	case "revoke":
		if len(args) != 2 {
			return errors.New(errors.KindUsage, "cli.sk", "usage: infrapilot sk revoke <device-id>")
		}
		db, err := openIdentityDB(ctx, env)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := identity.NewRepository(db).Revoke(ctx, args[1], time.Now().UTC()); err != nil {
			return err
		}
		fmt.Fprintf(out.Out, "Revoked device %s\n", args[1])
	case "status":
		if len(args) != 1 {
			return errors.New(errors.KindUsage, "cli.sk", "sk status takes no arguments")
		}
		i, err := identity.Load(identityDir(env))
		if err != nil {
			return err
		}
		fmt.Fprintf(out.Out, "InfraPilot Secure Identity\n\nDevice ID: %s\nPublic Key: %s\nPrivate key: stored securely\n", i.DeviceID, base64.RawURLEncoding.EncodeToString(i.PublicKey))
	default:
		return errors.Newf(errors.KindUsage, "cli.sk", "unknown secure identity subcommand %q", args[0])
	}
	return nil
}

func identityDir(env Env) string {
	if env.Config.Agent.DataDir != "" {
		return env.Config.Agent.DataDir
	}
	return env.Paths.DataDir
}

func openIdentityDB(ctx context.Context, env Env) (*storage.DB, error) {
	path, err := env.Config.DatabasePath()
	if err != nil {
		return nil, err
	}
	return storage.Open(ctx, storage.Options{Path: path, BusyTimeout: env.Config.Storage.BusyTimeout})
}
