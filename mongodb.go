package testdock

import (
	"context"
	"fmt"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// mongo driver name for separating sql and mongo
const mongoDriverName = "mongodb"

// GetMongoDatabase  initializes a test MongoDB database, applies migrations, and returns a database connection.
func GetMongoDatabase(tb testing.TB, dsn string, opt ...Option) (*mongo.Database, Informer) {
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
		WithDockerEnv([]string{
			fmt.Sprintf("MONGO_INITDB_ROOT_USERNAME=%s", url.User),
			fmt.Sprintf("MONGO_INITDB_ROOT_PASSWORD=%s", url.Password),
		}),
	)

	optPrepared = append(optPrepared, opt...)

	tDB := newTDB(ctx, tb, mongoDriverName, dsn, optPrepared)

	client, err := tDB.connectMongoDB(ctx)
	if err != nil {
		tb.Fatalf("cannot connect to mongo: %v", err)
	}

	tb.Cleanup(func() { _ = client.Disconnect(context.Background()) })

	return client.Database(tDB.databaseName), tDB
}

// connectDB connects to MongoDB with retries
func (d *testDB) connectMongoDB(ctx context.Context) (*mongo.Client, error) {
	var (
		client *mongo.Client
		err    error
	)

	err = d.retryConnect(ctx, d.url.string(true), func() error {
		client, err = mongo.Connect(ctx, options.Client().ApplyURI(d.url.string(false)))
		if err != nil {
			return fmt.Errorf("mongo connect: %w", err)
		}

		if err = client.Ping(ctx, nil); err != nil {
			return fmt.Errorf("mongo ping: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("connect mongo url (%s): %w", d.url.string(false), err)
	}

	return client, nil
}
