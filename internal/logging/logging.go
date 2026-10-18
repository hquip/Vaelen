// Package logging stores bounded, private JSONL diagnostics shared by all VSM
// frontends. Recording is best effort: it never writes to stdout or stderr.
package logging

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vsm/internal/buildinfo"
)

const (
	fileName      = "vsm.jsonl"
	maxEntryBytes = 16 << 10
	MaxTail       = 1000
)

type Fields map[string]any

type Entry struct {
	Time      string `json:"time"`
	Level     string `json:"level"`
	Component string `json:"component"`
	Event     string `json:"event"`
	PID       int    `json:"pid"`
	Version   string `json:"version"`
	Fields    Fields `json:"fields,omitempty"`
}

type File struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

type Snapshot struct {
	Directory string  `json:"directory"`
	Level     string  `json:"level"`
	Files     []File  `json:"files"`
	Entries   []Entry `json:"entries"`
}

// Store's directory is always global, including when the active project uses
// local or Docker isolation. Fields are private to keep rotation limits fixed.
type Store struct {
	dir      string
	maxBytes int64
	backups  int
}

func Default() (Store, error) {
	root := os.Getenv("VSM_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Store{}, err
		}
		root = filepath.Join(home, ".vsm")
	}
	dir, err := filepath.Abs(filepath.Join(root, "logs"))
	return Store{dir: dir, maxBytes: 2 << 20, backups: 3}, err
}

func (s Store) Directory() string { return s.dir }

func priority(level string) int {
	switch level {
	case "debug":
		return 0
	case "info":
		return 1
	case "warn":
		return 2
	case "error":
		return 3
	case "off":
		return 4
	default:
		return -1
	}
}

func Level() string {
	level := strings.ToLower(strings.TrimSpace(os.Getenv("VSM_LOG_LEVEL")))
	if priority(level) < 0 {
		return "info"
	}
	return level
}

func ValidFilter(level string) bool { return level == "" || (priority(level) >= 0 && level != "off") }

// Record deliberately ignores filesystem failures. Log-management commands
// return those errors explicitly without changing the operation being logged.
func Record(level, component, event string, fields Fields) {
	if priority(level) < 0 || level == "off" || priority(level) < priority(Level()) {
		return
	}
	s, err := Default()
	if err == nil {
		_ = s.write(level, component, event, fields)
	}
}

// Start returns an operation finisher. Callers must pass the actual final error.
// Never pass command arguments, environment dumps, subprocess output or MCP
// request/result bodies as fields.
func Start(component, event string, fields Fields) func(error) {
	start := time.Now()
	Record("info", component, event+".started", fields)
	return func(err error) {
		result := make(Fields, len(fields)+2)
		for k, v := range fields {
			result[k] = v
		}
		result["duration_ms"] = time.Since(start).Milliseconds()
		level, status := "info", "completed"
		if err != nil {
			level, status = "error", "failed"
			result["error"] = err.Error()
		}
		Record(level, component, event+"."+status, result)
	}
}

func safeFields(fields Fields) Fields {
	result := make(Fields, len(fields))
	for k, v := range fields {
		if sensitiveKey(k) {
			result[k] = "[REDACTED]"
			continue
		}
		switch v := v.(type) {
		case string:
			result[k] = Redact(v)
		case bool, int, int64, float64:
			result[k] = v
		default:
			result[k] = "[omitted]"
		}
	}
	return result
}

func (s Store) write(level, component, event string, fields Fields) error {
	entry := Entry{Time: time.Now().UTC().Format(time.RFC3339Nano), Level: level,
		Component: component, Event: event, PID: os.Getpid(), Version: buildinfo.DisplayVersion(), Fields: safeFields(fields)}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if len(data) > maxEntryBytes {
		entry.Fields = Fields{"truncated": true}
		data, err = json.Marshal(entry)
		if err != nil {
			return err
		}
	}
	data = append(data, '\n')
	return s.locked(func(root *os.Root) error {
		if fi, err := root.Stat(fileName); err == nil && fi.Size()+int64(len(data)) > s.maxBytes {
			if err := s.rotate(root); err != nil {
				return err
			}
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		f, err := root.OpenFile(fileName, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}

		// A killed process can leave a partial JSON record. Separate it from the
		// next complete entry so a crash cannot swallow the following event.
		fi, statErr := f.Stat()
		if statErr != nil {
			f.Close()
			return statErr
		}
		if fi.Size() > 0 {
			var last [1]byte
			if _, err := f.ReadAt(last[:], fi.Size()-1); err != nil {
				f.Close()
				return err
			}
			if last[0] != '\n' {
				data = append([]byte{'\n'}, data...)
			}
		}
		_, writeErr := f.Write(data)
		return errors.Join(writeErr, f.Close())
	})
}

// locked uses an OS lock shared across CLI processes and the GUI. Locks are
// released by the kernel even if VSM crashes; no stale lock cleanup is needed.
func (s Store) locked(fn func(*os.Root) error) error {
	if err := os.MkdirAll(filepath.Dir(s.dir), 0o700); err != nil {
		return err
	}
	parent, err := os.OpenRoot(filepath.Dir(s.dir))
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := parent.Mkdir("logs", 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	if err := regularDirectory(parent, "logs"); err != nil {
		return err
	}
	root, err := parent.OpenRoot("logs")
	if err != nil {
		return err
	}
	defer root.Close()
	if err := regularFile(root, ".lock"); err != nil {
		return err
	}
	f, err := root.OpenFile(".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	unlock, err := lockFile(f)
	if err != nil {
		return err
	}
	defer unlock()
	for _, name := range s.names() {
		if err := regularFile(root, name); err != nil {
			return err
		}
	}
	return fn(root)
}

func regularDirectory(root *os.Root, name string) error {
	fi, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("日志目录不能是链接: %s", name)
	}
	if _, err := root.Readlink(name); err == nil {
		return fmt.Errorf("日志目录不能是链接: %s", name)
	}
	return nil
}

func regularFile(root *os.Root, name string) error {
	fi, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("日志文件不能是链接或特殊文件: %s", name)
	}
	return nil
}

// names returns oldest first, so concatenation preserves the rotation order.
func (s Store) names() []string {
	names := make([]string, 0, s.backups+1)
	for i := s.backups; i > 0; i-- {
		names = append(names, fmt.Sprintf("%s.%d", fileName, i))
	}
	return append(names, fileName)
}

func (s Store) rotate(root *os.Root) error {
	if err := root.Remove(fmt.Sprintf("%s.%d", fileName, s.backups)); err != nil && !os.IsNotExist(err) {
		return err
	}
	for i := s.backups - 1; i >= 0; i-- {
		old := fileName
		if i > 0 {
			old = fmt.Sprintf("%s.%d", fileName, i)
		}
		if err := root.Rename(old, fmt.Sprintf("%s.%d", fileName, i+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s Store) Read(tail int, level string) (Snapshot, error) {
	if tail < 1 || tail > MaxTail || !ValidFilter(level) {
		return Snapshot{}, fmt.Errorf("tail 必须为 1–%d，level 为 debug/info/warn/error", MaxTail)
	}
	return s.read(tail, level)
}

func (s Store) read(tail int, level string) (Snapshot, error) {
	out := Snapshot{Directory: s.dir, Level: Level(), Files: []File{}, Entries: []Entry{}}
	err := s.locked(func(root *os.Root) error {
		for _, name := range s.names() {
			f, err := root.Open(name)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			fi, err := f.Stat()
			if err != nil {
				f.Close()
				return err
			}
			out.Files = append(out.Files, File{name, fi.Size()})
			// Bound reads even if an external process replaced a log with a huge file.
			if fi.Size() > s.maxBytes+maxEntryBytes {
				f.Close()
				return fmt.Errorf("日志文件过大: %s", name)
			}
			scan := bufio.NewScanner(f)
			scan.Buffer(make([]byte, maxEntryBytes), maxEntryBytes+1)
			for scan.Scan() {
				var entry Entry
				if json.Unmarshal(scan.Bytes(), &entry) != nil {
					continue
				} // tolerate a crash-truncated last record
				if priority(entry.Level) < priority(level) {
					continue
				}
				entry.Fields = safeFields(entry.Fields)
				out.Entries = append(out.Entries, entry)
			}
			err = errors.Join(scan.Err(), f.Close())
			if err != nil {
				return err
			}
		}
		return nil
	})
	if tail > 0 && len(out.Entries) > tail {
		out.Entries = out.Entries[len(out.Entries)-tail:]
	}
	return out, err
}

// Export writes all retained entries to a NEW file, so exporting cannot
// overwrite executable/configuration files or an active log, even via links.
func (s Store) Export(dest string) error {
	data, err := s.read(0, "")
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return err
	}
	logDir, err := filepath.EvalSymlinks(s.dir)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(parent); err == nil {
		if li, err := os.Stat(logDir); err == nil && os.SameFile(fi, li) {
			return fmt.Errorf("请导出到日志目录之外")
		}
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("导出失败，请选择不存在的新文件: %w", err)
	}
	enc := json.NewEncoder(f)
	for _, entry := range data.Entries {
		if err := enc.Encode(entry); err != nil {
			f.Close()
			os.Remove(abs)
			return err
		}
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		os.Remove(abs)
		return err
	}
	return nil
}

// Clear only removes VSM's known log files; it never traverses directories or
// deletes the lock file (which would invalidate another process's lock).
func (s Store) Clear() error {
	return s.locked(func(root *os.Root) error {
		for _, name := range s.names() {
			if err := root.Remove(name); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	})
}
