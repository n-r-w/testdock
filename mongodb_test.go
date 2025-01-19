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
	db := GetMongoDatabase(t,
		"mongodb://testuser:secret@127.0.0.1:27017/testdb?authSource=admin",
		WithDockerRepository("mongo"),
		WithDockerImage("6.0.20"),
		WithMigrations("migrations/mongodb", GolangMigrateFactory),
	)

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
