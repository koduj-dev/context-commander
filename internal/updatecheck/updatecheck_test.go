package updatecheck

import (
	"context"
	"errors"
	"testing"
)

type fake struct {
	tag      string
	err      error
	askedPre bool
}

func (f *fake) Latest(_ context.Context, pre bool) (string, string, error) {
	f.askedPre = pre
	return f.tag, "https://example.test/" + f.tag, f.err
}

func TestCheck(t *testing.T) {
	off := New("v0.1.0-beta.4", nil)
	off.Check(context.Background())
	if s := off.Status(); s.Enabled || s.CheckedAt != nil {
		t.Errorf("off: %+v", s)
	}

	src := &fake{tag: "v0.1.0-beta.5"}
	c := New("v0.1.0-beta.4", src)
	c.Check(context.Background())
	if s := c.Status(); !s.UpdateAvailable || s.Latest != "v0.1.0-beta.5" || !src.askedPre || s.CheckedAt == nil {
		t.Errorf("newer beta: %+v, asked pre %v", s, src.askedPre)
	}

	src.tag = "v0.1.0-beta.4"
	c.Check(context.Background())
	if c.Status().UpdateAvailable {
		t.Error("the same version reported as an update")
	}

	stable := New("v1.2.0", &fake{tag: "v1.3.0"})
	stable.Check(context.Background())
	if s := stable.Status(); !s.UpdateAvailable {
		t.Errorf("stable: %+v", s)
	}

	dev := New("dev", &fake{tag: "v1.3.0"})
	dev.Check(context.Background())
	if dev.Status().UpdateAvailable {
		t.Error("a development build cannot be compared")
	}

	failing := New("v1.2.0", &fake{err: errors.New("offline")})
	failing.Check(context.Background())
	if s := failing.Status(); s.Error != "offline" || s.UpdateAvailable {
		t.Errorf("failure: %+v", s)
	}
}
