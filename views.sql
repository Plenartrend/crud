-- activity_relevance stores protocol relevance
-- activity_mappings stores sentiments regarding topics in activities


-- TODO everything per election period as otherwise group changes cause misrepresentations

------------------ Relevance ------------------


CREATE OR REPLACE VIEW topic_relevance_weekly AS
SELECT ar.topic_id, avg(ar.relevance) AS relevance, date_trunc('week', p.date) AS week
FROM activity_relevance ar, protocols p
WHERE ar.protocol_id = p.id
GROUP BY ar.topic_id, date_trunc('week', p.date);


CREATE OR REPLACE VIEW topic_relevance AS
SELECT topic_id, avg(relevance) AS relevance
FROM topic_relevance_weekly
GROUP BY topic_id;


------------------ Sentiment ------------------


CREATE OR REPLACE VIEW activity_dates AS
SELECT 
    a.*,
    COALESCE(p.date, pp.date) AS date
FROM activities a
LEFT JOIN protocols p ON a.protocol_id = p.id
LEFT JOIN printed_papers pp ON a.printed_paper_id = pp.id;


CREATE OR REPLACE VIEW person_topic_sentiment_weekly AS
SELECT am.topic_id, avg(am.sentiment_value) AS sentiment, date_trunc('week', a.date) AS week, r.person_id
FROM activity_mappings am, activity_dates a, roles r
WHERE a.id = am.activity_id
    AND a.role_id = r.id
GROUP BY am.topic_id, date_trunc('week', a.date), r.person_id;


CREATE OR REPLACE VIEW person_topic_sentiment AS
SELECT p.topic_id, avg(p.sentiment) AS sentiment, p.person_id
FROM person_topic_sentiment_weekly p
GROUP BY p.topic_id, p.person_id;


CREATE OR REPLACE VIEW party_topic_sentiment_weekly AS
SELECT p.topic_id, avg(p.sentiment) AS sentiment, p.week, r.group_id
FROM person_topic_sentiment_weekly p, roles r
WHERE p.person_id = r.person_id
GROUP BY p.topic_id, p.week, r.group_id;


CREATE OR REPLACE VIEW party_topic_sentiment AS
SELECT p.topic_id, avg(p.sentiment) AS sentiment, p.group_id
FROM party_topic_sentiment_weekly p
GROUP BY p.topic_id, p.group_id;


CREATE OR REPLACE VIEW topic_sentiment_weekly AS
SELECT topic_id, avg(sentiment) AS sentiment, week
FROM party_topic_sentiment_weekly
GROUP BY topic_id, week;


CREATE OR REPLACE VIEW topic_sentiment AS
SELECT topic_id, avg(sentiment) AS sentiment
FROM topic_sentiment_weekly
GROUP BY topic_id;


------------------ Volatility ------------------


CREATE OR REPLACE VIEW party_topic_volatility AS
WITH monthly_changes AS (
    SELECT 
        topic_id,
        group_id,
        sentiment,
        week,
        ABS(sentiment - LAG(sentiment, 4) OVER (PARTITION BY topic_id, group_id ORDER BY week)) AS sentiment_change
    FROM party_topic_sentiment_weekly
)
SELECT 
    topic_id,
    group_id,
    AVG(sentiment_change) AS volatility
FROM monthly_changes
WHERE sentiment_change IS NOT NULL
GROUP BY topic_id, group_id;


CREATE OR REPLACE VIEW party_volatility AS
SELECT 
    group_id,
    AVG(volatility) AS volatility
FROM party_topic_volatility
GROUP BY group_id;


CREATE OR REPLACE VIEW topic_volatility AS
SELECT 
    topic_id,
    AVG(volatility) AS volatility
FROM party_topic_volatility
GROUP BY topic_id;


------------------ Politician Activity ------------------


CREATE OR REPLACE VIEW politician_activity_per_period AS
SELECT 
    r.person_id,
    r.election_period,
    (COUNT(a.id)::FLOAT / NULLIF(COUNT(DISTINCT a.protocol_id), 0)) AS activities_per_protocol
FROM activities a, roles r
WHERE a.role_id = r.id
GROUP BY r.person_id, r.election_period;


CREATE OR REPLACE VIEW average_politician_activity_per_period AS
SELECT 
    election_period,
    AVG(pa.activities_per_protocol) AS average_activities_per_protocol
FROM politician_activity_per_period pa
GROUP BY election_period;
