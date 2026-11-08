// Command fnos-qb-wrapper captures fnOS's generated qBittorrent password
// before replacing itself with the original qBittorrent executable.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	defaultRealBinary = "/usr/trim/bin/trim-qbittorrent-nox.real"
	defaultCredential = "/run/fnos-qb-proxy/credential.json"
)

type credential struct {
	PID      int    `json:"pid"`
	Password string `json:"password"`
	UDS      string `json:"uds"`
}

func main() {
	password := argument("--webui-password")
	uds := argument("--webui-sock-path")
	if password == "" || uds == "" {
		fatal("required qBittorrent arguments are missing")
	}
	if strings.Trim(password, "*") == "" {
		fatal("fnOS passed a masked WebUI password; refusing to start")
	}

	path := envOr("FNOS_QB_CREDENTIAL_FILE", defaultCredential)
	if err := writeCredential(path, credential{PID: os.Getpid(), Password: password, UDS: uds}); err != nil {
		fatal("write credential: %v", err)
	}

	realBinary := envOr("FNOS_QB_REAL_BINARY", defaultRealBinary)
	if err := syscall.Exec(realBinary, os.Args, os.Environ()); err != nil {
		fatal("exec %s: %v", realBinary, err)
	}
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

func writeCredential(path string, c credential) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credential-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
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
	return os.Rename(tmpName, path)
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
