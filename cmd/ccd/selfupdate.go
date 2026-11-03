package main

import (
	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/selfupdate"
)

// newSelfUpdateCmd replaces the running binary with a newer release. The
// resolution and verification rules are internal/selfupdate's, and the same
// as scripts/install.sh's.
func newSelfUpdateCmd() *cobra.Command {
	var opts selfupdate.Options
	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Replace this binary with the newest release",
		Long: `Replace this binary with the newest release from GitHub.

The newest stable release is used; while there is none yet, the newest
pre-release. --pre follows pre-releases too. The download must match its line
in the release's SHA256SUMS before it replaces anything, and when cosign is on
PATH the signature on SHA256SUMS is verified as well (--require-signature
fails without it). The swap is an atomic rename in the binary's own directory.

A ccd installed by a package manager (/usr/bin/ccd from the .deb or .rpm, or
Homebrew) is not touched; the message names the command that upgrades it.
A running "ccd serve" keeps the old version until it is restarted.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.Current = version
			return selfupdate.New().Run(cmd.Context(), opts, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&opts.CheckOnly, "check", false, "only report whether a newer release exists")
	cmd.Flags().BoolVar(&opts.Pre, "pre", false, "follow pre-releases too")
	cmd.Flags().BoolVar(&opts.RequireSignature, "require-signature", false, "fail unless cosign verifies the signature on SHA256SUMS")
	return cmd
}
