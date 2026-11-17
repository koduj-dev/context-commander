package cli

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/skills"
)

// readSigningKey reads a private key written by `ccd skill keygen`.
func readSigningKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-supplied key path
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%s is not a key written by `ccd skill keygen`", path)
	}
	return ed25519.PrivateKey(key), nil
}

// signSkill signs content for the skill named name (docs/adr/0040).
func signSkill(key ed25519.PrivateKey, name string, content []byte) []byte {
	sum := sha256.Sum256(content)
	return ed25519.Sign(key, skills.SigningMessage(name, hex.EncodeToString(sum[:])))
}

func newSkillKeygenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keygen",
		Short: "Create an Ed25519 key pair for signing skills",
		Long: "Writes the private key to --out (mode 0600; keep it off the server, with whoever publishes skills) and " +
			"prints the public key, which an installation administrator registers with " +
			"POST /v1/installation/skill-signers (docs/adr/0040). Works offline.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, _ := cmd.Flags().GetString("out")
			if out == "" {
				return errors.New("--out is required")
			}
			pub, priv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return err
			}
			f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // operator-supplied path
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(priv)); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "private key written to %s\npublic key: %s\n", out, base64.StdEncoding.EncodeToString(pub))
			return nil
		},
	}
	cmd.Flags().String("out", "", "file to write the private key to; refused if it exists")
	return cmd
}

func newSkillSignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sign",
		Short: "Print the signature of a skill's content, for publishing it as signed",
		Long: "Signs \"context-commander/skill/v1\\n<name>\\n<sha256 of the content>\" with a key from `ccd skill keygen` and " +
			"prints the signature, base64, for the signature field of skill.publish or POST /v1/skills. " +
			"`ccd skill publish --sign-key` does the same in one step. Works offline.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keyPath, _ := cmd.Flags().GetString("key")
			name, _ := cmd.Flags().GetString("name")
			file, _ := cmd.Flags().GetString("file")
			if keyPath == "" || name == "" {
				return errors.New("--key and --name are required")
			}
			key, err := readSigningKey(keyPath)
			if err != nil {
				return err
			}
			var content []byte
			if file == "" || file == "-" {
				content, err = io.ReadAll(cmd.InOrStdin())
			} else {
				content, err = os.ReadFile(file) //nolint:gosec // operator-supplied path
			}
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), base64.StdEncoding.EncodeToString(signSkill(key, name, content)))
			return nil
		},
	}
	cmd.Flags().String("key", "", "private key file from `ccd skill keygen`")
	cmd.Flags().String("name", "", "the skill's name, exactly as published")
	cmd.Flags().String("file", "", "file holding the content (default: stdin)")
	return cmd
}
