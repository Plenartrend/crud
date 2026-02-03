-- activity_relevance stores protocol relevance
-- activity_mappings stores sentiments regarding topics in activities


-- TODO everything per election period as otherwise group changes cause misrepresentations

--- GENERAL HELPERS ---
CREATE OR REPLACE VIEW analysed_protocols AS (
    SELECT *
    FROM protocols p
    WHERE EXISTS (SELECT 1 from activities a JOIN activity_mappings am ON am.activity_id = a.id WHERE a.protocol_id = p.id)
)

CREATE OR REPLACE FUNCTION get_last_x_protocols(
    _week_date DATE,
    _limit INT
)
RETURNS SETOF protocols
AS $$
    SELECT *
    FROM analysed_protocols p
    WHERE date_trunc('week', p.date) <= _week_date
    ORDER BY p.date DESC
    LIMIT _limit;
$$ LANGUAGE SQL STABLE;

------------------ Relevance ------------------

CREATE OR REPLACE VIEW relevance_per_topic AS
WITH counts AS (
    SELECT
        a.protocol_id,
        am.topic_id,
        COUNT(*)::float AS topic_count,
        SUM(COUNT(*)) OVER (PARTITION BY a.protocol_id) AS total_count
    FROM activity_mappings am
             JOIN activities a ON a.id = am.activity_id
    WHERE am.topic_id IS NOT NULL
    GROUP BY a.protocol_id, am.topic_id
)
SELECT
    protocol_id,
    topic_id,
    topic_count,
    total_count,
    topic_count / total_count AS topic_share
FROM counts;


CREATE OR REPLACE VIEW relevance_per_topic_per_person AS
WITH counts AS (
    SELECT
        a.protocol_id,
        am.topic_id,
        r.person_id,
        COUNT(*)::float AS topic_count,
        SUM(COUNT(*)) OVER (PARTITION BY a.protocol_id) AS total_count
    FROM activity_mappings am
             JOIN activities a ON a.id = am.activity_id
             JOIN roles r ON r.id = a.role_id
    WHERE am.topic_id IS NOT NULL
    GROUP BY a.protocol_id, am.topic_id, r.person_id
)
SELECT
    protocol_id,
    topic_id,
    person_id,
    topic_count,
    total_count,
    topic_count / total_count AS topic_share
FROM counts;


CREATE OR REPLACE VIEW relevance_per_topic_per_group AS
WITH counts AS (
    SELECT
        a.protocol_id,
        am.topic_id,
        r.group_id,
        COUNT(*)::float AS topic_count,
        SUM(COUNT(*)) OVER (PARTITION BY a.protocol_id) AS total_count
    FROM activity_mappings am
             JOIN activities a ON a.id = am.activity_id
             JOIN roles r ON r.id = a.role_id
    WHERE am.topic_id IS NOT NULL
    GROUP BY a.protocol_id, am.topic_id, r.group_id
)
SELECT
    protocol_id,
    topic_id,
    group_id,
    topic_count,
    total_count,
    topic_count / total_count AS topic_share
FROM counts;

------------------ Relevance over last x weeks ------------------

CREATE OR REPLACE FUNCTION get_topic_shares(
    _week_date DATE,
    _limit INT,
    _group_id INT DEFAULT NULL,
    _person_id INT DEFAULT NULL
)
RETURNS TABLE (topic_id INT, topic_share FLOAT) 
AS $$
    WITH counts AS (
        SELECT
            am.topic_id,
            COUNT(*)::float AS topic_count,
            SUM(COUNT(*)) OVER () AS total_count
        FROM activity_mappings am
        JOIN activities a ON a.id = am.activity_id
        JOIN roles r ON r.id = a.role_id
        WHERE am.topic_id IS NOT NULL
          AND a.protocol_id IN (SELECT id FROM get_last_x_protocols(_week_date, _limit))
          AND (_group_id IS NULL OR r.group_id = _group_id)
          AND (_person_id IS NULL OR r.person_id = _person_id)
        GROUP BY am.topic_id
    )
    SELECT
        topic_id,
        topic_count / NULLIF(total_count, 0)
    FROM counts;
$$ LANGUAGE SQL STABLE;

------------------ Sentiment over last x weeks ------------------

CREATE OR REPLACE FUNCTION get_topic_sentiment(
    _week_date DATE,
    _limit INT,
    _group_id INT DEFAULT NULL,
    _person_id INT DEFAULT NULL
)
RETURNS TABLE (topic_id INT, avg_sentiment FLOAT) 
AS $$
    SELECT
        am.topic_id,
        AVG(am.sentiment_value)::float
    FROM activity_mappings am
    JOIN activities a ON a.id = am.activity_id
    JOIN roles r ON r.id = a.role_id
    WHERE am.topic_id IS NOT NULL
      AND a.protocol_id IN (SELECT id FROM get_last_x_protocols(_week_date, _limit))
      AND (_group_id IS NULL OR r.group_id = _group_id)
      AND (_person_id IS NULL OR r.person_id = _person_id)
    GROUP BY am.topic_id;
$$ LANGUAGE SQL STABLE;

--- ... ---

CREATE OR REPLACE VIEW topic_relevance_weekly AS
SELECT ar.topic_id, avg(ar.relevance) AS relevance, date_trunc('week', p.date) AS week
FROM activity_relevance ar, protocols p
WHERE ar.protocol_id = p.id
GROUP BY ar.topic_id, date_trunc('week', p.date);


CREATE OR REPLACE VIEW topic_relevance AS
SELECT topic_id, avg(relevance) AS relevance
FROM topic_relevance_weekly
GROUP BY topic_id;


---Combined---

CREATE OR REPLACE FUNCTION get_topic_analytics(
    _week_date DATE,
    _limit INT,
    _group_id INT DEFAULT NULL,
    _person_id INT DEFAULT NULL
)
RETURNS TABLE (
    cluster_id INT, 
    topic_relevance FLOAT, 
    avg_sentiment FLOAT
) 
AS $$
    WITH counts AS (
        SELECT
            c.id as cluster_id,
            COUNT(*)::float AS topic_count,
            SUM(COUNT(*)) OVER () AS total_count,
            AVG(am.sentiment_value)::float AS sentiment_agg
        FROM activity_mappings am
        JOIN activities a ON a.id = am.activity_id
        JOIN roles r ON r.id = a.role_id JOIN topics t ON t.id = am.topic_id JOIN topic_clusters c ON c.id = t.cluster_id
          WHERE a.protocol_id IN (SELECT id FROM get_last_x_protocols(_week_date, _limit))
          AND (_group_id IS NULL OR r.group_id = _group_id)
          AND (_person_id IS NULL OR r.person_id = _person_id)
        GROUP BY c.id
    )
    SELECT
        cluster_id,
        LEAST(
                ROUND(((topic_count / NULLIF(total_count, 0)) * 1000)::numeric, 2),
                100
        )::float AS topic_relevance,
        sentiment_agg::float AS avg_sentiment
    FROM counts;
$$ LANGUAGE SQL STABLE;

--TODO: If needed somewhere we can like with _per_person group by topic as well and make the attribute optional
CREATE OR REPLACE FUNCTION get_topic_analytics_per_party(
    _week_date DATE,
    _limit INT,
    _cluster_id INT
)
    RETURNS TABLE (
        group_id INT,
        topic_relevance FLOAT,
        avg_sentiment FLOAT
    )
AS $$
WITH counts AS (
    SELECT
        r.group_id,
        COUNT(*)::float AS topic_count,
        SUM(COUNT(*)) OVER () AS total_count,
        AVG(am.sentiment_value)::float AS sentiment_agg
    FROM activity_mappings am
             JOIN activities a ON a.id = am.activity_id
             JOIN roles r ON r.id = a.role_id
             JOIN topics t ON t.id = am.topic_id JOIN topic_clusters c ON c.id = t.cluster_id
    WHERE c.id = _cluster_id
      AND a.protocol_id IN (SELECT id FROM get_last_x_protocols(_week_date, _limit))
    GROUP BY r.group_id
)
SELECT
    group_id,
    LEAST(
                ROUND(((topic_count / NULLIF(total_count, 0)) * 1000)::numeric, 2),
                100
        )::float AS topic_relevance,
    sentiment_agg::float AS avg_sentiment
FROM counts;
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION get_topic_analytics_per_person(
    _week_date DATE,
    _limit INT,
    _cluster_id INT DEFAULT NULL
)
    RETURNS TABLE (
        person_id INT,
        cluster_id INT,
        topic_relevance FLOAT,
        avg_sentiment FLOAT
    )
AS $$
WITH counts AS (
    SELECT
        r.person_id,
        c.id as cluster_id,
        COUNT(*)::float AS topic_count,
        SUM(COUNT(*)) OVER () AS total_count,
        AVG(am.sentiment_value)::float AS sentiment_agg
    FROM activity_mappings am
             JOIN activities a ON a.id = am.activity_id
             JOIN roles r ON r.id = a.role_id
             JOIN topics t ON t.id = am.topic_id JOIN topic_clusters c ON c.id = t.cluster_id
    WHERE (_cluster_id IS NULL OR c.id = _cluster_id)
      AND a.protocol_id IN (SELECT id FROM get_last_x_protocols(_week_date, _limit))
    GROUP BY r.person_id, c.id
)
SELECT
    person_id,
    cluster_id,
    LEAST(
                ROUND(((topic_count / NULLIF(total_count, 0)) * 1000)::numeric, 2),
                100
        )::float AS topic_relevance,
    sentiment_agg::float AS avg_sentiment
FROM counts;
$$ LANGUAGE SQL STABLE;


------------------ Most positive polititians ------------------

-- Use queries for sentiment and relevance over e.g. the last 10 protocols, and get top and lowest ten relevance * sentiment --

CREATE OR REPLACE FUNCTION get_most_active(
    _week_date DATE,
    _limit INT,
    _num_of_politicians_per_side INT,
    _cluster_id INT DEFAULT NULL
)
RETURNS TABLE (
    person_id INT,
    score FLOAT,
    ranking_type TEXT
) AS $$
    WITH person_stats AS (
        -- We aggregate by person_id for the given protocols
        SELECT
            r.person_id,
            COUNT(*)::float / SUM(COUNT(*)) OVER () AS topic_share,
            AVG(am.sentiment_value)::float AS avg_sentiment
        FROM activity_mappings am
        JOIN activities a ON a.id = am.activity_id
        JOIN roles r ON r.id = a.role_id JOIN topics t ON t.id = am.topic_id JOIN topic_clusters c ON c.id = t.cluster_id
        WHERE c.id = _cluster_id
          AND a.protocol_id IN (SELECT id FROM get_last_x_protocols(_week_date, _limit))
        GROUP BY r.person_id
    ),
    scored_people AS (
        SELECT 
            person_id, 
            (topic_share * avg_sentiment) AS activity_score
        FROM person_stats
    )
    (
        SELECT person_id, activity_score, 'HIGHEST'
        FROM scored_people
        WHERE activity_score IS NOT NULL AND activity_score > 0
        ORDER BY activity_score DESC
        LIMIT _num_of_politicians_per_side
    )
    UNION ALL
    (
        SELECT person_id, activity_score, 'LOWEST'
        FROM scored_people
        WHERE activity_score IS NOT NULL AND activity_score < 0
        ORDER BY activity_score ASC
        LIMIT _num_of_politicians_per_side
    );
$$ LANGUAGE SQL STABLE;

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


------------------ Time Series Analytics (Optimized) ------------------

-- Generates time series analytics for all weeks in a date range in a single query
-- This replaces the need to call get_topic_analytics() in a loop for each week
-- Performance improvement: O(n) queries -> O(1) query
CREATE OR REPLACE FUNCTION get_time_series_analytics(
    _start_date DATE,
    _end_date DATE,
    _lookback_limit INT DEFAULT 20,
    _cluster_id INT DEFAULT NULL,
    _person_id INT DEFAULT NULL,
    _group_id INT DEFAULT NULL
)
    RETURNS TABLE (
                      week_date DATE,
                      topic_relevance FLOAT,
                      avg_sentiment FLOAT
                  )
AS $$
WITH week_series AS (
    -- Generate all Monday dates (start of ISO weeks) in the range
    SELECT date_trunc('week', d)::date AS week_start
    FROM generate_series(_start_date, _end_date, '1 week'::interval) AS d
),
     protocols_per_week AS (
         -- For each week, find the protocols to include (last _lookback_limit protocols up to that week)
         SELECT
             ws.week_start,
             p.id AS protocol_id
         FROM week_series ws
                  CROSS JOIN LATERAL (
             SELECT id, date
             FROM analysed_protocols
             WHERE date_trunc('week', date) <= ws.week_start
             ORDER BY date DESC
             LIMIT _lookback_limit
             ) p
     ),
     activity_data AS (
         -- Get all relevant activity mappings for the protocols in our window
         SELECT
             pw.week_start,
             c.id as cluster_id,
             am.sentiment_value,
             r.person_id,
             r.group_id
         FROM protocols_per_week pw
                  JOIN activities a ON a.protocol_id = pw.protocol_id
                  JOIN activity_mappings am ON am.activity_id = a.id
                  JOIN roles r ON r.id = a.role_id
                  JOIN topics t ON t.id = am.topic_id JOIN topic_clusters c ON c.id = t.cluster_id
           WHERE (_person_id IS NULL OR r.person_id = _person_id)
           AND (_group_id IS NULL OR r.group_id = _group_id)
     ),
     weekly_totals AS (
         -- Calculate total activity count per week (across all topics)
         SELECT
             ad.week_start,
             COUNT(*)::float AS total_count
         FROM activity_data ad
         GROUP BY ad.week_start
     ),
     weekly_stats AS (
         -- Calculate topic-specific counts and sentiment per week
         SELECT
             ad.week_start,
             COUNT(*)::float AS topic_count,
             AVG(ad.sentiment_value)::float AS sentiment_agg
         FROM activity_data ad
         WHERE (_cluster_id IS NULL OR ad.cluster_id = _cluster_id)
         GROUP BY ad.week_start
     )
SELECT
    ws.week_start AS week_date,
    LEAST(
        COALESCE(
            ROUND(((wst.topic_count / NULLIF(wt.total_count, 0)) * 1000)::numeric, 2),
            0 -- Default to 0 if data is missing
        ),
        100
    )::float AS topic_relevance,
    COALESCE(wst.sentiment_agg, 0.0) AS avg_sentiment
FROM week_series ws
         LEFT JOIN weekly_stats wst ON ws.week_start = wst.week_start
         LEFT JOIN weekly_totals wt ON ws.week_start = wt.week_start
ORDER BY ws.week_start ASC;

$$ LANGUAGE SQL STABLE;


CREATE OR REPLACE FUNCTION power_mean(p_value NUMERIC, vals NUMERIC[])
RETURNS NUMERIC AS $$
BEGIN
    RETURN POWER(
        (SELECT AVG(POWER(v, p_value)) FROM unnest(vals) AS v), 
        1.0 / p_value
    );
END;
$$ LANGUAGE plpgsql;


CREATE OR REPLACE FUNCTION get_volatility_for_election_period(
    _election_period INT,
    _person_ids INT[] DEFAULT NULL,
    _group_id INT DEFAULT NULL,
    _cluster_id INT DEFAULT NULL
)
    RETURNS TABLE (
                      person_id INT,
                      volatility FLOAT
                  )
AS $$
WITH std_dev_per_person_topic AS (
    SELECT r.person_id, c.id as cluster_id, stddev_pop(am.sentiment_value) as std_dev
    FROM activity_mappings am 
    JOIN activities a ON a.id = am.activity_id
    JOIN roles r ON r.id = a.role_id 
    JOIN protocols p ON p.id = a.protocol_id
    JOIN topics t ON t.id = am.topic_id JOIN topic_clusters c ON c.id = t.cluster_id
    WHERE p.election_period = _election_period
      AND (_person_ids IS NULL OR r.person_id = ANY(_person_ids))
      AND (_group_id IS NULL OR r.group_id = _group_id)
      AND (_cluster_id IS NULL OR c.id = _cluster_id)
    GROUP BY r.person_id, c.id
    HAVING COUNT(*) > 1
)
SELECT sppt.person_id, COALESCE(AVG(sppt.std_dev), 0.0) as volatility
FROM std_dev_per_person_topic sppt
GROUP BY sppt.person_id;
$$ LANGUAGE SQL STABLE;


CREATE OR REPLACE FUNCTION get_time_series_activity(
    _start_date DATE,
    _end_date DATE,
    _person_id INT DEFAULT NULL,
    _group_id INT DEFAULT NULL
)
RETURNS TABLE (
    month_date DATE,
    speech_count BIGINT
) 
LANGUAGE sql
STABLE
AS $$
WITH month_series AS (
    SELECT generate_series(
        date_trunc('month', _start_date)::date, 
        date_trunc('month', _end_date)::date, 
        '1 month'::interval
    )::date AS m_start
),
activity_counts AS (
    SELECT
        date_trunc('month', ap.date)::date AS m_start,
        COUNT(a.id) AS total
    FROM analysed_protocols ap
    JOIN activities a ON a.protocol_id = ap.id
    JOIN roles r ON r.id = a.role_id
    WHERE ap.date BETWEEN _start_date AND _end_date
      AND a.type LIKE 'Rede%'
      AND (_person_id IS NULL OR r.person_id = _person_id)
      AND (_group_id IS NULL OR r.group_id = _group_id)
    GROUP BY m_start
)
SELECT
    ms.m_start,
    COALESCE(ac.total, 0)
FROM month_series ms
LEFT JOIN activity_counts ac ON ms.m_start = ac.m_start
ORDER BY ms.m_start;
$$;