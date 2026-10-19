package database

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDatabaseProcessHelper(t *testing.T) {
	switch os.Getenv("VSM_DATABASE_TEST_HELPER") {
	case "inspect":
		_, present := os.LookupEnv("PGSERVICE")
		fmt.Printf("service=%t options=%s\n", present, os.Getenv("PGOPTIONS"))
	case "stdout":
		fmt.Println("query-row")
		fmt.Fprintln(os.Stderr, "nonfatal client warning")
	case "error":
		fmt.Fprintln(os.Stderr, "rejected password "+os.Getenv("PGPASSWORD"))
		os.Exit(2)
	default:
		return
	}
	os.Exit(0)
}
func TestPostgresClientClearsServiceEnvironment(t *testing.T) {
	m, _ := fakeManager(t)
	t.Setenv("PGSERVICE", "")
	t.Setenv("PGOPTIONS", "inherited-options")
	_, env := clientSettings(&Instance{Engine: "postgres"}, credential{Password: "test"}, 5432)
	env["VSM_DATABASE_TEST_HELPER"] = "inspect"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := m.run(ctx, os.Args[0], []string{"-test.run=^TestDatabaseProcessHelper$"}, env)
	if err != nil || !strings.Contains(string(out), "service=false options=\n") {
		t.Fatalf("inherited or empty PGSERVICE reached client: %q %v", out, err)
	}
}
func TestQueryOutputExcludesWarningsAndErrorRedactsCredentials(t *testing.T) {
	m, _ := fakeManager(t)
	configureTest(t, m)
	i, _ := m.load("dev")
	c, err := m.credentials(i)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := m.run(ctx, os.Args[0], []string{"-test.run=^TestDatabaseProcessHelper$"}, map[string]string{"VSM_DATABASE_TEST_HELPER": "stdout"})
	if err != nil || strings.TrimSpace(string(out)) != "query-row" {
		t.Fatalf("stderr was mixed into query rows: %q %v", out, err)
	}
	_, err = m.run(ctx, os.Args[0], []string{"-test.run=^TestDatabaseProcessHelper$"}, map[string]string{"VSM_DATABASE_TEST_HELPER": "error", "PGPASSWORD": c.Password})
	if err == nil || strings.Contains(err.Error(), c.Password) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("credential error not redacted: %v", err)
	}
}
