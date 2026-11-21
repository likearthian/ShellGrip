package main

import (
	"sync"
	"testing"

	"github.com/egoist/mygo"
)

type fakeSecretStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newFakeSecretStore(t *testing.T) *fakeSecretStore {
	t.Helper()
	store := &fakeSecretStore{data: map[string][]byte{}}
	prev := keychain
	keychain = store
	t.Cleanup(func() { keychain = prev })
	return store
}

func (f *fakeSecretStore) Get(key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[key]
	if !ok {
		return nil, mygo.ErrSecretNotFound
	}
	return append([]byte(nil), v...), nil
}

func (f *fakeSecretStore) Set(key string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = append([]byte(nil), value...)
	return nil
}

func (f *fakeSecretStore) Delete(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[key]; !ok {
		return mygo.ErrSecretNotFound
	}
	delete(f.data, key)
	return nil
}

func TestSSHPasswordKeychainRoundTrip(t *testing.T) {
	newFakeSecretStore(t)
	conn := sshConnection{Host: "example.com", User: "dev", Port: 2222}

	if got := loadSSHPassword(conn); got != "" {
		t.Fatalf("expected empty password, got %q", got)
	}
	saveSSHPassword(conn, "s3cret")
	if got := loadSSHPassword(conn); got != "s3cret" {
		t.Fatalf("loadSSHPassword = %q, want %q", got, "s3cret")
	}
	deleteSSHPassword(conn)
	if got := loadSSHPassword(conn); got != "" {
		t.Fatalf("expected deleted password to be empty, got %q", got)
	}
}

func TestGitCredentialsKeychainRoundTrip(t *testing.T) {
	newFakeSecretStore(t)
	dir := "/tmp/my-repo"

	if got := loadGitCredentials(dir); got != nil {
		t.Fatalf("expected nil credentials, got %+v", got)
	}
	saveGitCredentials(dir, gitCredentials{Username: "alice", Password: "tok"})
	got := loadGitCredentials(dir)
	if got == nil || got.Username != "alice" || got.Password != "tok" {
		t.Fatalf("loadGitCredentials = %+v, want alice/tok", got)
	}
	deleteGitCredentials(dir)
	if got := loadGitCredentials(dir); got != nil {
		t.Fatalf("expected nil credentials after delete, got %+v", got)
	}
}
