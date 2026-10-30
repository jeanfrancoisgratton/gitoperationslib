package contracttest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Account checks the authenticated user's e-mail addresses, SSH keys and GPG
// keys. It adds a throwaway address (where the forge allows it), SSH key and
// GPG key, and removes them. A part the token may not read (HTTP 403) is
// skipped; the GPG part also needs the gpg binary.
func Account(t *testing.T, env Env) {
	acct, ok := env.Client.(gitoperationslib.Account)
	if !ok || !env.Client.Capabilities().Has(gitoperationslib.CapAccount) {
		t.Fatalf("%s does not implement Account", env.Client.Backend())
	}
	t.Run("emails", func(t *testing.T) { accountEmails(t, env, acct.Emails()) })
	t.Run("ssh keys", func(t *testing.T) { accountSSHKeys(t, acct.SSHKeys()) })
	t.Run("gpg keys", func(t *testing.T) { accountGPGKeys(t, env, acct) })
}

// skipForbidden skips the test when the token lacks the permission.
func skipForbidden(t *testing.T, err error) {
	t.Helper()
	if gitoperationslib.IsForbidden(err) {
		t.Skipf("the token lacks the permission: %v", err)
	}
}

func randomHex(t *testing.T) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func hasEmail(list []gitoperationslib.Email, addr string) bool {
	return slices.ContainsFunc(list, func(e gitoperationslib.Email) bool { return strings.EqualFold(e.Address, addr) })
}

func accountEmails(t *testing.T, env Env, emails gitoperationslib.Emails) {
	ctx := testContext(t)
	list, err := emails.List(ctx)
	skipForbidden(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(list, func(e gitoperationslib.Email) bool { return e.Primary }) {
		t.Errorf("no primary address in %+v", list)
	}

	addr := RepoPrefix + randomHex(t) + "@example.com"
	added, err := emails.Add(ctx, addr)
	if isBitbucket(env) {
		if !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("Bitbucket Add: err = %v, want ErrNotSupported", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = emails.Delete(context.Background(), addr) })
	if !hasEmail(added, addr) {
		t.Errorf("Add returned %+v, want %s", added, addr)
	}
	if list, err = emails.List(ctx); err != nil || !hasEmail(list, addr) {
		t.Errorf("after Add: List = %+v, %v", list, err)
	}
	if err := emails.Delete(ctx, addr); err != nil {
		t.Fatal(err)
	}
	if list, err = emails.List(ctx); err != nil || hasEmail(list, addr) {
		t.Errorf("after Delete: List = %+v, %v", list, err)
	}
}

// newSSHKey returns a fresh ed25519 public key in OpenSSH format.
func newSSHKey(t *testing.T, comment string) string {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var blob bytes.Buffer
	for _, part := range [][]byte{[]byte("ssh-ed25519"), pub} {
		_ = binary.Write(&blob, binary.BigEndian, uint32(len(part)))
		blob.Write(part)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob.Bytes()) + " " + comment
}

func accountSSHKeys(t *testing.T, keys gitoperationslib.SSHKeys) {
	ctx := testContext(t)
	_, err := keys.List(ctx, gitoperationslib.ListOptions{})
	skipForbidden(t, err)
	if err != nil {
		t.Fatal(err)
	}

	title := RepoPrefix + randomHex(t)
	pub := newSSHKey(t, title)
	key, err := keys.Add(ctx, gitoperationslib.AddSSHKeyOptions{Title: title, Key: pub})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = keys.Delete(context.Background(), key.ID) })
	// Servers drop or rewrite the comment: compare type and key material.
	material := strings.Join(strings.Fields(pub)[:2], " ")
	if key.ID == 0 || key.Title != title || !strings.HasPrefix(key.Key, material) {
		t.Errorf("Add returned %+v", key)
	}

	got, err := keys.Get(ctx, key.ID)
	if err != nil || got.Title != title {
		t.Errorf("Get = %+v, %v", got, err)
	}
	listed := false
	for k, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{}, keys.List) {
		if err != nil {
			t.Fatal(err)
		}
		listed = listed || k.ID == key.ID
	}
	if !listed {
		t.Error("the new key is not listed")
	}

	if err := keys.Delete(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Get(ctx, key.ID); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after Delete: err = %v, want 404", err)
	}
}

func accountGPGKeys(t *testing.T, env Env, acct gitoperationslib.Account) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is not installed")
	}
	ctx := testContext(t)
	keys := acct.GPGKeys()
	_, err := keys.List(ctx, gitoperationslib.ListOptions{})
	skipForbidden(t, err)
	if err != nil {
		t.Fatal(err)
	}

	// Forgejo and Bitbucket want a key bearing one of the account's
	// addresses.
	emails, err := acct.Emails().List(ctx)
	if err != nil {
		t.Skipf("cannot read the account's e-mail addresses for the key: %v", err)
	}
	var primary string
	for _, e := range emails {
		if e.Primary {
			primary = e.Address
		}
	}
	g := newGPG(t)
	armored, keyID := g.generate(t, "gitoperationslib test <"+primary+">")

	key, err := keys.Add(ctx, armored, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = keys.Delete(context.Background(), key.ID) })
	if key.ID == 0 || key.KeyID != keyID {
		t.Errorf("Add returned ID %d, key ID %q; want key ID %s", key.ID, key.KeyID, keyID)
	}
	if got, err := keys.Get(ctx, key.ID); err != nil || got.KeyID != keyID {
		t.Errorf("Get = %+v, %v", got, err)
	}

	token, err := keys.VerificationToken(ctx)
	if env.Client.Backend() == gitoperationslib.BackendForgejo {
		if err != nil {
			t.Fatal(err)
		}
		verified, err := keys.Verify(ctx, keyID, g.sign(t, keyID, token))
		if err != nil || !verified.Verified {
			t.Errorf("Verify = %+v, %v", verified, err)
		}
		// A key with none of the account's addresses needs a signature.
		stranger, strangerID := g.generate(t, "stranger <"+RepoPrefix+randomHex(t)+"@example.org>")
		if _, err := keys.Add(ctx, stranger, ""); !errors.Is(err, gitoperationslib.ErrGPGSignature) {
			t.Errorf("unknown address, no signature: err = %v, want ErrGPGSignature", err)
		}
		signed, err := keys.Add(ctx, stranger, g.sign(t, strangerID, token))
		if err != nil || !signed.Verified {
			t.Errorf("unknown address, signed: %+v, %v", signed, err)
		} else {
			t.Cleanup(func() { _ = keys.Delete(context.Background(), signed.ID) })
		}
	} else if !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("VerificationToken: err = %v, want ErrNotSupported", err)
	}

	if err := keys.Delete(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Get(ctx, key.ID); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after Delete: err = %v, want 404", err)
	}
}

// gpgHome runs gpg in a throwaway keyring.
type gpgHome string

func newGPG(t *testing.T) gpgHome {
	dir, err := os.MkdirTemp("", "gpg") // short path: gpg-agent's socket path is length-limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("gpgconf", "--homedir", dir, "--kill", "gpg-agent").Run()
		os.RemoveAll(dir)
	})
	return gpgHome(dir)
}

func (g gpgHome) run(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("gpg", append([]string{"--homedir", string(g), "--batch", "--quiet", "--pinentry-mode", "loopback", "--passphrase", ""}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("gpg %v: %v: %s", args, err, stderr.String())
	}
	return out.String()
}

// generate creates a signing key for uid and returns it armored, with its
// 16-digit key ID.
func (g gpgHome) generate(t *testing.T, uid string) (armored, keyID string) {
	g.run(t, "", "--quick-gen-key", uid, "ed25519", "sign", "never")
	for _, line := range strings.Split(g.run(t, "", "--with-colons", "--list-keys", uid), "\n") {
		if f := strings.Split(line, ":"); f[0] == "pub" && len(f) > 4 {
			keyID = f[4]
		}
	}
	return g.run(t, "", "--armor", "--export", uid), keyID
}

// sign returns an armored detached signature of data by keyID.
func (g gpgHome) sign(t *testing.T, keyID, data string) string {
	return g.run(t, data, "--local-user", keyID, "--armor", "--detach-sign")
}
