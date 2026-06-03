package testdock

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

// we ensure the creation of docker resources only once for all tests.
//
//nolint:gochecknoglobals // used to synchronize access to the same database connection string across tests.
var (
	globalDockerMu        sync.Mutex
	globalDockerResources = make(map[string]*dockerResourceInfo)
	globalDockerPool      *dockertest.Pool
)

type dockerResourceInfo struct {
	resource *dockertest.Resource
	port     int
	count    int
	mu       sync.Mutex
}

// createDockerResources create a pool and a resource for creating a test database in docker.
func (d *testDB) createDockerResources(ctx context.Context) error {
	globalDockerMu.Lock()

	info, ok := globalDockerResources[d.dsn]
	if !ok {
		info = &dockerResourceInfo{}
	}

	logDsn := d.dsnNoPass
	if globalDockerPool == nil {
		if err := d.createDockerPoolLocked(ctx); err != nil {
			globalDockerMu.Unlock()
			return err
		}

		defer d.clearDockerPoolWhenUnused(ctx)
	}

	globalDockerMu.Unlock()

	info.mu.Lock()
	defer info.mu.Unlock()

	if info.count > 0 {
		d.url.Port = info.port
		d.logger.Info(ctx, "use existing resources", "component", "docker", "dsn", logDsn)
	} else if err := d.createDockerResource(ctx, info, logDsn); err != nil {
		return err
	}

	globalDockerMu.Lock()
	globalDockerResources[d.dsn] = info
	globalDockerMu.Unlock()

	info.count++
	d.registerDockerResourceCleanup(info, logDsn)

	return nil
}

// createDockerPoolLocked creates the global Docker pool while globalDockerMu is held.
func (d *testDB) createDockerPoolLocked(ctx context.Context) error {
	var err error
	globalDockerPool, err = dockertest.NewPool(d.dockerSocketEndpoint)
	if err != nil {
		return fmt.Errorf("dockertest NewPool: %w", err)
	}

	if d.unsetProxyEnv {
		d.unsetDockerProxyEnv(ctx)
	}

	if err = globalDockerPool.Client.Ping(); err != nil {
		return fmt.Errorf("dockertest ping: %w", err)
	}

	d.logger.Info(ctx, "pool created", "component", "docker")

	return nil
}

// unsetDockerProxyEnv removes proxy variables that can affect Docker client calls.
func (d *testDB) unsetDockerProxyEnv(ctx context.Context) {
	proxyEnv := []string{
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"ALL_PROXY",
		"http_proxy",
		"https_proxy",
		"all_proxy",
	}
	for _, env := range proxyEnv {
		if os.Getenv(env) == "" {
			continue
		}

		d.logger.Info(ctx, "unset proxy env", "component", "docker", "env", env)
		_ = os.Unsetenv(env)
	}
}

// clearDockerPoolWhenUnused clears the global Docker pool if no resources were registered.
func (d *testDB) clearDockerPoolWhenUnused(ctx context.Context) {
	globalDockerMu.Lock()
	defer globalDockerMu.Unlock()

	if len(globalDockerResources) != 0 {
		return
	}

	globalDockerPool = nil
	d.logger.Info(ctx, "pool purged", "component", "docker")
}

// createDockerResource creates a Docker resource and retries while Docker holds the previous port.
func (d *testDB) createDockerResource(ctx context.Context, info *dockerResourceInfo, logDsn string) error {
	const (
		maxAttempts = 10
		sleepTime   = 5 * time.Second
	)

	var (
		attempt    int
		dockerPort = fmt.Sprintf("%d/tcp", d.dockerPort)
		err        error
	)
	for {
		runOptions := &dockertest.RunOptions{ //nolint:exhaustruct // optional SDK fields use zero values.
			Repository: d.dockerRepository,
			Tag:        d.dockerImage,
			Env:        d.dockerEnv,
			PortBindings: map[docker.Port][]docker.PortBinding{
				docker.Port(dockerPort): {{
					HostIP:   d.url.Host,
					HostPort: strconv.Itoa(d.url.Port),
				}},
			},
		}
		info.resource, err = globalDockerPool.RunWithOptions(runOptions, func(config *docker.HostConfig) {
			config.AutoRemove = true
			config.RestartPolicy = docker.RestartPolicy{Name: "no", MaximumRetryCount: 0}
		})
		if err == nil {
			break
		}

		if isDockerBindError(err) {
			d.logger.Info(ctx, "port is already allocated, trying next port", "dsn", logDsn, "next_port", d.url.Port+1)
			d.url.Port++
			continue
		}

		attempt++
		if attempt >= maxAttempts {
			break
		}

		d.logger.Info(ctx, "RunWithOptions failed", "component", "docker", "dsn", logDsn, "attempt", attempt, "error", err)
		time.Sleep(sleepTime)
	}

	if err != nil {
		return fmt.Errorf("dockertest RunWithOptions: %w", err)
	}

	info.port = d.url.Port
	d.logger.Info(ctx, "resources created", "component", "docker", "dsn", logDsn)

	return nil
}

// isDockerBindError checks errors reported when a Docker port is already allocated.
func isDockerBindError(err error) bool {
	bindErrors := []string{
		"address already in use",
		"port is already allocated",
		"failed to bind host port",
	}
	for _, bindError := range bindErrors {
		if strings.Contains(err.Error(), bindError) {
			return true
		}
	}

	return false
}

// registerDockerResourceCleanup removes the shared Docker resource after the last user test.
func (d *testDB) registerDockerResourceCleanup(info *dockerResourceInfo, logDsn string) {
	d.t.Cleanup(func() {
		cleanupCtx := context.Background()

		info.mu.Lock()
		defer info.mu.Unlock()
		info.count--

		if info.count != 0 {
			return
		}

		globalDockerMu.Lock()
		defer globalDockerMu.Unlock()

		delete(globalDockerResources, d.dsn)
		d.purgeDockerResource(cleanupCtx, info, logDsn)
	})
}

// purgeDockerResource purges the Docker resource with retries.
func (d *testDB) purgeDockerResource(ctx context.Context, info *dockerResourceInfo, logDsn string) {
	const (
		maxTime      = 10 * time.Second
		retryTimeout = 1 * time.Second
	)
	var attempt int

	operation := func() (struct{}, error) {
		if purgeErr := globalDockerPool.Purge(info.resource); purgeErr != nil {
			attempt++
			d.logger.Info(ctx, "purge attempt failed",
				"component", "docker", "dsn", logDsn, "attempt", attempt, "error", purgeErr)
			return struct{}{}, purgeErr
		}
		return struct{}{}, nil
	}

	if _, retryErr := backoff.Retry(ctx, operation,
		backoff.WithBackOff(backoff.NewConstantBackOff(retryTimeout)),
		backoff.WithMaxElapsedTime(maxTime)); retryErr != nil {
		d.logger.Info(ctx, "purge failed after retries",
			"component", "docker", "dsn", logDsn, "attempt", attempt, "error", retryErr)
		return
	}

	d.logger.Info(ctx, "resources purged successfully", "component", "docker", "dsn", logDsn, "attempts", attempt)
}
