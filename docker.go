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

// we ensure the creation of docker resources only once for all tests
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
func (d *testDB) createDockerResources(ctx context.Context) error { //nolint:gocognit // ok
	globalDockerMu.Lock()

	info, ok := globalDockerResources[d.dsn]
	if !ok {
		info = &dockerResourceInfo{}
	}

	var (
		err    error
		logDsn = d.dsnNoPass
	)
	if globalDockerPool == nil {
		globalDockerPool, err = dockertest.NewPool(d.dockerSocketEndpoint)
		if err != nil {
			globalDockerMu.Unlock()
			return fmt.Errorf("dockertest NewPool: %w", err)
		}

		if d.unsetProxyEnv {
			// we clear the proxy environment variables, because they can interfere with the work of docker
			proxyEnv := []string{
				"HTTP_PROXY",
				"HTTPS_PROXY",
				"ALL_PROXY",
				"http_proxy",
				"https_proxy",
				"all_proxy",
			}
			for _, env := range proxyEnv {
				if os.Getenv(env) != "" {
					d.logger.Info(ctx, "unset proxy env", "component", "docker", "env", env)
					_ = os.Unsetenv(env)
				}
			}
		}

		err = globalDockerPool.Client.Ping()
		if err != nil {
			globalDockerMu.Unlock()
			return fmt.Errorf("dockertest ping: %w", err)
		}

		d.logger.Info(ctx, "pool created", "component", "docker")

		defer func() {
			globalDockerMu.Lock()
			defer globalDockerMu.Unlock()

			if len(globalDockerResources) == 0 {
				globalDockerPool = nil
				d.logger.Info(ctx, "pool purged", "component", "docker")
			}
		}()
	}

	globalDockerMu.Unlock()

	info.mu.Lock()
	defer info.mu.Unlock()

	if info.count == 0 {
		// docker releases the port after calling globalDockerPool.Purge(globalDockerResource) not instantly, so we try several times
		const (
			maxAttempts = 10
			sleepTime   = 5 * time.Second
		)

		var (
			attempt    int
			dockerPort = fmt.Sprintf("%d/tcp", d.dockerPort)
		)
		for {
			info.resource, err = globalDockerPool.RunWithOptions(&dockertest.RunOptions{
				Repository: d.dockerRepository,
				Tag:        d.dockerImage,
				Env:        d.dockerEnv,
				PortBindings: map[docker.Port][]docker.PortBinding{
					docker.Port(dockerPort): {{
						HostIP:   d.url.Host,
						HostPort: strconv.Itoa(d.url.Port),
					}},
				},
			}, func(config *docker.HostConfig) {
				config.AutoRemove = true
				config.RestartPolicy = docker.RestartPolicy{Name: "no"}
			})

			if err == nil {
				break
			}

			bindErrors := []string{
				"bind: address already in use",
				"port is already allocated",
			}
			needNextPort := false
			for _, bindError := range bindErrors {
				if strings.Contains(err.Error(), bindError) {
					needNextPort = true
					break
				}
			}
			if needNextPort {
				// increase hostPort by 1
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
	} else {
		d.url.Port = info.port // restore port

		d.logger.Info(ctx, "use existing resources", "component", "docker", "dsn", logDsn)
	}

	globalDockerMu.Lock()
	globalDockerResources[d.dsn] = info
	globalDockerMu.Unlock()

	info.count++

	d.t.Cleanup(func() {
		ctx := context.Background()

		info.mu.Lock()
		defer info.mu.Unlock()
		info.count--

		if info.count == 0 {
			globalDockerMu.Lock()
			defer globalDockerMu.Unlock()

			delete(globalDockerResources, d.dsn)

			const (
				maxTime      = 10 * time.Second
				retryTimeout = 1 * time.Second
			)
			var attempt int

			operation := func() (struct{}, error) {
				if err := globalDockerPool.Purge(info.resource); err != nil {
					attempt++
					// Closure needs access to context, so we'll pass background context since this is a cleanup function
					d.logger.Info(ctx, "purge attempt failed", "component", "docker", "dsn", logDsn, "attempt", attempt, "error", err)
					return struct{}{}, err
				}
				return struct{}{}, nil
			}

			if _, err := backoff.Retry(ctx, operation,
				backoff.WithBackOff(backoff.NewConstantBackOff(retryTimeout)),
				backoff.WithMaxElapsedTime(maxTime)); err != nil {
				d.logger.Info(ctx, "purge failed after retries", "component", "docker", "dsn", logDsn, "attempt", attempt, "error", err)
			} else {
				d.logger.Info(ctx, "resources purged successfully", "component", "docker", "dsn", logDsn, "attempts", attempt)
			}
		}
	})

	return nil
}
