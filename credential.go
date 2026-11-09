package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"sync"
)

type credential struct {
	PID      int    `json:"pid"`
	Password string `json:"password"`
	UDS      string `json:"uds"`
	OK       bool   `json:"ok,omitempty"`
	Error    string `json:"error,omitempty"`
}

type credentialStore struct {
	mu    sync.RWMutex
	items map[int]credential
}

func newCredentialStore() *credentialStore {
	return &credentialStore{items: make(map[int]credential)}
}

func (s *credentialStore) set(c credential) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[c.PID] = c
}

func (s *credentialStore) get(pid int, uds string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.items[pid]
	if !ok {
		return "", fmt.Errorf("no credential received for qBittorrent pid %d; restart qBittorrent after proxy restart", pid)
	}
	if c.UDS != uds {
		return "", fmt.Errorf("credential socket path does not match qBittorrent")
	}
	return c.Password, nil
}

func (s *credentialStore) serve(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(path, 0660); err != nil {
		return err
	}
	if group := os.Getenv("FNOS_QB_CREDENTIAL_GROUP"); group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			return fmt.Errorf("lookup credential group: %w", err)
		}
		gid, err := strconv.Atoi(g.Gid)
		if err != nil {
			return fmt.Errorf("parse credential group id: %w", err)
		}
		if err := os.Chown(path, -1, gid); err != nil {
			return fmt.Errorf("set credential socket group: %w", err)
		}
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *credentialStore) handle(conn net.Conn) {
	defer conn.Close()
	var c credential
	if err := json.NewDecoder(conn).Decode(&c); err != nil {
		return
	}
	if c.PID <= 0 || c.UDS == "" {
		return
	}
	if c.Password != "" {
		if isMasked(c.Password) {
			return
		}
		s.set(c)
		_ = json.NewEncoder(conn).Encode(credential{OK: true})
		return
	}
	password, err := s.get(c.PID, c.UDS)
	if err != nil {
		_ = json.NewEncoder(conn).Encode(credential{Error: err.Error()})
		return
	}
	_ = json.NewEncoder(conn).Encode(credential{Password: password})
}
