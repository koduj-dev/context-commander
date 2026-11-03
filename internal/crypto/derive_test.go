package crypto

import (
	"context"
	"testing"
)

type fixedRoot [KeySize]byte

func (f fixedRoot) CurrentKey(context.Context) ([KeySize]byte, string, error) {
	return f, "test", nil
}

func TestDeriveKeySeparatesPurposesAndIsNotTheRootKey(t *testing.T) {
	ctx := context.Background()
	var root fixedRoot
	for i := range root {
		root[i] = byte(i)
	}

	a, err := DeriveKey(ctx, root, PurposeOIDCAttemptCookie)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := DeriveKey(ctx, root, PurposeOIDCAttemptCookie)
	if a != again {
		t.Fatal("the same purpose derived two different keys — a restart would invalidate every signature")
	}
	other, _ := DeriveKey(ctx, root, "context-commander/some-other-purpose/v1")
	if a == other {
		t.Fatal("two purposes derived the same key")
	}
	if a == [KeySize]byte(root) {
		t.Fatal("the derived key is the root key itself")
	}

	var rotated fixedRoot
	rotated[0] = 0xff
	if b, _ := DeriveKey(ctx, rotated, PurposeOIDCAttemptCookie); a == b {
		t.Fatal("a different root key derived the same key")
	}

	if _, err := DeriveKey(ctx, root, ""); err == nil {
		t.Fatal("an empty purpose was accepted — it would collide with every other caller that forgot one")
	}
}
