package models

import "time"

const (
	SyncStatusSyncing   = "SYNCING"
	SyncStatusCompleted = "COMPLETED"
	SyncStatusFailed    = "FAILED"
)

// SyncJob persists node synchronization state surviving API restarts.
type SyncJob struct {
	ID              string    `json:"id" bson:"_id,omitempty"`
	NodeID          string    `json:"nodeId" bson:"nodeId"`
	Status          string    `json:"status" bson:"status"`
	StartSeq        int64     `json:"startSeq" bson:"startSeq"`
	LastReplayedSeq int64     `json:"lastReplayedSeq" bson:"lastReplayedSeq"`
	TotalFiles      int       `json:"totalFiles" bson:"totalFiles"`
	SyncedFiles     int       `json:"syncedFiles" bson:"syncedFiles"`
	StartedAt       time.Time `json:"startedAt" bson:"startedAt"`
	UpdatedAt       time.Time `json:"updatedAt" bson:"updatedAt"`
	ErrorMessage    string    `json:"errorMessage,omitempty" bson:"errorMessage,omitempty"`
}
