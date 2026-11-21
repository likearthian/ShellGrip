package main

import (
	"encoding/json"
	"errors"
	"log"
	"strings"

	"github.com/egoist/mygo"
)

// secretStore is the OS keychain surface used by GoRex, swappable in tests.
type secretStore interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(key string) error
}

var keychain secretStore = mygo.Secrets

func sshPasswordKey(c sshConnection) string {
	return "ssh:" + sshRelayKey(c)
}

func loadSSHPassword(c sshConnection) string {
	if keychain == nil {
		return ""
	}
	b, err := keychain.Get(sshPasswordKey(c))
	if err != nil {
		if !errors.Is(err, mygo.ErrSecretNotFound) {
			log.Printf("keychain: get ssh password: %v", err)
		}
		return ""
	}
	return string(b)
}

func saveSSHPassword(c sshConnection, password string) {
	if keychain == nil || password == "" {
		return
	}
	if err := keychain.Set(sshPasswordKey(c), []byte(password)); err != nil {
		log.Printf("keychain: save ssh password: %v", err)
	}
}

func deleteSSHPassword(c sshConnection) {
	if keychain == nil {
		return
	}
	if err := keychain.Delete(sshPasswordKey(c)); err != nil && !errors.Is(err, mygo.ErrSecretNotFound) {
		log.Printf("keychain: delete ssh password: %v", err)
	}
}

func gitCredsKey(dir string) string {
	return "git:" + strings.TrimSpace(dir)
}

func loadGitCredentials(dir string) *gitCredentials {
	if keychain == nil || strings.TrimSpace(dir) == "" {
		return nil
	}
	b, err := keychain.Get(gitCredsKey(dir))
	if err != nil {
		if !errors.Is(err, mygo.ErrSecretNotFound) {
			log.Printf("keychain: get git credentials: %v", err)
		}
		return nil
	}
	var creds gitCredentials
	if err := json.Unmarshal(b, &creds); err != nil || creds.Password == "" {
		return nil
	}
	return &creds
}

func saveGitCredentials(dir string, creds gitCredentials) {
	if keychain == nil || strings.TrimSpace(dir) == "" || creds.Password == "" {
		return
	}
	b, err := json.Marshal(creds)
	if err != nil {
		return
	}
	if err := keychain.Set(gitCredsKey(dir), b); err != nil {
		log.Printf("keychain: save git credentials: %v", err)
	}
}

func deleteGitCredentials(dir string) {
	if keychain == nil || strings.TrimSpace(dir) == "" {
		return
	}
	if err := keychain.Delete(gitCredsKey(dir)); err != nil && !errors.Is(err, mygo.ErrSecretNotFound) {
		log.Printf("keychain: delete git credentials: %v", err)
	}
}
