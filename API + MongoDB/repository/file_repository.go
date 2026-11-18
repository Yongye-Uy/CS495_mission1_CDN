package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"mini-cdn/api/models"
)

var (
	ErrNotFound = errors.New("file metadata not found in database")
)

type FileRepository interface {
	Create(ctx context.Context, meta *models.FileMetadata) error
	FindByID(ctx context.Context, id string) (*models.FileMetadata, error)
	FindAll(ctx context.Context) ([]*models.FileMetadata, error)
	UpdateReplicas(ctx context.Context, id string, replicas []models.Replica) error
	Delete(ctx context.Context, id string) error
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
}

// MongoFileRepository implements FileRepository using official MongoDB driver.
type MongoFileRepository struct {
	client     *mongo.Client
	collection *mongo.Collection
}

func NewMongoFileRepository(ctx context.Context, uri, dbName string) (*MongoFileRepository, error) {
	clientOpts := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Verify connection
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	coll := client.Database(dbName).Collection("files")

	return &MongoFileRepository{
		client:     client,
		collection: coll,
	}, nil
}

func (m *MongoFileRepository) Client() *mongo.Client {
	return m.client
}

func (m *MongoFileRepository) Create(ctx context.Context, meta *models.FileMetadata) error {
	_, err := m.collection.InsertOne(ctx, meta)
	if err != nil {
		return fmt.Errorf("failed to insert metadata: %w", err)
	}
	return nil
}

func (m *MongoFileRepository) FindByID(ctx context.Context, id string) (*models.FileMetadata, error) {
	var meta models.FileMetadata
	err := m.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&meta)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to find metadata: %w", err)
	}
	return &meta, nil
}

func (m *MongoFileRepository) FindAll(ctx context.Context) ([]*models.FileMetadata, error) {
	findOpts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := m.collection.Find(ctx, bson.M{}, findOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list metadata: %w", err)
	}
	defer cursor.Close(ctx)

	var results []*models.FileMetadata
	for cursor.Next(ctx) {
		var meta models.FileMetadata
		if err := cursor.Decode(&meta); err != nil {
			return nil, fmt.Errorf("failed to decode metadata: %w", err)
		}
		results = append(results, &meta)
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor iteration error: %w", err)
	}

	return results, nil
}

func (m *MongoFileRepository) UpdateReplicas(ctx context.Context, id string, replicas []models.Replica) error {
	_, err := m.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"replicas": replicas}})
	if err != nil {
		return fmt.Errorf("failed to update replicas: %w", err)
	}
	return nil
}

func (m *MongoFileRepository) Delete(ctx context.Context, id string) error {
	res, err := m.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete metadata: %w", err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (m *MongoFileRepository) Ping(ctx context.Context) error {
	return m.client.Ping(ctx, readpref.Primary())
}

func (m *MongoFileRepository) Close(ctx context.Context) error {
	return m.client.Disconnect(ctx)
}

// MockFileRepository provides an in-memory repository for fast unit tests.
type MockFileRepository struct {
	mu      sync.RWMutex
	files   map[string]*models.FileMetadata
	pingErr error
}

func NewMockFileRepository() *MockFileRepository {
	return &MockFileRepository{
		files: make(map[string]*models.FileMetadata),
	}
}

func (m *MockFileRepository) SetPingError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pingErr = err
}

func (m *MockFileRepository) Create(ctx context.Context, meta *models.FileMetadata) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[meta.ID] = meta
	return nil
}

func (m *MockFileRepository) FindByID(ctx context.Context, id string) (*models.FileMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	meta, exists := m.files[id]
	if !exists {
		return nil, ErrNotFound
	}
	return meta, nil
}

func (m *MockFileRepository) FindAll(ctx context.Context) ([]*models.FileMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []*models.FileMetadata
	for _, meta := range m.files {
		list = append(list, meta)
	}
	return list, nil
}

func (m *MockFileRepository) UpdateReplicas(ctx context.Context, id string, replicas []models.Replica) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	file, exists := m.files[id]
	if !exists {
		return ErrNotFound
	}
	file.Replicas = replicas
	return nil
}

func (m *MockFileRepository) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.files[id]; !exists {
		return ErrNotFound
	}
	delete(m.files, id)
	return nil
}

func (m *MockFileRepository) Ping(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pingErr
}

func (m *MockFileRepository) Close(ctx context.Context) error {
	return nil
}
