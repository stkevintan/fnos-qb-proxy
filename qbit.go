package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const defaultCredentialFile = "/home/kevin/.config/fnos-qb-proxy/credential.json"

type Qbit struct {
	password string
	uds      string
	pid      int
}

type credential struct {
	PID      int    `json:"pid"`
	Password string `json:"password"`
	UDS      string `json:"uds"`
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
	if err := q.refresh(); err != nil {
		return "", fmt.Errorf("refresh qbittorrent parameters: %w", err)
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

	path := os.Getenv("FNOS_QB_CREDENTIAL_FILE")
	if path == "" {
		path = defaultCredentialFile
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read credential file %q: %w", path, err)
	}
	var c credential
	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("parse credential file: %w", err)
	}
	if c.PID != pid {
		return fmt.Errorf("credential belongs to pid %d, current qBittorrent pid is %d", c.PID, pid)
	}
	if c.Password == "" || isMasked(c.Password) {
		return fmt.Errorf("credential file contains no usable password")
	}
	q.password = c.Password
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
		content, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		cmdline := strings.ReplaceAll(string(content), "\x00", " ")
		if strings.Contains(cmdline, "trim-qbittorrent-nox") {
			return pid, cmdline, nil
		}
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
