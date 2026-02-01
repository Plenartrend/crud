package main

import (
	"database/sql"
	"slices"
	"time"
)

type DocumentType string

const (
	DocumentProtocol     DocumentType = "protocol"
	DocumentPrintedPaper DocumentType = "printedPaper"
)

type Body string

const (
	Bundestag         Body = "BT"
	Bundesrat         Body = "BR"
	Bundesversammlung Body = "BV"
	Europakammer      Body = "EK"
)

func IsValidBody(value string) bool {
	valid := []Body{Bundestag, Bundesrat, Bundesversammlung, Europakammer}
	return slices.Contains(valid, Body(value))
}

type Topic struct {
	ID      int       `db:"id" json:"id,omitempty"`
	Name    string    `db:"name" json:"name,omitempty"`
	Updated time.Time `db:"updated" json:"updated,omitempty"`
	Created time.Time `db:"created" json:"created,omitempty"`
}

// TopicWithAnalytics is the result of joining topics with get_topic_analytics (relevance + sentiment).
type TopicWithAnalytics struct {
	Topic
	TopicRelevance float64 `db:"topic_share"` // DB column from get_topic_analytics is topic_share
	AvgSentiment   float64 `db:"avg_sentiment"`
}

type Process struct {
	ID             int       `db:"id" json:"id,omitempty"`
	Title          string    `db:"title" json:"title,omitempty"`
	Status         string    `db:"status" json:"status,omitempty"`
	Summary        string    `db:"summary" json:"summary,omitempty"`
	Keywords       []string  `db:"keywords" json:"keywords,omitempty"`
	ElectionPeriod int       `db:"election_period" json:"election_period,omitempty"`
	Type           string    `db:"type" json:"type,omitempty"`
	Date           time.Time `db:"date" json:"date,omitempty"`
	APIUpdated     time.Time `db:"api_updated" json:"api_updated,omitempty"`
	Updated        time.Time `db:"updated" json:"updated,omitempty"`
	Created        time.Time `db:"created" json:"created,omitempty"`
}

type ProcessTopics struct {
	ProcessID int       `db:"process_id" json:"process_id,omitempty"`
	TopicID   int       `db:"topic_id" json:"topic_id,omitempty"`
	Updated   time.Time `db:"updated" json:"updated,omitempty"`
	Created   time.Time `db:"created" json:"created,omitempty"`
}

type ProcessInitiator struct {
	ProcessID int       `db:"process_id" json:"process_id,omitempty"`
	GroupID   int       `db:"group_id" json:"group_id,omitempty"`
	Updated   time.Time `db:"updated" json:"updated,omitempty"`
	Created   time.Time `db:"created" json:"created,omitempty"`
}

type ProcessPosition struct {
	ID             int           `db:"id" json:"id,omitempty"`
	Type           string        `db:"type" json:"type,omitempty"`
	ProcessID      int           `db:"process_id" json:"process_id,omitempty"`
	PrintedPaperID sql.NullInt64 `db:"printed_paper_id" json:"printed_paper_id,omitempty"`
	ProtocolID     sql.NullInt64 `db:"protocol_id" json:"protocol_id,omitempty"`
	Association    Body          `db:"association" json:"association,omitempty"`
	Continuation   bool          `db:"continuation" json:"continuation,omitempty"`
	Supplement     bool          `db:"supplement" json:"supplement,omitempty"`
	Title          string        `db:"title" json:"title,omitempty"`
	DocumentType   DocumentType  `db:"document_type" json:"document_type,omitempty"`
	Date           time.Time     `db:"date" json:"date,omitempty"`
	APIUpdated     time.Time     `db:"api_updated" json:"api_updated,omitempty"`
	Updated        time.Time     `db:"updated" json:"updated,omitempty"`
	Created        time.Time     `db:"created" json:"created,omitempty"`
}

type PrintedPaper struct {
	ID             int            `db:"id" json:"id,omitempty"`
	Type           string         `db:"type" json:"type,omitempty"`
	Title          string         `db:"title" json:"title,omitempty"`
	DocumentNumber string         `db:"document_number" json:"document_number,omitempty"`
	Publisher      Body           `db:"publisher" json:"publisher,omitempty"`
	GroupID        sql.NullInt64  `db:"group_id" json:"group_id,omitempty"`
	URL            string         `db:"url" json:"url,omitempty"`
	Text           sql.NullString `db:"text" json:"text,omitempty"`
	ElectionPeriod int            `db:"election_period" json:"election_period,omitempty"`
	Date           time.Time      `db:"date" json:"date,omitempty"`
	APIUpdated     time.Time      `db:"api_updated" json:"api_updated,omitempty"`
	Updated        time.Time      `db:"updated" json:"updated,omitempty"`
	Created        time.Time      `db:"created" json:"created,omitempty"`
}

type Protocol struct {
	ID                  int              `db:"id" json:"id,omitempty"`
	Title               string           `db:"title" json:"title,omitempty"`
	DocumentNumber      string           `db:"document_number" json:"document_number,omitempty"`
	Publisher           Body             `db:"publisher" json:"publisher,omitempty"`
	SessionNote         sql.NullString   `db:"session_note" json:"session_note,omitempty"`
	URL                 string           `db:"url" json:"url,omitempty"`
	Text                sql.NullString   `db:"text" json:"text,omitempty"`
	ElectionPeriod      int              `db:"election_period" json:"election_period,omitempty"`
	Date                time.Time        `db:"date" json:"date,omitempty"`
	APIUpdated          time.Time        `db:"api_updated" json:"api_updated,omitempty"`
	Updated             time.Time        `db:"updated" json:"updated,omitempty"`
	Created             time.Time        `db:"created" json:"created,omitempty"`
	ProcessingStatus    ProcessingStatus `db:"processing_status" json:"processing_status,omitempty"`
	AttemptsCount       int              `db:"attempts_count" json:"attempts_count,omitempty"`
	ProcessingTimestamp sql.NullTime     `db:"processing_timestamp" json:"processing_timestamp,omitempty"`
}

type Activity struct {
	ID             int           `db:"id" json:"id,omitempty"`
	Type           string        `db:"type" json:"type,omitempty"`
	RoleID         int           `db:"role_id" json:"role_id,omitempty"`
	DocumentType   DocumentType  `db:"document_type" json:"document_type,omitempty"`
	PrintedPaperID sql.NullInt64 `db:"printed_paper_id" json:"printed_paper_id,omitempty"`
	ProtocolID     sql.NullInt64 `db:"protocol_id" json:"protocol_id,omitempty"`
	Text           string        `db:"text" json:"text,omitempty"`
	APIUpdated     time.Time     `db:"api_updated" json:"api_updated,omitempty"`
	Updated        time.Time     `db:"updated" json:"updated,omitempty"`
	Created        time.Time     `db:"created" json:"created,omitempty"`
}

type ActivityMapping struct {
	ID              int             `db:"id" json:"id,omitempty"`
	ActivityID      int             `db:"activity_id" json:"activity_id,omitempty"`
	TopicID         sql.NullInt64   `db:"topic_id" json:"topic_id,omitempty"`
	SentimentValue  sql.NullFloat64 `db:"sentiment_value" json:"sentiment_value,omitempty"`
	SentimentReason sql.NullString  `db:"sentiment_reason" json:"sentiment_reason,omitempty"`
}

type ActivityTfidf struct {
	PersonID    int    `db:"person_id" json:"person_id,omitempty"`
	TfidfVector string `db:"tfidf_vector" json:"tfidf_vector,omitempty"`
}

type ActivityRelevance struct {
	ProtocolID int     `db:"protocol_id" json:"protocol_id,omitempty"`
	TopicID    int     `db:"topic_id" json:"topic_id,omitempty"`
	Relevance  float64 `db:"relevance" json:"relevance,omitempty"`
}

type Person struct {
	ID         int       `db:"id" json:"id,omitempty"`
	APIUpdated time.Time `db:"api_updated" json:"api_updated,omitempty"`
	Updated    time.Time `db:"updated" json:"updated,omitempty"`
	Created    time.Time `db:"created" json:"created,omitempty"`
}

type ParliamentaryGroup struct {
	ID        int            `db:"id" json:"id,omitempty"`
	Name      sql.NullString `db:"name" json:"name,omitempty"`
	ShortName sql.NullString `db:"short_name" json:"short_name,omitempty"`
	Updated   time.Time      `db:"updated" json:"updated,omitempty"`
	Created   time.Time      `db:"created" json:"created,omitempty"`
}

type Role struct {
	ID             int            `db:"id" json:"id,omitempty"`
	RoleName       sql.NullString `db:"name" json:"name,omitempty"`
	Title          sql.NullString `db:"title" json:"title,omitempty"`
	NameSuffix     sql.NullString `db:"name_suffix" json:"name_suffix,omitempty"`
	LastName       string         `db:"last_name" json:"last_name,omitempty"`
	FirstName      string         `db:"first_name" json:"first_name,omitempty"`
	PersonID       int            `db:"person_id" json:"person_id,omitempty"`
	GroupID        sql.NullInt64  `db:"group_id" json:"group_id,omitempty"`
	ElectionPeriod sql.NullInt64  `db:"election_period" json:"election_period,omitempty"`
	Updated        time.Time      `db:"updated" json:"updated,omitempty"`
	Created        time.Time      `db:"created" json:"created,omitempty"`
}

type ElectionPeriod struct {
	Number    int          `db:"number" json:"number"`
	StartDate sql.NullTime `db:"start_date" json:"start_date,omitempty"`
	EndDate   sql.NullTime `db:"end_date" json:"end_date,omitempty"`
	Updated   time.Time    `db:"updated" json:"updated,omitempty"`
	Created   time.Time    `db:"created" json:"created,omitempty"`
}

type ProcessingStatus string

const (
	ProcessingStatusNotStarted ProcessingStatus = "not_started"
	ProcessingStatusInProgress ProcessingStatus = "in_progress"
	ProcessingStatusCompleted  ProcessingStatus = "completed"
	ProcessingStatusFailed     ProcessingStatus = "failed"
)

// PersonRanking represents the result of SQL functions that return person rankings with scores.
type PersonRanking struct {
	PersonID     int     `db:"person_id" json:"person_id"`
	Score        float64 `db:"score" json:"score"`
	RankingType  string  `db:"ranking_type" json:"ranking_type"`
}

// PartyAnalytics is the result type from get_topic_analytics_per_party function
// The function returns: group_id, topic_relevance, avg_sentiment (no name, no topic_id)
type PartyAnalytics struct {
	GroupID        int     `db:"group_id"`
	TopicRelevance float64 `db:"topic_relevance"`
	AvgSentiment   float64 `db:"avg_sentiment"`
}
