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

type JournalRepository interface {
	NextSequence(ctx context.Context) (int64, error)
	InsertEntry(ctx context.Context, entry *models.MutationJournalEntry) error
	RecordMutationTx(ctx context.Context, sc mongo.SessionContext, op string, fileID string, filename string, size int64, sha256 string, fileDoc *models.FileMetadata) (int64, error)
	RecordDeleteTx(ctx context.Context, sc mongo.SessionContext, fileID string) (int64, error)
	GetMutationsAfter(ctx context.Context, seq int64) ([]*models.MutationJournalEntry, error)
	GetMaxSequence(ctx context.Context) (int64, error)
	PruneJournal(ctx context.Context, safeWatermarkSeq int64, cutoff time.Time) (int64, error)
}

type MongoJournalRepository struct {
	client       *mongo.Client
	journalColl  *mongo.Collection
	countersColl *mongo.Collection
	filesColl    *mongo.Collection
}

func NewMongoJournalRepository(client *mongo.Client, dbName string) *MongoJournalRepository {
	db := client.Database(dbName)
	return &MongoJournalRepository{
		client:       client,
		journalColl:  db.Collection("mutation_journal"),
		countersColl: db.Collection("atomic_counters"),
		filesColl:    db.Collection("files"),
	}
}

// NextSequence allocates an atomic monotonically increasing sequence number using findOneAndUpdate.
func (r *MongoJournalRepository) NextSequence(ctx context.Context) (int64, error) {
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var res struct {
		Val int64 `bson:"val"`
	}
	err := r.countersColl.FindOneAndUpdate(ctx,
		bson.M{"_id": "mutation_seq"},
		bson.M{"$inc": bson.M{"val": int64(1)}},
		opts,
	).Decode(&res)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate sequence number: %w", err)
	}
	return res.Val, nil
}

// InsertEntry saves a journal entry.
func (r *MongoJournalRepository) InsertEntry(ctx context.Context, entry *models.MutationJournalEntry) error {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	_, err := r.journalColl.InsertOne(ctx, entry)
	return err
}

// RecordMutationTx commits sequence allocation, journal entry insertion, and file metadata atomically in a transaction.
func (r *MongoJournalRepository) RecordMutationTx(ctx context.Context, sc mongo.SessionContext, op string, fileID string, filename string, size int64, sha256 string, fileDoc *models.FileMetadata) (int64, error) {
	// 1. Allocate sequence in the transaction
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var counterRes struct {
		Val int64 `bson:"val"`
	}
	err := r.countersColl.FindOneAndUpdate(sc,
		bson.M{"_id": "mutation_seq"},
		bson.M{"$inc": bson.M{"val": int64(1)}},
		opts,
	).Decode(&counterRes)
	if err != nil {
		return 0, fmt.Errorf("tx: failed to increment mutation sequence: %w", err)
	}

	seq := counterRes.Val

	// 2. Insert journal record
	entry := &models.MutationJournalEntry{
		Seq:       seq,
		Op:        op,
		FileID:    fileID,
		Filename:  filename,
		Size:      size,
		SHA256:    sha256,
		CreatedAt: time.Now(),
	}
	if _, err := r.journalColl.InsertOne(sc, entry); err != nil {
		return 0, fmt.Errorf("tx: failed to insert mutation journal entry: %w", err)
	}

	// 3. Insert or update file metadata
	if fileDoc != nil {
		fileOpts := options.Update().SetUpsert(true)
		_, err := r.filesColl.UpdateOne(sc,
			bson.M{"_id": fileDoc.ID},
			bson.M{"$set": fileDoc},
			fileOpts,
		)
		if err != nil {
			return 0, fmt.Errorf("tx: failed to upsert file metadata: %w", err)
		}
	}

	return seq, nil
}

// RecordDeleteTx atomically logs a DELETE mutation and flags/removes the file record.
func (r *MongoJournalRepository) RecordDeleteTx(ctx context.Context, sc mongo.SessionContext, fileID string) (int64, error) {
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var counterRes struct {
		Val int64 `bson:"val"`
	}
	err := r.countersColl.FindOneAndUpdate(sc,
		bson.M{"_id": "mutation_seq"},
		bson.M{"$inc": bson.M{"val": int64(1)}},
		opts,
	).Decode(&counterRes)
	if err != nil {
		return 0, fmt.Errorf("tx: failed to increment delete sequence: %w", err)
	}

	seq := counterRes.Val

	entry := &models.MutationJournalEntry{
		Seq:       seq,
		Op:        models.OpDelete,
		FileID:    fileID,
		CreatedAt: time.Now(),
	}
	if _, err := r.journalColl.InsertOne(sc, entry); err != nil {
		return 0, fmt.Errorf("tx: failed to insert delete journal entry: %w", err)
	}

	// Tombstone / remove file metadata
	_, err = r.filesColl.UpdateOne(sc,
		bson.M{"_id": fileID},
		bson.M{"$set": bson.M{"isDeleted": true, "updatedAt": time.Now()}},
	)
	if err != nil {
		return 0, fmt.Errorf("tx: failed to mark file deleted: %w", err)
	}

	return seq, nil
}

// GetMutationsAfter retrieves journal entries strictly greater than seq, ordered ascending.
func (r *MongoJournalRepository) GetMutationsAfter(ctx context.Context, seq int64) ([]*models.MutationJournalEntry, error) {
	findOpts := options.Find().SetSort(bson.D{{Key: "seq", Value: 1}})
	filter := bson.M{"seq": bson.M{"$gt": seq}}

	cursor, err := r.journalColl.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to query mutation journal: %w", err)
	}
	defer cursor.Close(ctx)

	var entries []*models.MutationJournalEntry
	for cursor.Next(ctx) {
		var e models.MutationJournalEntry
		if err := cursor.Decode(&e); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, nil
}

// GetMaxSequence returns the highest current sequence number.
func (r *MongoJournalRepository) GetMaxSequence(ctx context.Context) (int64, error) {
	var res struct {
		Val int64 `bson:"val"`
	}
	err := r.countersColl.FindOne(ctx, bson.M{"_id": "mutation_seq"}).Decode(&res)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return 0, nil
		}
		return 0, err
	}
	return res.Val, nil
}

// PruneJournal removes journal records strictly older than cutoff and below safeWatermarkSeq.
func (r *MongoJournalRepository) PruneJournal(ctx context.Context, safeWatermarkSeq int64, cutoff time.Time) (int64, error) {
	filter := bson.M{
		"seq":       bson.M{"$lt": safeWatermarkSeq},
		"createdAt": bson.M{"$lt": cutoff},
	}
	res, err := r.journalColl.DeleteMany(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("failed to prune mutation journal: %w", err)
	}
	return res.DeletedCount, nil
}

// MockJournalRepository provides an in-memory implementation for unit testing.
type MockJournalRepository struct {
	mu      sync.Mutex
	seq     int64
	entries []*models.MutationJournalEntry
}

func NewMockJournalRepository() *MockJournalRepository {
	return &MockJournalRepository{}
}

func (m *MockJournalRepository) NextSequence(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	return m.seq, nil
}

func (m *MockJournalRepository) InsertEntry(ctx context.Context, entry *models.MutationJournalEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	m.entries = append(m.entries, entry)
	return nil
}

func (m *MockJournalRepository) RecordMutationTx(ctx context.Context, sc mongo.SessionContext, op string, fileID string, filename string, size int64, sha256 string, fileDoc *models.FileMetadata) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	entry := &models.MutationJournalEntry{
		Seq:       m.seq,
		Op:        op,
		FileID:    fileID,
		Filename:  filename,
		Size:      size,
		SHA256:    sha256,
		CreatedAt: time.Now(),
	}
	m.entries = append(m.entries, entry)
	return m.seq, nil
}

func (m *MockJournalRepository) RecordDeleteTx(ctx context.Context, sc mongo.SessionContext, fileID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	entry := &models.MutationJournalEntry{
		Seq:       m.seq,
		Op:        models.OpDelete,
		FileID:    fileID,
		CreatedAt: time.Now(),
	}
	m.entries = append(m.entries, entry)
	return m.seq, nil
}

func (m *MockJournalRepository) GetMutationsAfter(ctx context.Context, seq int64) ([]*models.MutationJournalEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []*models.MutationJournalEntry
	for _, e := range m.entries {
		if e.Seq > seq {
			res = append(res, e)
		}
	}
	return res, nil
}

func (m *MockJournalRepository) GetMaxSequence(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seq, nil
}

func (m *MockJournalRepository) PruneJournal(ctx context.Context, safeWatermarkSeq int64, cutoff time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept []*models.MutationJournalEntry
	deleted := int64(0)
	for _, e := range m.entries {
		if e.Seq < safeWatermarkSeq && e.CreatedAt.Before(cutoff) {
			deleted++
		} else {
			kept = append(kept, e)
		}
	}
	m.entries = kept
	return deleted, nil
}
