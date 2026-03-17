package postgres

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	startOnce sync.Once
	startErr  error
	container testcontainers.Container
	adminURL  string
)

func NewDatabaseURL(t *testing.T) string {
	t.Helper()

	if !dockerAvailable() {
		t.Skip("Docker is required to run Postgres testcontainers")
	}

	ctx := context.Background()
	if err := ensureContainer(ctx); err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	dbName := "bf_test_" + strings.ToLower(ulid.Make().String())

	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to postgres admin db: %v", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("create test database %s: %v", dbName, err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cleanupConn, err := pgx.Connect(cleanupCtx, adminURL)
		if err != nil {
			t.Fatalf("reconnect to postgres admin db: %v", err)
		}
		defer cleanupConn.Close(cleanupCtx)

		if _, err := cleanupConn.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)"); err != nil {
			t.Fatalf("drop test database %s: %v", dbName, err)
		}
	})

	return fmt.Sprintf("postgres://postgres:postgres@%s/%s?sslmode=disable", hostPort(t), dbName)
}

func Terminate(ctx context.Context) error {
	if container == nil {
		return nil
	}
	err := container.Terminate(ctx)
	container = nil
	return err
}

func ensureContainer(ctx context.Context) error {
	startOnce.Do(func() {
		req := testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER":     "postgres",
				"POSTGRES_PASSWORD": "postgres",
				"POSTGRES_DB":       "postgres",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90 * time.Second),
		}

		var err error
		container, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		if err != nil {
			startErr = err
			return
		}

		host, err := container.Host(ctx)
		if err != nil {
			startErr = err
			return
		}
		port, err := container.MappedPort(ctx, "5432/tcp")
		if err != nil {
			startErr = err
			return
		}

		adminURL = fmt.Sprintf("postgres://postgres:postgres@%s:%s/postgres?sslmode=disable", host, port.Port())
	})

	return startErr
}

func hostPort(t *testing.T) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("get postgres host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("get postgres port: %v", err)
	}
	return host + ":" + port.Port()
}

func dockerAvailable() bool {
	if os.Getenv("DOCKER_HOST") != "" {
		return true
	}
	if _, err := exec.LookPath("docker"); err == nil {
		return true
	}
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".docker/run/docker.sock")); err == nil {
			return true
		}
	}
	return false
}
