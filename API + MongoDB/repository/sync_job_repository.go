package repository

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"mini-cdn/api/models"
)

type SyncJobRepository interface {
	CreateJob(ctx context.Context, job *models.SyncJob) error
	UpdateJobProgress(ctx context.Context, nodeID string, lastReplayedSeq int64, syncedFiles int) error
	CompleteJob(ctx context.Context, nodeID string) error
	FailJob(ctx context.Context, nodeID string, errMsg string) error
	GetActiveJobs(ctx context.Context) ([]*models.SyncJob, error)
	GetJobByNodeID(ctx context.Context, nodeID string) (*models.SyncJob, error)
}

type MongoSyncJobRepository struct {
	coll *mongo.Collection
}

func NewMongoSyncJobRepository(client *mongo.Client, dbName string) *MongoSyncJobRepository {
	return &MongoSyncJobRepository{
		coll: client.Database(dbName).Collection("sync_jobs"),
	}
}

func (r *MongoSyncJobRepository) CreateJob(ctx context.Context, job *models.SyncJob) error {
	if job.StartedAt.IsZero() {
		job.StartedAt = time.Now()
	}
	job.UpdatedAt = time.Now()
	opts := options.Update().SetUpsert(true)
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"nodeId": job.NodeID},
		bson.M{"$set": job},
		opts,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert sync job: %w", err)
	}
	return nil
}

func (r *MongoSyncJobRepository) UpdateJobProgress(ctx context.Context, nodeID string, lastReplayedSeq int64, syncedFiles int) error {
	update := bson.M{
		"$set": bson.M{
			"lastReplayedSeq": lastReplayedSeq,
			"syncedFiles":     syncedFiles,
			"updatedAt":       time.Now(),
		},
	}
	_, err := r.coll.UpdateOne(ctx, bson.M{"nodeId": nodeID}, update)
	return err
}

func (r *MongoSyncJobRepository) CompleteJob(ctx context.Context, nodeID string) error {
	update := bson.M{
		"$set": bson.M{
			"status":    models.SyncStatusCompleted,
			"updatedAt": time.Now(),
		},
	}
	_, err := r.coll.UpdateOne(ctx, bson.M{"nodeId": nodeID}, update)
	return err
}

func (r *MongoSyncJobRepository) FailJob(ctx context.Context, nodeID string, errMsg string) error {
	update := bson.M{
		"$set": bson.M{
			"status":       models.SyncStatusFailed,
			"errorMessage": errMsg,
			"updatedAt":    time.Now(),
		},
	}
	_, err := r.coll.UpdateOne(ctx, bson.M{"nodeId": nodeID}, update)
	return err
}

func (r *MongoSyncJobRepository) GetActiveJobs(ctx context.Context) ([]*models.SyncJob, error) {
	cursor, err := r.coll.Find(ctx, bson.M{"status": models.SyncStatusSyncing})
	if err != nil {
		return nil, fmt.Errorf("failed to query active sync jobs: %w", err)
	}
	defer cursor.Close(ctx)

	var jobs []*models.SyncJob
	for cursor.Next(ctx) {
		var j models.SyncJob
		if err := cursor.Decode(&j); err != nil {
			return nil, err
		}
		jobs = append(jobs, &j)
	}
	return jobs, nil
}

func (r *MongoSyncJobRepository) GetJobByNodeID(ctx context.Context, nodeID string) (*models.SyncJob, error) {
	var job models.SyncJob
	err := r.coll.FindOne(ctx, bson.M{"nodeId": nodeID}).Decode(&job)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &job, nil
}

// MockSyncJobRepository provides an in-memory repository for unit tests.
type MockSyncJobRepository struct {
	mu   sync.RWMutex
	jobs map[string]*models.SyncJob
}

func NewMockSyncJobRepository() *MockSyncJobRepository {
	return &MockSyncJobRepository{
		jobs: make(map[string]*models.SyncJob),
	}
}

func (m *MockSyncJobRepository) CreateJob(ctx context.Context, job *models.SyncJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.NodeID] = job
	return nil
}

func (m *MockSyncJobRepository) UpdateJobProgress(ctx context.Context, nodeID string, lastReplayedSeq int64, syncedFiles int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, exists := m.jobs[nodeID]; exists {
		j.LastReplayedSeq = lastReplayedSeq
		j.SyncedFiles = syncedFiles
		j.UpdatedAt = time.Now()
	}
	return nil
}

func (m *MockSyncJobRepository) CompleteJob(ctx context.Context, nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, exists := m.jobs[nodeID]; exists {
		j.Status = models.SyncStatusCompleted
		j.UpdatedAt = time.Now()
	}
	return nil
}

func (m *MockSyncJobRepository) FailJob(ctx context.Context, nodeID string, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, exists := m.jobs[nodeID]; exists {
		j.Status = models.SyncStatusFailed
		j.ErrorMessage = errMsg
		j.UpdatedAt = time.Now()
	}
	return nil
}

func (m *MockSyncJobRepository) GetActiveJobs(ctx context.Context) ([]*models.SyncJob, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []*models.SyncJob
	for _, j := range m.jobs {
		if j.Status == models.SyncStatusSyncing {
			list = append(list, j)
		}
	}
	return list, nil
}

func (m *MockSyncJobRepository) GetJobByNodeID(ctx context.Context, nodeID string) (*models.SyncJob, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[nodeID], nil
}
