package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	qbittorrentBinary = "/usr/trim/bin/trim-qbittorrent-nox"
	realQbitBinary    = "/usr/trim/bin/trim-qbittorrent-nox.real"
)

type wrapperCredential struct {
	PID      int    `json:"pid"`
	Password string `json:"password"`
	UDS      string `json:"uds"`
}

func isWrapperInvocation() bool {
	return argument("--webui-password") != "" && argument("--webui-sock-path") != ""
}

func runWrapper() {
	password := argument("--webui-password")
	uds := argument("--webui-sock-path")
	if strings.Trim(password, "*") == "" {
		fatal("fnOS passed a masked WebUI password; refusing to start")
	}
	socket := envOr("FNOS_QB_CREDENTIAL_SOCKET", defaultCredentialSocket)
	if err := sendWrapperCredential(socket, wrapperCredential{PID: os.Getpid(), Password: password, UDS: uds}); err != nil {
		fatal("send credential: %v", err)
	}
	if err := syscall.Exec(realQbitBinary, os.Args, os.Environ()); err != nil {
		fatal("exec %s: %v", realQbitBinary, err)
	}
}

func installWrapper() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("wrapper installation requires root")
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate proxy binary: %w", err)
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return fmt.Errorf("resolve proxy binary: %w", err)
	}
	if _, err := os.Stat(realQbitBinary); os.IsNotExist(err) {
		if sameBinary(self, qbittorrentBinary) {
			return fmt.Errorf("wrapper is installed but original qBittorrent binary is missing")
		}
		if err := os.Rename(qbittorrentBinary, realQbitBinary); err != nil {
			return fmt.Errorf("backup qBittorrent binary: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("check real qBittorrent binary: %w", err)
	}
	if sameBinary(self, qbittorrentBinary) {
		return nil
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return fmt.Errorf("read proxy binary: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(qbittorrentBinary), ".fnos-qb-wrapper-*")
	if err != nil {
		return fmt.Errorf("create wrapper temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, qbittorrentBinary); err != nil {
		return fmt.Errorf("install wrapper: %w", err)
	}
	return nil
}

func sameBinary(a, b string) bool {
	ai, aerr := os.ReadFile(a)
	bi, berr := os.ReadFile(b)
	if aerr != nil || berr != nil || len(ai) != len(bi) {
		return false
	}
	return sha256.Sum256(ai) == sha256.Sum256(bi)
}

func sendWrapperCredential(path string, c wrapperCredential) error {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(c); err != nil {
		return err
	}
	var response struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return err
	}
	if response.Error != "" {
		return fmt.Errorf("credential service: %s", response.Error)
	}
	if !response.OK {
		return fmt.Errorf("credential service did not acknowledge the password")
	}
	return nil
}

func argument(name string) string {
	prefix := name + "="
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
