package service

import (
	"database/sql"
	"log"
	"time"

	"github.com/jmoiron/sqlx"
)

type HelpersService struct {
	db *sqlx.DB
}

func NewHelpersService(db *sqlx.DB) *HelpersService {
	return &HelpersService{
		db: db,
	}
}

func (s *HelpersService) GetFirstAndLastAnalyzedDate() (time.Time, time.Time, error) {
	firstProtocolDate, lastProtocolDate := time.Time{}, time.Time{}
	err := s.db.Get(&firstProtocolDate, "SELECT MIN(date) FROM analysed_protocols")
	if err != nil {
		log.Printf("Failed to get first protocol date: %v", err)
		return time.Time{}, time.Time{}, err
	}
	err = s.db.Get(&lastProtocolDate, "SELECT MAX(date) FROM analysed_protocols")
	if err != nil {
		log.Printf("Failed to get last protocol date: %v", err)
		return time.Time{}, time.Time{}, err
	}
	return firstProtocolDate, lastProtocolDate, nil
}

// GetDateRangeForTimeRange calculates start and end dates for time series queries
func (s *HelpersService) GetDateRangeForTimeRange(timeRange string, endDate *time.Time) (time.Time, time.Time, error) {
	now := time.Now()
	if endDate == nil {
		endDate = &now
	}
	var startDate time.Time

	switch timeRange {
	case "last_month":
		startDate = endDate.AddDate(0, -1, 0)
	case "last_6_months":
		startDate = endDate.AddDate(0, -6, 0)
	case "ytd":
		startDate = time.Date(endDate.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	case "last_year":
		startDate = endDate.AddDate(-1, 0, 0)
	case "last_2_years":
		startDate = endDate.AddDate(-2, 0, 0)
	case "last_5_years":
		startDate = endDate.AddDate(-5, 0, 0)
	case "max":
		firstProtocolDate, lastProtocolDate, err := s.GetFirstAndLastAnalyzedDate()
		if err != nil {
			log.Printf("Failed to get first and last analyzed date: %v", err)
			return time.Time{}, time.Time{}, err
		}
		return firstProtocolDate, lastProtocolDate, nil
	default:
		log.Printf("Invalid time_range parameter: %s", timeRange)
		return time.Time{}, time.Time{}, sql.ErrNoRows
	}

	return startDate, *endDate, nil
}

func (s *HelpersService) GetMaxElectionPeriod(personID int) (int, error) {
	query := `
		SELECT MAX(election_period) as max_election_period
		FROM roles
		WHERE person_id = $1
	`
	var maxElectionPeriod int
	err := s.db.Get(&maxElectionPeriod, query, personID)
	if err != nil {
		log.Printf("Failed to get max election period: %v", err)
		return 0, err
	}
	return maxElectionPeriod, nil
}
