package types

import (
	"time"
)

// TopicWithAnalytics combines topic data with analytics from get_topic_analytics function
type TopicWithAnalytics struct {
	ID             int       `db:"id"`
	Name           string    `db:"name"`
	TopicRelevance float64   `db:"topic_relevance"` // DB column from get_topic_analytics is topic_relevance
	AvgSentiment   float64   `db:"avg_sentiment"`
	Updated        time.Time `db:"updated"`
	Created        time.Time `db:"created"`
}

// PartyAnalytics is the result type from get_topic_analytics_per_party function
type PartyAnalytics struct {
	GroupID        int     `db:"group_id"`
	TopicRelevance float64 `db:"topic_relevance"` // Function returns topic_relevance not topic_relevance
	AvgSentiment   float64 `db:"avg_sentiment"`
}

// PersonRanking is the result type from get_most_active function
type PersonRanking struct {
	PersonID    int     `db:"person_id"`
	Score       float64 `db:"score"`
	RankingType string  `db:"ranking_type"`
}
