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

func NewSpeechesService(db *sqlx.DB) *SpeechesService {
	return &SpeechesService{
		db: db,
	}
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

func (s *SpeechesService) GetSpeeches(limit, offset int) ([]api.Speech, int, error) {
	type speechRow struct {
		Id            string     `db:"id"`
		Type          *string    `db:"type"`
		Title         *string    `db:"title"`
		Date          *time.Time `db:"date"`
		SpeakerId     string     `db:"speaker.id"`
		FirstName     string     `db:"speaker.FirstName"`
		LastName      string     `db:"speaker.LastName"`
		Party         string     `db:"speaker.Party"`
		TopicId       string     `db:"topic.id"`
		TopicCategory *string    `db:"topic.Category"`
		Session       *string    `db:"session"`
		Publisher     *string    `db:"publisher"`
	}

	var totalCount int
	err := s.db.Get(&totalCount, `
		SELECT COUNT(*)
		FROM activities a
		JOIN protocols p ON a.protocol_id = p.id
		JOIN roles r on r.id = a.role_id
		JOIN parliamentary_groups pg on pg.id = r.group_id
		WHERE a.text IS NOT NULL
		  AND a.text != ''
		  AND a.type LIKE 'Rede%'
	`)
	if err != nil {
		return nil, 0, err
	}

	var rows []speechRow
	err = s.db.Select(&rows, `
		SELECT
			a.id as id,
			a.type as type,
			p.title as title,
			p.date as date,
			r.person_id as "speaker.id",
			r.first_name as "speaker.FirstName",
			r.last_name as "speaker.LastName",
			pg.name as "speaker.Party",
			coalesce(am.topic_id, -1) as "topic.id",
			coalesce(t.name, '') as "topic.Category",
			p.document_number as session,
			p.publisher as publisher
		FROM activities a
		JOIN protocols p ON a.protocol_id = p.id
		JOIN roles r on r.id = a.role_id
		JOIN parliamentary_groups pg on pg.id = r.group_id
		LEFT JOIN activity_mappings am on am.activity_id = a.id
		LEFT JOIN topics t on t.id = am.topic_id
		WHERE a.text IS NOT NULL
		  AND a.text != ''
		  AND a.type LIKE 'Rede%'
		ORDER BY p.date DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)

	if err != nil {
		return nil, 0, err
	}

	speeches := make([]api.Speech, len(rows))
	for i, row := range rows {
		t := ""
		if row.Type != nil {
			t = *row.Type
		}
		title := ""
		if row.Title != nil {
			title = *row.Title
		}

		var date time.Time
		if row.Date != nil {
			date = *row.Date
		}

		speeches[i] = api.Speech{
			Id:    row.Id,
			Type:  t,
			Title: title,
			Date:  date,
			Speaker: api.PoliticianRef{
				Id:        row.SpeakerId,
				FirstName: row.FirstName,
				LastName:  row.LastName,
				Party:     row.Party,
			},
			Topic: &api.TopicRef{
				Id:       row.TopicId,
				Category: *row.TopicCategory,
			},
			Session:   row.Session,
			Publisher: row.Publisher,
		}
	}

	return speeches, totalCount, nil
}

func (s *SpeechesService) GetSpeechById(id int) (*api.SpeechDetail, error) {
	type speechRow struct {
		Content       *string    `db:"content"`
		Date          *time.Time `db:"date"`
		Duration      *string    `db:"duration"`
		Id            *string    `db:"id"`
		Session       *string    `db:"session"`
		Publisher     *string    `db:"publisher"`
		SourceUrl     *string    `db:"sourceurl"`
		SpeakerId     string     `db:"speaker.id"`
		FirstName     string     `db:"speaker.FirstName"`
		LastName      string     `db:"speaker.LastName"`
		Party         string     `db:"speaker.Party"`
		Title         *string    `db:"title"`
		TopicId       string     `db:"topic.id"`
		Type          *string    `db:"type"`
		Sentiment     string     `db:"sentiment"`
		RelatedTopics *string    `db:"relatedtopics"`
		TopicCategory *string    `db:"topic.category"`
		Reason        *string    `db:"reason"`
	}
	var row speechRow

	err := s.db.Get(&row, `
		SELECT
			a.text as content,
			p.date as date,
			null as duration,
			a.id as id,
			null as relatedTopics,
			CASE
			    WHEN am.sentiment_value <= -0.6 THEN 'stark negativ'
			    WHEN am.sentiment_value <= -0.2 THEN 'negativ'	
			    WHEN am.sentiment_value <=  0.2 THEN 'neutral'
			    WHEN am.sentiment_value <=  0.6 THEN 'positiv'
			    WHEN am.sentiment_value > 0.6 THEN 'stark positiv'
				ELSE 'unbekannt'
			END as sentiment,
			p.document_number as session,
			p.publisher as publisher,
			p.url as sourceUrl,
			r.person_id as "speaker.id",
			r.first_name as "speaker.FirstName",
			r.last_name as "speaker.LastName",
			pg.name as "speaker.Party",
			p.title as title,
			coalesce(am.topic_id, -1) as "topic.id",
			coalesce(t.name, '') as "topic.category",
			coalesce(a.type, '') as type, -- Regierungserklärung?
			coalesce(am.sentiment_reason, '') as reason
		FROM activities a
		JOIN protocols p ON a.protocol_id = p.id
		JOIN roles r on r.id = a.role_id
		JOIN parliamentary_groups pg on pg.id = r.group_id
		LEFT JOIN activity_mappings am on am.activity_id = a.id
		LEFT JOIN topics t on t.id = am.topic_id
			    
		WHERE a.type LIKE 'Rede%' AND a.id = $1
	`, id)

	if err != nil {
		return nil, err
	}
	sentiment := api.SpeechDetailSentiment(row.Sentiment)
	related := []string{}
	speech := &api.SpeechDetail{
		Content:       row.Content,
		Date:          row.Date,
		Duration:      row.Duration,
		Id:            row.Id,
		Session:       row.Session,
		Publisher:     row.Publisher,
		SourceUrl:     row.SourceUrl,
		Title:         row.Title,
		Type:          row.Type,
		Sentiment:     &sentiment,
		RelatedTopics: &related,
		Speaker: &api.PoliticianRef{
			Id:        row.SpeakerId,
			FirstName: row.FirstName,
			LastName:  row.LastName,
			Party:     row.Party,
		},
		Topic: &api.TopicRef{
			Id:       row.TopicId,
			Category: *row.TopicCategory,
		},
		Reason: row.Reason,
	}

	return speech, nil
}
