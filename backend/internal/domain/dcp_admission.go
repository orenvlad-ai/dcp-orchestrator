package domain

import "time"

// DCPReviewLabAdmission is the bounded FIFO/lease record subordinate to one
// exact-head structured ReviewRun. It never replaces the native session/card.
type DCPReviewLabAdmission struct {
	Sequence         int64
	ID               string
	ReviewRunID      string
	ReviewID         string
	SessionID        SessionID
	PRURL            string
	PRNumber         int64
	TargetSHA        string
	ReviewBaseSHA    string
	AdmittedBaseSHA  string
	Status           DCPAdmissionStatus
	LeaseID          string
	MergeCommitSHA   string
	ErrorCode        string
	IncidentPacket   string
	RefreshWakeCount int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// DCPAdmissionStatus is the durable mechanical state of one exact review-lab
// terminal candidate. Waiting has no process or timeout. Claimed is the only
// merge owner. Refreshing is the single permitted same-worker wake action.
type DCPAdmissionStatus string

const (
	DCPAdmissionWaiting    DCPAdmissionStatus = "waiting"
	DCPAdmissionClaimed    DCPAdmissionStatus = "claimed"
	DCPAdmissionRefreshing DCPAdmissionStatus = "refreshing"
	DCPAdmissionSucceeded  DCPAdmissionStatus = "succeeded"
	DCPAdmissionFailed     DCPAdmissionStatus = "failed"
	DCPAdmissionIncident   DCPAdmissionStatus = "incident"
)
