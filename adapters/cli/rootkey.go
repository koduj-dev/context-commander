package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
)

// rootKeyPreviousEnv holds the outgoing key during `ccd root-key rotate`.
const rootKeyPreviousEnv = "CC_ROOT_KEY_PREVIOUS"

func newRootKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "root-key",
		Short: "Generate, check and rotate the root key ($CC_ROOT_KEY) that wraps every project key",
	}

	generate := &cobra.Command{
		Use:   "generate",
		Short: "Print a new random root key, base64, for $CC_ROOT_KEY",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			key, err := crypto.GenerateRootKeyBase64()
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), key)
			return nil
		},
	}

	check := &cobra.Command{
		Use:   "check",
		Short: "Check $CC_ROOT_KEY against the project keys in the database, as `ccd serve` does at startup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool, root, err := openWithRootKey(cmd, rootKeyEnv)
			if err != nil {
				return err
			}
			defer pool.Close()
			res, err := crypto.CheckRootKey(cmd.Context(), pool, root)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch {
			case res.Fresh():
				_, _ = fmt.Fprintf(out, "%s: no project keys yet, nothing is encrypted\n", res.Current)
			case len(res.Others) == 0:
				_, _ = fmt.Fprintf(out, "%s: wraps all %d project keys\n", res.Current, res.Matching)
			default:
				return fmt.Errorf("%s wraps %d project keys, and %d are wrapped under other keys: %v; "+
					"finish the rotation with `ccd root-key rotate`", res.Current, res.Matching, others(res), res.Others)
			}
			return nil
		},
	}

	rotate := &cobra.Command{
		Use:   "rotate",
		Short: "Rewrap every project key from $" + rootKeyPreviousEnv + " to $" + rootKeyEnv,
		Long: "Rewraps every project key from the old root key ($" + rootKeyPreviousEnv + ") to the new one ($" + rootKeyEnv + "), " +
			"in one transaction. Content is not re-encrypted: only the project keys are rewrapped, so it takes seconds " +
			"however much is stored. Connect as cc_owner, the role `ccd migrate` uses. Stop `ccd serve` first, then start " +
			"it with the new key: a server still running with the old key cannot read anything once this commits, and a " +
			"project it creates in the meantime gets a key under the old root key (`ccd root-key check` finds it; running " +
			"rotate again fixes it). See docs/backup-restore.md.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool, newRoot, err := openWithRootKey(cmd, rootKeyEnv)
			if err != nil {
				return err
			}
			defer pool.Close()
			oldRoot, err := crypto.NewEnvRootKeyProvider(rootKeyPreviousEnv)
			if err != nil {
				return err
			}
			_, oldRef, _ := oldRoot.CurrentKey(cmd.Context())
			_, newRef, _ := newRoot.CurrentKey(cmd.Context())
			n, err := crypto.NewProvider(newRoot).RotateInstallationRootKey(cmd.Context(), pool, oldRoot)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"rewrapped %d project keys from %s to %s\nstart ccd serve with the new $%s, keep the old key until a backup "+
					"taken after this rotation is verified, then unset $%s\n",
				n, oldRef, newRef, rootKeyEnv, rootKeyPreviousEnv)
			return nil
		},
	}

	databaseURLFlag(check)
	databaseURLFlag(rotate)
	cmd.AddCommand(generate, check, rotate)
	return cmd
}

func openWithRootKey(cmd *cobra.Command, env string) (*db.Pool, crypto.RootKeyProvider, error) {
	root, err := crypto.NewEnvRootKeyProvider(env)
	if err != nil {
		return nil, nil, err
	}
	dsn, err := resolveDatabaseURL(cmd)
	if err != nil {
		return nil, nil, err
	}
	pool, err := db.NewPool(cmd.Context(), dsn)
	if err != nil {
		return nil, nil, err
	}
	return pool, root, nil
}

func others(c crypto.RootKeyCheck) int64 {
	var n int64
	for _, u := range c.Others {
		n += u.ProjectKeys
	}
	return n
}
