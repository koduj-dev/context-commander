package directory_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/koduj-dev/context-commander/internal/directory"
)

// The seed these tests rely on is deploy/ldap/*.ldif — the same files
// `docker compose --profile ldap up` uses, deliberately, so a fixture that
// works by hand works here and a fixture someone breaks breaks both.
const (
	ldapImage = "osixia/openldap:1.5.0"
	// The seed's password, committed in deploy/ldap/01-people.ldif for the
	// same reason: it belongs to a container that exists for seconds and a
	// fixture nobody can read is a fixture nobody can fix.
	ldapPassword = "ldap-dev-password" //nolint:gosec // G101: test fixture, not a credential
	userBase     = "ou=people,dc=example,dc=test"
	readonlyDN   = "cn=readonly,ou=services,dc=example,dc=test"
)

// ldapURL is the shared directory, started once by TestMain.
//
// One container for the whole package, not one per test function: slapd takes
// a few seconds to start, every test here reads the same immutable seed, and
// two containers churning at once produced an intermittent startup failure —
// a flaky integration test is worse than none, because it teaches people to
// re-run the suite instead of reading it.
var ldapURL string

func TestMain(m *testing.M) {
	url, terminate, err := startLDAP()
	if err != nil {
		// Not a skip: Docker is a stated requirement for the integration
		// tier (docs/testing.md), and silently passing without a directory
		// would mean this package is only ever "tested" by compiling.
		fmt.Fprintf(os.Stderr, "start ldap container: %v\n", err)
		os.Exit(1)
	}
	ldapURL = url
	code := m.Run()
	terminate()
	// Started lazily by the OIDC tests, and a no-op if none of them ran.
	stopKeycloak()
	os.Exit(code)
}

func startLDAP() (url string, terminate func(), err error) {
	ctx := context.Background()

	ldif := func(name string) (testcontainers.ContainerFile, error) {
		path, err := filepath.Abs(filepath.Join("..", "..", "deploy", "ldap", name))
		if err != nil {
			return testcontainers.ContainerFile{}, fmt.Errorf("resolve %s: %w", name, err)
		}
		return testcontainers.ContainerFile{
			HostFilePath:      path,
			ContainerFilePath: "/container/service/slapd/assets/config/bootstrap/ldif/custom/" + name,
			FileMode:          0o644,
		}, nil
	}

	people, err := ldif("01-people.ldif")
	if err != nil {
		return "", nil, err
	}
	access, err := ldif("02-access.ldif")
	if err != nil {
		return "", nil, err
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        ldapImage,
			ExposedPorts: []string{"389/tcp"},
			Env: map[string]string{ //nolint:gosec // G101: disposable container, see ldapPassword
				"LDAP_ORGANISATION":   "Context Commander Test",
				"LDAP_DOMAIN":         "example.test",
				"LDAP_ADMIN_PASSWORD": "ldap_admin_test_only",
				"LDAP_TLS":            "false",
			},
			// --copy-service is what makes the mounted LDIFs run at all.
			// Without it the image starts an empty tree and every test below
			// fails with "no such user", which reads as a bug in the code
			// rather than a seed that was never read.
			Cmd:   []string{"--copy-service", "--loglevel", "info"},
			Files: []testcontainers.ContainerFile{people, access},
			// Two signals, in this order, because each one alone has been
			// watched to let a broken start through.
			//
			// The log line is the important half, and it is not decoration.
			// This image bootstraps by starting slapd, applying the mounted
			// LDIFs, STOPPING slapd, and then starting it again for real —
			// its own log says so:
			//
			//	Start OpenLDAP...            <- bootstrap instance
			//	Add custom bootstrap ldif...
			//	Stop OpenLDAP...             <- and it is killed again
			//	First start is done...
			//	Running .../process/slapd/run...   <- the instance that stays
			//	slapd starting
			//
			// So a readiness check can pass against the bootstrap instance
			// moments before it is torn down. That is what broke CI: every
			// subtest failed with "read: connection reset by peer" against a
			// directory that was merely mid-restart, which reads as "LDAP
			// sign-in is broken".
			//
			// It is deliberately this line rather than the second occurrence
			// of "slapd starting", which was the first thing I tried. The
			// bootstrap instance does not reliably log that at all — it was
			// present when I ran the image by hand and absent on six runs out
			// of six under the test, so waiting for occurrence 2 simply hung
			// for the full timeout. "Running .../slapd/run" is emitted once,
			// by the supervisor, and only after the bootstrap is stopped.
			//
			// This does couple the test to a log line in the image. If
			// osixia changes the wording the wait times out and says so,
			// which is a loud, obvious failure — unlike the race it replaces.
			//
			// The exec probe then checks what a log line cannot: that the
			// seed actually loaded and that the ACL lets the service account
			// read it, inside the container, before any port mapping is
			// involved. Both of those have gone wrong here before.
			WaitingFor: wait.ForAll(
				wait.ForLog("Running /container/run/process/slapd/run"),
				wait.ForExec([]string{
					"ldapsearch", "-x", "-H", "ldap://localhost:389",
					"-D", readonlyDN, "-w", ldapPassword,
					"-b", userBase, "(uid=alice)", "uid",
				}),
			).WithStartupTimeoutDefault(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return "", nil, err
	}
	stop := func() {
		if err := container.Terminate(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "terminate ldap container: %v\n", err)
		}
	}

	endpoint, err := container.PortEndpoint(ctx, "389/tcp", "ldap")
	if err != nil {
		stop()
		return "", nil, fmt.Errorf("ldap endpoint: %w", err)
	}
	if err := waitReachable(ctx, endpoint); err != nil {
		stop()
		return "", nil, err
	}
	return endpoint, stop, nil
}

// waitReachable blocks until the directory answers a real bind FROM THE HOST,
// over the mapped port, the way every test below will.
//
// The exec probe above is not enough, and CI proved it. osixia's entrypoint
// starts slapd, applies the mounted LDIFs, and then RESTARTS slapd to run it
// for real. An `ldapsearch` executed inside the container can therefore
// succeed against the bootstrap process moments before it is torn down, so
// testcontainers reports the container ready and the first host-side bind
// lands in the gap:
//
//	bind as uid=alice,...: unable to read LDAP response packet:
//	read tcp [::1]:52438->[::1]:32781: read: connection reset by peer
//
// Every subtest then fails, which reads as "LDAP sign-in is broken" when the
// directory is merely still starting. It is timing-dependent, so it passed
// locally and on most CI runs and failed on one — the worst kind.
//
// The exec probe stays, because it checks something this cannot: that the
// seed loaded and the ACL permits reading it, inside the container, before
// any port mapping is involved. This adds the half that has to be true for
// the tests to mean anything.
func waitReachable(ctx context.Context, url string) error {
	const (
		deadline = 60 * time.Second
		interval = 250 * time.Millisecond
	)
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	d, err := directory.NewLDAP(directory.LDAPConfig{
		URL:          url,
		BindDN:       readonlyDN,
		BindPassword: ldapPassword,
		UserBase:     userBase,
	})
	if err != nil {
		return fmt.Errorf("probe config: %w", err)
	}

	// Every error is retried, including ones that look like a broken fixture.
	// An earlier version of this gate failed fast on anything that was not
	// ErrUnavailable, on the reasoning that a directory which answers but
	// rejects the seed's own credentials is broken rather than slow. Watching
	// it start proved that wrong twice over: mid-bootstrap slapd answers a
	// service-account bind with LDAP code 49, and a search the ACL has not
	// yet opened returns zero entries, which this package reports as
	// ErrNoSuchUser (the masked access denial in docs/gotchas.md). Both are
	// indistinguishable from a wrong fixture and both resolve themselves a
	// moment later, so treating either as fatal just moves the flake.
	//
	// A genuinely broken seed therefore costs the full deadline and then says
	// so. That is the right trade: a slow clear failure beats a fast
	// intermittent one, which is the whole reason this function exists.
	var last error
	for {
		if _, last = d.Authenticate(ctx, "alice", ldapPassword); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"directory on %s never answered for alice within %s — still starting, "+
					"or deploy/ldap/*.ldif is wrong: %w", url, deadline, last)
		case <-time.After(interval):
		}
	}
}

func newLDAP(t *testing.T, url string) *directory.LDAP {
	t.Helper()
	d, err := directory.NewLDAP(directory.LDAPConfig{
		URL:          url,
		BindDN:       readonlyDN,
		BindPassword: ldapPassword,
		UserBase:     userBase,
	})
	if err != nil {
		t.Fatalf("NewLDAP: %v", err)
	}
	return d
}

func TestLDAPAuthenticate(t *testing.T) {
	d := newLDAP(t, ldapURL)
	ctx := context.Background()

	t.Run("by uid", func(t *testing.T) {
		got, err := d.Authenticate(ctx, "alice", ldapPassword)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		if got.Email != "alice@example.test" {
			t.Errorf("Email = %q, want alice@example.test", got.Email)
		}
		if got.DisplayName != "Alice Ashworth" {
			t.Errorf("DisplayName = %q, want Alice Ashworth", got.DisplayName)
		}
		if got.Subject == "" {
			t.Error("Subject is empty — there is nothing to key user_identities on")
		}
		if got.Subject == got.Email || got.Subject == "alice" {
			t.Errorf("Subject = %q: it must be the directory's own stable id, not a label a person can change", got.Subject)
		}
		if got.Provider != "ldap" {
			t.Errorf("Provider = %q, want ldap", got.Provider)
		}
	})

	t.Run("by email, same person", func(t *testing.T) {
		// The default filter accepts either, because a login form that
		// insists on knowing which one you have is a login form that argues
		// with people. Both must resolve to the SAME subject, or the two ways
		// in would provision two accounts.
		byUID, err := d.Authenticate(ctx, "alice", ldapPassword)
		if err != nil {
			t.Fatalf("by uid: %v", err)
		}
		byMail, err := d.Authenticate(ctx, "alice@example.test", ldapPassword)
		if err != nil {
			t.Fatalf("by mail: %v", err)
		}
		if byUID.Subject != byMail.Subject {
			t.Errorf("subjects differ: %q by uid, %q by mail", byUID.Subject, byMail.Subject)
		}
	})

	t.Run("the subject is stable across sign-ins", func(t *testing.T) {
		first, err := d.Authenticate(ctx, "bob", ldapPassword)
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		second, err := d.Authenticate(ctx, "bob", ldapPassword)
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		if first.Subject != second.Subject {
			t.Errorf("subject changed between sign-ins: %q then %q", first.Subject, second.Subject)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		_, err := d.Authenticate(ctx, "alice", "not the password")
		if !errors.Is(err, directory.ErrInvalidCredentials) {
			t.Errorf("err = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("an empty password is refused, not treated as an anonymous bind", func(t *testing.T) {
		// The oldest bug in LDAP integrations: a simple bind with an empty
		// password is an *unauthenticated bind*, which many servers accept.
		// Admitting it would let anyone in who knows a username.
		for _, pw := range []string{"", " "} {
			_, err := d.Authenticate(ctx, "alice", pw)
			if !errors.Is(err, directory.ErrInvalidCredentials) {
				t.Errorf("password %q: err = %v, want ErrInvalidCredentials", pw, err)
			}
		}
	})

	t.Run("unknown identifier", func(t *testing.T) {
		_, err := d.Authenticate(ctx, "nobody", ldapPassword)
		if !errors.Is(err, directory.ErrNoSuchUser) {
			t.Errorf("err = %v, want ErrNoSuchUser", err)
		}
	})

	t.Run("a filter wildcard matches nobody", func(t *testing.T) {
		// Unescaped, "*" in the identifier would match every entry, and the
		// bind that follows would authenticate whoever the server returned
		// first. ldap.EscapeFilter turns it into \2a, which matches a literal
		// asterisk — that is, nobody.
		for _, injection := range []string{"*", "alice)(uid=*", "*)(|(uid=*"} {
			_, err := d.Authenticate(ctx, injection, ldapPassword)
			if !errors.Is(err, directory.ErrNoSuchUser) {
				t.Errorf("identifier %q: err = %v, want ErrNoSuchUser", injection, err)
			}
		}
	})

	t.Run("an entry with no mail attribute is refused with a reason", func(t *testing.T) {
		// carol authenticates perfectly well; she just cannot be provisioned,
		// because users.email is NOT NULL and inventing an address would put
		// one into the product that belongs to nobody. The error has to say
		// which entry, so an administrator can fix the directory.
		_, err := d.Authenticate(ctx, "carol", ldapPassword)
		if !errors.Is(err, directory.ErrNoEmail) {
			t.Fatalf("err = %v, want ErrNoEmail", err)
		}
		if !strings.Contains(err.Error(), "uid=carol") {
			t.Errorf("the error does not name the entry: %v", err)
		}
	})

	t.Run("mail is returned as the directory stores it", func(t *testing.T) {
		// dave's address is recorded mixed-case. Our own users.email is
		// matched case-insensitively (migrations/000025), so this is allowed
		// to differ from what anyone types — but it must not be silently
		// rewritten here, because what the directory holds is the fact.
		got, err := d.Authenticate(ctx, "dave", ldapPassword)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		if got.Email != "Dave.Dvorak@Example.Test" {
			t.Errorf("Email = %q, want the stored Dave.Dvorak@Example.Test", got.Email)
		}
	})
}

func TestLDAPDirectoryFaultsAreNotFailedLogins(t *testing.T) {
	ctx := context.Background()

	t.Run("a bad service account is unavailable, not invalid credentials", func(t *testing.T) {
		// Telling someone their password is wrong when the SERVICE account is
		// misconfigured sends them to reset a password that was never the
		// problem, and hides an outage behind a help-desk ticket.
		d, err := directory.NewLDAP(directory.LDAPConfig{
			URL: ldapURL, BindDN: readonlyDN, BindPassword: "wrong service password", UserBase: userBase,
		})
		if err != nil {
			t.Fatalf("NewLDAP: %v", err)
		}
		_, err = d.Authenticate(ctx, "alice", ldapPassword)
		if !errors.Is(err, directory.ErrUnavailable) {
			t.Errorf("err = %v, want ErrUnavailable", err)
		}
		if errors.Is(err, directory.ErrInvalidCredentials) {
			t.Error("a service-account fault was reported as the caller's bad password")
		}
	})

	t.Run("an unreachable server is unavailable", func(t *testing.T) {
		d, err := directory.NewLDAP(directory.LDAPConfig{
			URL: "ldap://127.0.0.1:1", UserBase: userBase, Timeout: 2 * time.Second,
		})
		if err != nil {
			t.Fatalf("NewLDAP: %v", err)
		}
		if _, err := d.Authenticate(ctx, "alice", ldapPassword); !errors.Is(err, directory.ErrUnavailable) {
			t.Errorf("err = %v, want ErrUnavailable", err)
		}
	})
}

func TestNewLDAPRejectsBadConfiguration(t *testing.T) {
	// Checked at startup, so a typo is a server that will not start rather
	// than a login screen that fails for everyone at nine in the morning.
	for name, cfg := range map[string]directory.LDAPConfig{
		"no url":              {UserBase: userBase},
		"no user base":        {URL: "ldap://localhost:389"},
		"wrong scheme":        {URL: "http://localhost:389", UserBase: userBase},
		"filter without %s":   {URL: "ldap://localhost:389", UserBase: userBase, UserFilter: "(uid=fixed)"},
		"password untethered": {URL: "ldap://localhost:389", UserBase: userBase, BindPassword: "x"},
		"missing ca file":     {URL: "ldaps://localhost:636", UserBase: userBase, CACertFile: "/nonexistent.pem"},
	} {
		if _, err := directory.NewLDAP(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
