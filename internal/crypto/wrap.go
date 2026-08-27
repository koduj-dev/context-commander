package crypto

import "fmt"

const nonceSize = 12 // AES-GCM standard nonce size (96 bits)

// packWrapped combines a nonce and ciphertext into the single-column format
// project_keys.wrapped_key uses (that table has no separate nonce column,
// unlike artifact_version_envelopes — see migrations/000002_project_keys.up.sql).
func packWrapped(ciphertext, nonce []byte) []byte {
	out := make([]byte, 0, len(nonce)+len(ciphertext))
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out
}

func unpackWrapped(blob []byte) (ciphertext, nonce []byte, err error) {
	if len(blob) < nonceSize {
		return nil, nil, fmt.Errorf("crypto: wrapped key blob too short (%d bytes)", len(blob))
	}
	return blob[nonceSize:], blob[:nonceSize], nil
}
