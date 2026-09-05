package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

var ErrConflict = errors.New("configuration changed in another process; retry the command")

const journalName = ".transaction.json"

type transaction struct {
	Config      []byte `json:"config"`
	Credentials []byte `json:"credentials"`
}

// Locks cover disk operations, never network requests or interactive prompts.
func withStoreLock(p Paths, action func() error) error {
	if err := ensureDir(p.ConfigDir); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(p.ConfigDir, ".lock"))
	defer lock.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	locked, err := lock.TryLockContext(ctx, 20*time.Millisecond)
	if err != nil {
		return fmt.Errorf("lock configuration: %w", err)
	}
	if !locked {
		return fmt.Errorf("configuration is busy; retry the command")
	}
	defer lock.Unlock()
	if err := recoverTransaction(p); err != nil {
		return err
	}
	return action()
}

// LoadState obtains a consistent pair, including recovery of interrupted commits.
func LoadState(p Paths) (cfg *Config, creds *Credentials, repaired bool, err error) {
	err = withStoreLock(p, func() error {
		var loadErr error
		cfg, loadErr = loadConfig(p)
		if loadErr != nil {
			return loadErr
		}
		creds, repaired, loadErr = loadCredentials(p)
		return loadErr
	})
	return
}

// SaveState commits both snapshots together, rejecting stale readers before writing.
func SaveState(cfg *Config, creds *Credentials) error {
	if cfg.transient && creds.transient {
		return nil
	}
	if cfg.transient || creds.transient || cfg.paths.ConfigDir != creds.paths.ConfigDir {
		return fmt.Errorf("configuration and credentials belong to different stores")
	}
	return saveState(cfg, creds)
}

func snapshotMatches(path string, snapshot []byte) error {
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !bytes.Equal(current, snapshot) {
		return ErrConflict
	}
	return nil
}

func marshalSnapshot(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func saveState(cfg *Config, creds *Credentials) error {
	var paths Paths
	if cfg != nil {
		paths = cfg.paths
	} else {
		paths = creds.paths
	}
	return withStoreLock(paths, func() error {
		var configData, credentialsData []byte
		var err error
		if cfg != nil {
			if err = snapshotMatches(paths.ConfigFile(), cfg.snapshot); err != nil {
				return err
			}
			if configData, err = marshalSnapshot(cfg); err != nil {
				return err
			}
		}
		if creds != nil {
			if err = snapshotMatches(paths.CredentialsFile(), creds.snapshot); err != nil {
				return err
			}
			if credentialsData, err = marshalSnapshot(creds); err != nil {
				return err
			}
		}
		switch {
		case cfg != nil && creds != nil:
			journal, err := marshalSnapshot(transaction{Config: configData, Credentials: credentialsData})
			if err != nil {
				return err
			}
			// The journal is the commit point; recovery rolls both files forward.
			if err = writeAtomic(filepath.Join(paths.ConfigDir, journalName), journal, credsFilePerm); err != nil {
				return err
			}
			if err = recoverTransaction(paths); err != nil {
				return err
			}
		case cfg != nil:
			if err = writeAtomic(paths.ConfigFile(), configData, configFilePerm); err != nil {
				return err
			}
		default:
			if err = writeAtomic(paths.CredentialsFile(), credentialsData, credsFilePerm); err != nil {
				return err
			}
		}
		if cfg != nil {
			cfg.snapshot = configData
		}
		if creds != nil {
			creds.snapshot = credentialsData
		}
		return nil
	})
}

func recoverTransaction(p Paths) error {
	path := filepath.Join(p.ConfigDir, journalName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var tx transaction
	if err := json.Unmarshal(data, &tx); err != nil {
		return fmt.Errorf("recover configuration transaction: %w", err)
	}
	if !json.Valid(tx.Config) || !json.Valid(tx.Credentials) {
		return fmt.Errorf("invalid configuration transaction")
	}
	if err := writeAtomic(p.CredentialsFile(), tx.Credentials, credsFilePerm); err != nil {
		return err
	}
	if err := writeAtomic(p.ConfigFile(), tx.Config, configFilePerm); err != nil {
		return err
	}
	return os.Remove(path)
}
