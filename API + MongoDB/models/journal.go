package models

import "time"

const (
	OpUpload = "UPLOAD"
	OpDelete = "DELETE"
)

// MutationJournalEntry represents an ordered mutation event in MongoDB.
type MutationJournalEntry struct {
	ID        string    `json:"id" bson:"_id,omitempty"`
	Seq       int64     `json:"seq" bson:"seq"`
	Op        string    `json:"op" bson:"op"` // UPLOAD or DELETE
	FileID    string    `json:"fileId" bson:"fileId"`
	Filename  string    `json:"filename,omitempty" bson:"filename,omitempty"`
	Size      int64     `json:"size,omitempty" bson:"size,omitempty"`
	SHA256    string    `json:"sha256,omitempty" bson:"sha256,omitempty"`
	CreatedAt time.Time `json:"createdAt" bson:"createdAt"`
}
