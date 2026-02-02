package service

import (
	"database/sql"
	"log"
	api "plenartrend/crud/src/openAPI"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
)

type SpeechesService struct {
	db *sqlx.DB
}

// TODO: Is this not simply getSpeeches?
func (s *TopicsService) GetSpeechSnippets(topicID *int, personID *int, groupId *int, electionPeriod *int, limit int) ([]api.SpeechSnippet, error) {
	type SpeechWithRole struct {
		ActivityID   int            `db:"activity_id"`
		Text         string         `db:"text"`
		ProtocolDate time.Time      `db:"protocol_date"`
		FirstName    string         `db:"first_name"`
		LastName     string         `db:"last_name"`
		Title        sql.NullString `db:"title"`
		GroupName    sql.NullString `db:"group_name"`
		Sentiment    float64        `db:"sentiment"`
	}

	var speechData []SpeechWithRole
	err := s.db.Select(&speechData, `
		SELECT DISTINCT ON (a.id)
			a.id as activity_id,
			a.text,
			p.date as protocol_date,
			r.first_name,
			r.last_name,
			r.title,
			pg.name as group_name,
			am.sentiment_value as sentiment
		FROM activities a
		JOIN activity_mappings am ON am.activity_id = a.id
		JOIN roles r ON r.id = a.role_id
		JOIN protocols p ON p.id = a.protocol_id
		LEFT JOIN parliamentary_groups pg ON pg.id = r.group_id
		WHERE ($1::int IS NULL OR am.topic_id = $1::int)
		AND ($2::int IS NULL OR r.person_id = $2::int)
		AND ($3::int IS NULL OR r.group_id = $3::int)
		AND ($4::int IS NULL OR p.election_period = $4::int)
		ORDER BY a.id, p.date DESC
		LIMIT $5::int
	`, topicID, personID, groupId, electionPeriod, limit)
	if err != nil {
		log.Printf("Failed to get speeches: %v", err)
		return nil, err
	}
	log.Printf("Found %d speeches for topic %d, person %d, group %d", len(speechData), topicID, personID, groupId)

	speeches := []api.SpeechSnippet{}
	for _, speech := range speechData {
		speakerName := speech.FirstName + " " + speech.LastName
		if speech.Title.Valid && speech.Title.String != "" {
			speakerName = speech.Title.String + " " + speakerName
		}

		partyName := "Fraktionslos"
		if speech.GroupName.Valid {
			partyName = speech.GroupName.String
		}

		var sentimentEnum api.SpeechSnippetSentiment
		if speech.Sentiment > 0.5 {
			sentimentEnum = api.StarkPositiv
		} else if speech.Sentiment > 0.2 {
			sentimentEnum = api.Positiv
		} else if speech.Sentiment < -0.5 {
			sentimentEnum = api.StarkNegativ
		} else if speech.Sentiment < -0.2 {
			sentimentEnum = api.Negativ
		} else {
			sentimentEnum = api.Neutral
		}

		activityIDStr := strconv.Itoa(speech.ActivityID)
		var topicIDStr *string
		if topicID != nil {
			str := strconv.Itoa(*topicID)
			topicIDStr = &str
		}

		speeches = append(speeches, api.SpeechSnippet{
			Id:           &activityIDStr,
			TopicId:      topicIDStr,
			Speaker:      &speakerName,
			Party:        &partyName,
			Text:         &speech.Text,
			Date:         &speech.ProtocolDate,
			Sentiment:    &sentimentEnum,
			FullSpeechId: &activityIDStr,
		})
	}
	log.Printf("Built %d speech snippets", len(speeches))
	return speeches, nil
}
