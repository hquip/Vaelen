package database

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"vsm/internal/core"
	"vsm/internal/plugin"
)

func freeTestPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestDatabaseDockerIntegration(t *testing.T) {
	if os.Getenv("VSM_TEST_DATABASE_DOCKER") != "1" {
		t.Skip("set VSM_TEST_DATABASE_DOCKER=1 to test real official database images")
	}
	name := os.Getenv("VSM_TEST_DATABASE_ENGINE")
	if name == "" {
		name = "redis"
	}
	pairs := map[string][]string{"postgres": {"17-alpine", "18-alpine"}, "mysql": {"8.0", "8.4"}, "mariadb": {"10.11", "11.4"}, "redis": {"7.2-alpine", "7.4-alpine"}, "valkey": {"7.2-alpine", "8-alpine"}}
	versions, ok := pairs[name]
	if !ok {
		t.Fatal("unsupported test engine")
	}
	paths := core.Paths{Root: t.TempDir()}
	t.Setenv("VSM_HOME", paths.Root)
	m := New(paths)
	m.Progress = func(text string) { t.Log(text) }
	t.Cleanup(func() {
		// Only disposable resources created by this test's random owner are removed.
		i, err := m.load("integration")
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		d := dockerDriver{m}
		for version := range i.Versions {
			copy := *i
			copy.Version = version
			container, err := d.inspect(ctx, &copy)
			if err != nil {
				t.Log(err)
				continue
			}
			if container != nil {
				if err := d.stop(ctx, &copy); err != nil {
					t.Log(err)
					continue
				}
				if _, err := d.command(ctx, []string{"container", "rm", containerName(&copy)}, nil); err != nil {
					t.Log(err)
					continue
				}
			}
			data, err := d.command(ctx, []string{"volume", "inspect", "--format", "{{json .Labels}}", volumeName(&copy)}, nil)
			if err != nil {
				continue
			}
			var labels map[string]string
			if json.Unmarshal(data, &labels) != nil || labels[ownerLabel] != i.Owner {
				continue
			}
			if _, err := d.command(ctx, []string{"volume", "rm", volumeName(&copy)}, nil); err != nil {
				t.Log(err)
			}
		}
	})
	testDatabaseLifecycle(t, m, name, "docker", versions)
}

func TestDatabaseNativeIntegration(t *testing.T) {
	path := os.Getenv("VSM_TEST_NATIVE_DATABASE_PATH")
	if path == "" {
		t.Skip("set VSM_TEST_NATIVE_DATABASE_PATH and VSM_TEST_DATABASE_ENGINE to test an existing server installation")
	}
	name := os.Getenv("VSM_TEST_DATABASE_ENGINE")
	if name == "" {
		name = "mysql"
	}
	paths := core.Paths{Root: t.TempDir()}
	t.Setenv("VSM_HOME", paths.Root)
	p, ok := plugin.Get(name)
	if !ok {
		t.Fatal("missing database plugin")
	}
	for n, source := range []string{path, os.Getenv("VSM_TEST_NATIVE_DATABASE_PATH_2")} {
		if source == "" {
			source = path
		}
		s, err := core.InspectToolPath(name, source)
		if err != nil {
			t.Fatal(err)
		}
		alias := []string{"native-a", "native-b"}[n]
		if err := (core.Installer{Paths: paths}).Adopt(p, alias, s.Path); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s -> actual %s (%s)", alias, s.Version, s.Path)
	}
	m := New(paths)
	m.Progress = func(text string) { t.Log(text) }
	testDatabaseLifecycle(t, m, name, "native", []string{"native-a", "native-b"})
}

func testDatabaseLifecycle(t *testing.T, m *Manager, name, backend string, versions []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	const instance = "integration"
	state, err := m.Use(ctx, Request{Name: instance, Engine: name, Backend: backend, Version: versions[0], Port: freeTestPort(t)})
	if err != nil {
		t.Fatal(err)
	}
	if state.Running {
		t.Fatal("configure unexpectedly starts a service")
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if _, err := m.Stop(stop, instance); err != nil {
			t.Logf("cleanup stop: %v", err)
		}
	})
	state, err = m.Start(ctx, instance)
	if err != nil {
		logs, _ := m.Logs(ctx, instance)
		t.Fatalf("start: %v\n%s", err, logs)
	}
	if !state.Running {
		t.Fatal("server not running")
	}
	connection, err := m.Connection(instance)
	if err != nil || connection.Port != state.Port || connection.Password == "" {
		t.Fatalf("invalid connection: %v", err)
	}
	isRedis := name == "redis" || name == "valkey"
	execute := func(args ...string) string {
		t.Helper()
		text, err := m.Exec(ctx, instance, args)
		if err != nil {
			t.Fatalf("client %v: %v\n%s", args, err, text)
		}
		return strings.TrimSpace(text)
	}
	write := func(value string) {
		t.Helper()
		if isRedis {
			execute("SET", "vsm_probe", value)
			return
		}
		flag := "-e"
		if name == "postgres" {
			flag = "-c"
		}
		execute(flag, "CREATE TABLE IF NOT EXISTS vsm_probe (id INT PRIMARY KEY, value VARCHAR(40)); INSERT INTO vsm_probe VALUES (1, '"+value+"');")
	}
	read := func() string {
		t.Helper()
		if isRedis {
			return execute("GET", "vsm_probe")
		}
		flag := "-Nse"
		if name == "postgres" {
			flag = "-Atc"
		}
		return execute(flag, "SELECT value FROM vsm_probe WHERE id=1")
	}
	write("first-version")
	state, err = m.Switch(ctx, instance, versions[1])
	if err != nil {
		logs, _ := m.Logs(ctx, instance)
		t.Fatalf("switch: %v\n%s", err, logs)
	}
	if !state.Running || state.Version != versions[1] {
		t.Fatal("switch did not select/start requested version")
	}
	// The new version owns a fresh dataset; creating the same primary key succeeds.
	write("second-version")
	if read() != "second-version" {
		t.Fatal("new data not readable")
	}
	state, err = m.Switch(ctx, instance, versions[0])
	if err != nil {
		t.Fatal(err)
	}
	if read() != "first-version" {
		t.Fatal("old version's data was not restored")
	}
	if _, err = m.Switch(ctx, instance, versions[1]); err != nil {
		t.Fatal(err)
	}
	if read() != "second-version" {
		t.Fatal("second version's data was not restored")
	}
	if _, err = m.Switch(ctx, instance, versions[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Stop(ctx, instance); err != nil {
		t.Fatal(err)
	}
	state, err = m.Start(ctx, instance)
	if err != nil {
		t.Fatal(err)
	}
	if read() != "first-version" {
		t.Fatal("stop/start lost database data")
	}
	t.Logf("PASS: %s %s %s -> %s -> %s, actual %s, isolated data preserved on port %s", backend, name, versions[0], versions[1], versions[0], state.Versions[state.Version].ActualVersion, strconv.Itoa(state.Port))
	if backend == "docker" {
		t.Log(fmt.Sprintf("preserved named volumes: %s", state.DataLocation))
	}
}
