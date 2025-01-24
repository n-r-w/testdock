package testdock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
)

func TestMongoDB(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create test database and return a MongoDB client
	db, informer := GetMongoDatabase(t,
		DefaultMongoDSN,
		WithDockerRepository("mongo"),
		WithDockerImage("6.0.20"),
		WithMigrations("migrations/mongodb", GolangMigrateFactory),
	)

	checkInformer(t, DefaultMongoDSN, informer)

	// Test collection
	collection := db.Collection("test_collection")

	// Insert duplicate document
	_, err := collection.InsertOne(ctx,
		bson.M{
			"_id":  "test1",
			"name": "test1",
		})
	require.Error(t, err)

	// Insert new document
	_, err = collection.InsertOne(ctx,
		bson.M{
			"_id":  "test2",
			"name": "test2",
		})
	require.NoError(t, err)

	// Query test document
	var result struct {
		Name string `bson:"name"`
	}
	err = collection.FindOne(ctx, bson.M{"_id": "test2"}).Decode(&result)
	require.NoError(t, err)

	require.Equal(t, "test2", result.Name)
}
