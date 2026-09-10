package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
)

func taskCredential(ctx context.Context, cfg config.Config, args []string) error {
	if args[0] == "revoke-task-delegation" {
		if len(args) != 2 {
			return errors.New("usage: admin revoke-task-delegation <delegation-id>")
		}
		svc, ctx, close, err := governanceService(ctx, cfg)
		if err != nil {
			return err
		}
		defer close()
		d, err := svc.RevokeTaskCredential(ctx, args[1])
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(d)
	}
	if len(args) != 4 {
		return errors.New("usage: admin delegate-task <task-id> <ttl-seconds:60..3600> <private-token-file>")
	}
	seconds, err := strconv.Atoi(args[2])
	if err != nil || seconds < 60 || seconds > 3600 {
		return errors.New("TTL must be between 60 and 3600 seconds")
	}
	parent, err := os.Stat(filepath.Dir(args[3]))
	if err != nil {
		return err
	}
	if !parent.IsDir() || parent.Mode().Perm()&0077 != 0 {
		return errors.New("token file requires a private parent directory (mode 0700)")
	}
	f, err := os.OpenFile(args[3], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = f.Close()
		if !committed {
			_ = os.Remove(args[3])
		}
	}()
	svc, ctx, close, err := governanceService(ctx, cfg)
	if err != nil {
		return err
	}
	defer close()
	d, token, err := svc.IssueTaskCredential(ctx, args[1], time.Duration(seconds)*time.Second)
	if err != nil {
		return err
	}
	if _, err = f.WriteString(token); err == nil {
		err = f.Sync()
	}
	if err != nil {
		_, revokeErr := svc.RevokeTaskCredential(ctx, d.ID)
		return errors.Join(err, revokeErr)
	}
	committed = true
	// Metadata only. The credential is never emitted to stdout or evidence.
	return json.NewEncoder(os.Stdout).Encode(d)
}
