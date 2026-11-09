package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const defaultCredentialSocket = "/run/fnos-qb-proxy/credential.sock"

type Qbit struct {
	password    string
	uds         string
	pid         int
	credentials *credentialStore
}

func NewQbit() *Qbit {
	return &Qbit{}
}

func (q *Qbit) GetPassword() (string, error) {
	if err := q.refresh(); err != nil {
		return "", fmt.Errorf("refresh qbittorrent parameters: %w", err)
	}
	return q.password, nil
}

func (q *Qbit) GetUds() (string, error) {
	pid, cmdline, err := findQbittorrentProcess()
	if err != nil {
		return "", fmt.Errorf("find process: %w", err)
	}
	q.pid = pid
	if q.uds, err = getCmdParams(cmdline, "webui-sock-path"); err != nil {
		return "", fmt.Errorf("get webui-sock-path: %w", err)
	}
	return q.uds, nil
}

// refresh reads the live qBittorrent command line and the password captured by
// the launcher before qBittorrent replaced its argv with masked characters.
func (q *Qbit) refresh() error {
	pid, cmdline, err := findQbittorrentProcess()
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	q.pid = pid
	if q.uds, err = getCmdParams(cmdline, "webui-sock-path"); err != nil {
		return fmt.Errorf("get webui-sock-path: %w", err)
	}

	credentialSocket := os.Getenv("FNOS_QB_CREDENTIAL_SOCKET")
	if credentialSocket == "" {
		credentialSocket = defaultCredentialSocket
	}
	if q.credentials == nil {
		return fmt.Errorf("credential store is not configured")
	}
	password, err := q.credentials.get(pid, q.uds)
	if err != nil {
		return fmt.Errorf("read credential from %q: %w", credentialSocket, err)
	}
	q.password = password
	return nil
}

func findQbittorrentProcess() (int, string, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, "", err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil || (!strings.HasSuffix(exe, "/trim-qbittorrent-nox") && !strings.HasSuffix(exe, "/trim-qbittorrent-nox.real")) {
			continue
		}
		content, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		cmdline := strings.ReplaceAll(string(content), "\x00", " ")
		return pid, cmdline, nil
	}
	return 0, "", fmt.Errorf("qbittorrent-nox process not found")
}

func getCmdParams(cmd string, parameter string) (string, error) {
	re := regexp.MustCompile(fmt.Sprintf(`--%s=(\S+)`, parameter))
	matches := re.FindStringSubmatch(cmd)
	if len(matches) > 1 {
		return matches[1], nil
	}
	return "", fmt.Errorf("parameter --%s not found", parameter)
}

func isMasked(s string) bool {
	return len(s) > 0 && strings.Trim(s, "*") == ""
}
