package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Qbit struct {
	password  string
	passwords []string
	uds       string
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

// GetPasswords returns all candidate passwords, the first one is the most likely
func (q *Qbit) GetPasswords() ([]string, error) {
	if err := q.refresh(); err != nil {
		return nil, fmt.Errorf("refresh qbittorrent parameters: %w", err)
	}
	return q.passwords, nil
}

func (q *Qbit) GetUds() (string, error) {
	// uds should not be changed once set
	if q.uds != "" {
		return q.uds, nil
	}
	if err := q.refresh(); err != nil {
		return "", fmt.Errorf("refresh qbittorrent parameters: %w", err)
	}
	return q.uds, nil
}

// refresh checks if the qbittorrent-nox process is running and updates password and uds path
func (q *Qbit) refresh() error {
	fmt.Printf("refreshing qbittorrent-nox parameters\n")
	cmdline, err := findQbittorrentProcess()
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	password, err := getCmdParams(cmdline, "webui-password")
	if err != nil {
		return fmt.Errorf("get webui-password: %w", err)
	}

	// newer fnOS versions rewrite --webui-password in place with '*' after startup,
	// so the plaintext has to be recovered from the download center process instead
	if isMasked(password) {
		passwords, err := findPasswordFromDlcenter()
		if err != nil {
			return fmt.Errorf("recover masked webui-password: %w", err)
		}
		q.passwords = passwords
	} else {
		q.passwords = []string{password}
	}
	q.password = q.passwords[0]

	if q.uds, err = getCmdParams(cmdline, "webui-sock-path"); err != nil {
		return fmt.Errorf("get webui-sock-path: %w", err)
	}
	return nil
}

func findQbittorrentProcess() (string, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		content, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		// cmdline arguments are separated by null bytes
		cmdline := strings.ReplaceAll(string(content), "\x00", " ")
		if strings.Contains(cmdline, "trim-qbittorrent-nox") {
			return cmdline, nil
		}
	}
	return "", fmt.Errorf("qbittorrent-nox process not found")
}

func getCmdParams(cmd string, parameter string) (string, error) {
	// parse output(likes --webui-password=xxx) to get
	re := regexp.MustCompile(fmt.Sprintf(`--%s=(\S+)`, parameter))
	matches := re.FindStringSubmatch(cmd)
	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("no qbittorrent-nox process found")
}

const (
	// qBittorrent masks --webui-password in its own argv, e.g. --webui-password=********
	maskChar = "*"
	// the body sent by the download center when it logs in to qBittorrent
	loginBodyPrefix = "username=admin&password="
	maxPasswordLen  = 64
	maxMapSize      = 512 << 20
	scanChunk       = 1 << 20
	passwordWait    = 5 * time.Second
	maxCandidates   = 3
)

// isMasked reports whether s looks like a password rewritten to '*' by qBittorrent
func isMasked(s string) bool {
	return len(s) > 0 && strings.Trim(s, maskChar) == ""
}

// findPasswordFromDlcenter recovers the qBittorrent WebUI password by looking for the
// login request the download center sends over the unix socket. The download center
// generates the password on every qBittorrent start, so it is only kept in its memory.
func findPasswordFromDlcenter() ([]string, error) {
	pid, err := findProcessByExe("dlcenter")
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(passwordWait)
	for {
		passwords, err := scanProcessMemory(pid, loginBodyPrefix)
		if err != nil {
			return nil, err
		}
		if len(passwords) > 0 {
			if len(passwords) > maxCandidates {
				passwords = passwords[:maxCandidates]
			}
			return passwords, nil
		}
		// the download center may not have logged in yet right after startup
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("no login request found in dlcenter (pid %d) memory, proxy needs root and host pid namespace", pid)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func findProcessByExe(name string) (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil {
			continue
		}
		exe = strings.TrimSuffix(exe, " (deleted)")
		if strings.HasSuffix(exe, "/"+name) {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("%s process not found", name)
}

// scanProcessMemory scans the readable memory of pid for prefix and returns the
// printable strings that follow it, in the order they are found.
func scanProcessMemory(pid int, prefix string) ([]string, error) {
	mapsData, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return nil, fmt.Errorf("read maps: %w", err)
	}
	mem, err := os.Open(fmt.Sprintf("/proc/%d/mem", pid))
	if err != nil {
		return nil, fmt.Errorf("open memory: %w", err)
	}
	defer mem.Close()

	needle := []byte(prefix)
	var passwords []string
	seen := make(map[string]bool)

	for _, line := range strings.Split(string(mapsData), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(fields[1], "r") {
			continue
		}
		startStr, endStr, ok := strings.Cut(fields[0], "-")
		if !ok {
			continue
		}
		start, errStart := strconv.ParseInt(startStr, 16, 64)
		end, errEnd := strconv.ParseInt(endStr, 16, 64)
		if errStart != nil || errEnd != nil || end <= start || end-start > maxMapSize {
			continue
		}

		scanRegion(mem, start, end, needle, func(password string) {
			if !seen[password] {
				seen[password] = true
				passwords = append(passwords, password)
			}
		})
	}
	return passwords, nil
}

func scanRegion(mem *os.File, start, end int64, needle []byte, found func(string)) {
	// keep a tail so that matches spanning two chunks are not missed
	overlap := len(needle) + maxPasswordLen
	buf := make([]byte, scanChunk+overlap)
	pos := start
	for pos < end {
		read := int64(len(buf))
		if end-pos < read {
			read = end - pos
		}
		n, err := mem.ReadAt(buf[:read], pos)
		if n > 0 {
			data := buf[:n]
			for idx := 0; idx < len(data); {
				i := indexOf(data[idx:], needle)
				if i < 0 {
					break
				}
				idx += i + len(needle)
				if password := readPrintable(data[idx:]); password != "" {
					found(password)
				}
			}
		}
		if err != nil && err != io.EOF {
			return
		}
		advance := int64(n) - int64(overlap)
		if advance <= 0 {
			return
		}
		pos += advance
	}
}

func indexOf(data, sub []byte) int {
	return strings.Index(string(data), string(sub))
}

func readPrintable(data []byte) string {
	end := 0
	for end < len(data) && end < maxPasswordLen {
		c := data[end]
		if c == 0 || c < 0x21 || c > 0x7e {
			break
		}
		end++
	}
	return string(data[:end])
}
