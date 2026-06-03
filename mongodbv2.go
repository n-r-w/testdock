package testdock

import (
	"context"
	"fmt"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mongo driver name for separating sql and mongo.
const mongoDriverName = "mongodb"

// GetMongoDatabaseV2 initializes a test MongoDB database, applies migrations, and returns a database connection.
//
//nolint:dupl // similar code, but with different drivers and options.
func GetMongoDatabaseV2(tb testing.TB, dsn string, opt ...Option) (*mongo.Database, Informer) {
	tb.Helper()

	ctx := context.Background()

	url, err := parseURL(dsn)
	if err != nil {
		tb.Fatalf("failed to parse dsn: %v", err)
	}

	optPrepared := make([]Option, 0, len(opt))
	optPrepared = append(optPrepared,
		WithDockerRepository("mongo"),
		WithDockerImage("latest"),
	)
	if url.User != "" {
		optPrepared = append(optPrepared,
			WithDockerEnv([]string{
				fmt.Sprintf("MONGO_INITDB_ROOT_USERNAME=%s", url.User),
				fmt.Sprintf("MONGO_INITDB_ROOT_PASSWORD=%s", url.Password),
			}))
	}

	optPrepared = append(optPrepared, opt...)

	tDB := newTDB(ctx, tb, mongoDriverName, dsn, optPrepared)

	client, err := tDB.connectMongoDBv2(ctx)
	if err != nil {
		tb.Fatalf("cannot connect to mongo: %v", err)
	}

	tb.Cleanup(func() {
		if tDB.mode != RunModeDocker {
			// protect against closing connection during tests
			clientClean, connectErr := tDB.connectMongoDBv2(ctx)
			if connectErr != nil {
				tb.Logf("cannot connect to mongo for cleanup: %v", connectErr)
				return
			}
			defer func() {
				if disconnectErr := clientClean.Disconnect(ctx); disconnectErr != nil {
					tb.Logf("cannot disconnect mongo cleanup client: %v", disconnectErr)
				}
			}()

			dbClean := clientClean.Database(tDB.databaseName)
			if dropErr := dbClean.Drop(ctx); dropErr != nil {
				tb.Logf("failed to drop database %s: %v", tDB.databaseName, dropErr)
			}
		}

		_ = client.Disconnect(context.Background())
	})

	return client.Database(tDB.databaseName), tDB
}

// connectMongoDBv2 connects to MongoDB with retries.
func (d *testDB) connectMongoDBv2(ctx context.Context) (*mongo.Client, error) {
	var (
		client *mongo.Client
		err    error
	)

	url := d.url.replaceDatabase(d.databaseName)

	err = d.retryConnect(ctx, url.string(true), func() error {
		client, err = mongo.Connect(options.Client().ApplyURI(url.string(false)))
		if err != nil {
			return fmt.Errorf("mongo connect: %w", err)
		}

		if err = client.Ping(ctx, nil); err != nil {
			return fmt.Errorf("mongo ping: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("connect mongo url (%s): %w", url.string(false), err)
	}

	return client, nil
}
