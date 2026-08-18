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
	case "help":
		if len(args) != 1 {
			return errors.New(errors.KindUsage, "cli.sk", "sk help takes no arguments")
		}
		fmt.Fprintln(out.Out, "Usage: infrapilot sk <subcommand>\n\nSubcommands:\n  create              Create a local device identity\n  status              Show the local identity\n  replace <token>     Register this identity with a trusted service\n  revoke <device-id>  Revoke a trusted remote device\n  delete              Delete local identity (requires --confirm)\n  reset               Alias for delete (requires --confirm)\n  list                List trusted devices")
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
	case "delete", "reset":
		if len(args) != 2 || args[1] != "--confirm" {
			return errors.New(errors.KindUsage, "cli.sk", "this permanently deletes the private key and cannot be recovered; use: infrapilot sk "+args[0]+" --confirm")
		}
		id, err := identity.Delete(identityDir(env))
		if err != nil {
			return err
		}
		// Keep an audit record when the local database is available. Deletion
		// itself has already succeeded and must not depend on the audit store.
		if db, dbErr := openIdentityDB(ctx, env); dbErr == nil {
			defer db.Close()
			_, _ = db.SQL().ExecContext(ctx, "INSERT INTO security_audit(event_type,device_id,success,reason,occurred_at) VALUES(?,?,?,?,?)", "local_identity_deleted", id, 1, "local key removed by user", time.Now().UTC().Format(time.RFC3339Nano))
		}
		fmt.Fprintf(out.Out, "Deleted local device identity %s. The private key cannot be recovered; trusted remote devices were not changed.\n", id)
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
