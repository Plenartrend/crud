package types

import (
	"database/sql"
	"time"
)

// TopicWithAnalytics combines topic data with analytics from get_topic_analytics function
type TopicWithAnalytics struct {
	ID             int       `db:"id"`
	Name           string    `db:"name"`
	TopicRelevance float64   `db:"topic_share"` // DB column from get_topic_analytics is topic_share
	AvgSentiment   float64   `db:"avg_sentiment"`
	Updated        time.Time `db:"updated"`
	Created        time.Time `db:"created"`
}

// PartyAnalytics is the result type from get_topic_analytics_per_party function
type PartyAnalytics struct {
	GroupID        int     `db:"group_id"`
	TopicRelevance float64 `db:"topic_relevance"` // Function returns topic_relevance not topic_share
	AvgSentiment   float64 `db:"avg_sentiment"`
}

// PersonRanking is the result type from get_most_active function
type PersonRanking struct {
	PersonID    int     `db:"person_id"`
	Score       float64 `db:"score"`
	RankingType string  `db:"ranking_type"`
}

// Role represents a person's role in parliament
type Role struct {
	ID             int            `db:"id"`
	RoleName       sql.NullString `db:"name"`
	Title          sql.NullString `db:"title"`
	NameSuffix     sql.NullString `db:"name_suffix"`
	LastName       string         `db:"last_name"`
	FirstName      string         `db:"first_name"`
	PersonID       int            `db:"person_id"`
	GroupID        sql.NullInt64  `db:"group_id"`
	ElectionPeriod sql.NullInt64  `db:"election_period"`
	Updated        time.Time      `db:"updated"`
	Created        time.Time      `db:"created"`
}
