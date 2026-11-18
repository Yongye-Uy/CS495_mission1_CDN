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

	"mini-cdn/api/models"
)

var (
	ErrNodeNotFound = errors.New("storage node not found")
	ErrNodeExists   = errors.New("storage node already registered")
)

type NodeRepository interface {
	Create(ctx context.Context, node *models.StorageNode) error
	FindByID(ctx context.Context, id string) (*models.StorageNode, error)
	FindByNodeID(ctx context.Context, nodeID string) (*models.StorageNode, error)
	FindByIPAndPort(ctx context.Context, ip string, port int) (*models.StorageNode, error)
	FindAll(ctx context.Context) ([]*models.StorageNode, error)
	FindOnline(ctx context.Context) ([]*models.StorageNode, error)
	UpdateStatus(ctx context.Context, id string, status string) error
	UpdateHeartbeat(ctx context.Context, id string, status string, heartbeat time.Time) error
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int64, error)
}

// MongoNodeRepository implements NodeRepository backed by MongoDB.
type MongoNodeRepository struct {
	collection *mongo.Collection
}

func NewMongoNodeRepository(client *mongo.Client, dbName string) *MongoNodeRepository {
	coll := client.Database(dbName).Collection("storage_nodes")
	return &MongoNodeRepository{
		collection: coll,
	}
}

func (m *MongoNodeRepository) Create(ctx context.Context, node *models.StorageNode) error {
	_, err := m.collection.InsertOne(ctx, node)
	if err != nil {
		return fmt.Errorf("failed to insert node: %w", err)
	}
	return nil
}

func (m *MongoNodeRepository) FindByID(ctx context.Context, id string) (*models.StorageNode, error) {
	var node models.StorageNode
	err := m.collection.FindOne(ctx, bson.M{"$or": []bson.M{{"_id": id}, {"node_id": id}}}).Decode(&node)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNodeNotFound
		}
		return nil, fmt.Errorf("failed to find node: %w", err)
	}
	return &node, nil
}

func (m *MongoNodeRepository) FindByNodeID(ctx context.Context, nodeID string) (*models.StorageNode, error) {
	var node models.StorageNode
	err := m.collection.FindOne(ctx, bson.M{"node_id": nodeID}).Decode(&node)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNodeNotFound
		}
		return nil, fmt.Errorf("failed to find node by node_id: %w", err)
	}
	return &node, nil
}

func (m *MongoNodeRepository) FindByIPAndPort(ctx context.Context, ip string, port int) (*models.StorageNode, error) {
	var node models.StorageNode
	err := m.collection.FindOne(ctx, bson.M{"ip": ip, "port": port}).Decode(&node)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNodeNotFound
		}
		return nil, fmt.Errorf("failed to find node by ip and port: %w", err)
	}
	return &node, nil
}

func (m *MongoNodeRepository) FindAll(ctx context.Context) ([]*models.StorageNode, error) {
	findOpts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}})
	cursor, err := m.collection.Find(ctx, bson.M{}, findOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	defer cursor.Close(ctx)

	var nodes []*models.StorageNode
	for cursor.Next(ctx) {
		var node models.StorageNode
		if err := cursor.Decode(&node); err != nil {
			return nil, fmt.Errorf("failed to decode node: %w", err)
		}
		nodes = append(nodes, &node)
	}
	return nodes, cursor.Err()
}

func (m *MongoNodeRepository) FindOnline(ctx context.Context) ([]*models.StorageNode, error) {
	cursor, err := m.collection.Find(ctx, bson.M{"status": models.NodeStatusOnline})
	if err != nil {
		return nil, fmt.Errorf("failed to find online nodes: %w", err)
	}
	defer cursor.Close(ctx)

	var nodes []*models.StorageNode
	for cursor.Next(ctx) {
		var node models.StorageNode
		if err := cursor.Decode(&node); err != nil {
			return nil, fmt.Errorf("failed to decode online node: %w", err)
		}
		nodes = append(nodes, &node)
	}
	return nodes, cursor.Err()
}

func (m *MongoNodeRepository) UpdateStatus(ctx context.Context, id string, status string) error {
	filter := bson.M{"$or": []bson.M{{"_id": id}, {"node_id": id}}}
	update := bson.M{"$set": bson.M{"status": status}}
	res, err := m.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update node status: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrNodeNotFound
	}
	return nil
}

func (m *MongoNodeRepository) UpdateHeartbeat(ctx context.Context, id string, status string, heartbeat time.Time) error {
	filter := bson.M{"$or": []bson.M{{"_id": id}, {"node_id": id}}}
	update := bson.M{
		"$set": bson.M{
			"status":         status,
			"last_heartbeat": heartbeat,
		},
	}
	res, err := m.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update node heartbeat: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrNodeNotFound
	}
	return nil
}

func (m *MongoNodeRepository) Delete(ctx context.Context, id string) error {
	filter := bson.M{"$or": []bson.M{{"_id": id}, {"node_id": id}}}
	res, err := m.collection.DeleteOne(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to delete node: %w", err)
	}
	if res.DeletedCount == 0 {
		return ErrNodeNotFound
	}
	return nil
}

func (m *MongoNodeRepository) Count(ctx context.Context) (int64, error) {
	return m.collection.CountDocuments(ctx, bson.M{})
}

// MockNodeRepository provides an in-memory repository for testing.
type MockNodeRepository struct {
	mu    sync.RWMutex
	nodes map[string]*models.StorageNode
}

func NewMockNodeRepository() *MockNodeRepository {
	return &MockNodeRepository{
		nodes: make(map[string]*models.StorageNode),
	}
}

func (m *MockNodeRepository) Create(ctx context.Context, node *models.StorageNode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.nodes {
		if n.IP == node.IP && n.Port == node.Port {
			return ErrNodeExists
		}
		if n.NodeID == node.NodeID {
			return ErrNodeExists
		}
	}
	m.nodes[node.ID] = node
	return nil
}

func (m *MockNodeRepository) FindByID(ctx context.Context, id string) (*models.StorageNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range m.nodes {
		if n.ID == id || n.NodeID == id {
			return n, nil
		}
	}
	return nil, ErrNodeNotFound
}

func (m *MockNodeRepository) FindByNodeID(ctx context.Context, nodeID string) (*models.StorageNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range m.nodes {
		if n.NodeID == nodeID {
			return n, nil
		}
	}
	return nil, ErrNodeNotFound
}

func (m *MockNodeRepository) FindByIPAndPort(ctx context.Context, ip string, port int) (*models.StorageNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range m.nodes {
		if n.IP == ip && n.Port == port {
			return n, nil
		}
	}
	return nil, ErrNodeNotFound
}

func (m *MockNodeRepository) FindAll(ctx context.Context) ([]*models.StorageNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []*models.StorageNode
	for _, n := range m.nodes {
		list = append(list, n)
	}
	return list, nil
}

func (m *MockNodeRepository) FindOnline(ctx context.Context) ([]*models.StorageNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []*models.StorageNode
	for _, n := range m.nodes {
		if n.Status == models.NodeStatusOnline {
			list = append(list, n)
		}
	}
	return list, nil
}

func (m *MockNodeRepository) UpdateStatus(ctx context.Context, id string, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.nodes {
		if n.ID == id || n.NodeID == id {
			n.Status = status
			return nil
		}
	}
	return ErrNodeNotFound
}

func (m *MockNodeRepository) UpdateHeartbeat(ctx context.Context, id string, status string, heartbeat time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.nodes {
		if n.ID == id || n.NodeID == id {
			n.Status = status
			n.LastHeartbeat = heartbeat
			return nil
		}
	}
	return ErrNodeNotFound
}

func (m *MockNodeRepository) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, n := range m.nodes {
		if n.ID == id || n.NodeID == id {
			delete(m.nodes, k)
			return nil
		}
	}
	return ErrNodeNotFound
}

func (m *MockNodeRepository) Count(ctx context.Context) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.nodes)), nil
}
