package repository

import (
	"database/sql"
	"strings"
	"time"
)

func healthHistorySchema() sqliteSchema {
	return sqliteSchema{
		Name: "health_history",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS health_check_samples (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				node_id TEXT NOT NULL,
				check_type TEXT NOT NULL,
				latency_ms INTEGER NOT NULL DEFAULT 0,
				success INTEGER NOT NULL,
				error_summary TEXT NOT NULL DEFAULT '',
				checked_at TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_health_check_samples_latest
				ON health_check_samples(node_id, check_type, checked_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_health_check_samples_checked_at
				ON health_check_samples(checked_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_health_check_samples_success
				ON health_check_samples(success, checked_at DESC)`,
		},
	}
}

func (s *Store) RecordHealthCheckSample(input HealthCheckSample) (HealthCheckSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	input.NodeID = strings.TrimSpace(input.NodeID)
	input.CheckType = strings.TrimSpace(input.CheckType)
	if input.CheckType == "" {
		input.CheckType = "tcp"
	}
	if input.CheckedAt.IsZero() {
		input.CheckedAt = time.Now().UTC()
	}
	if len(input.ErrorSummary) > 500 {
		input.ErrorSummary = input.ErrorSummary[:500]
	}
	success := 0
	if input.Success {
		success = 1
	}
	result, err := s.db.Exec(`
		INSERT INTO health_check_samples (
			node_id, check_type, latency_ms, success, error_summary, checked_at
		)
		VALUES (?, ?, ?, ?, ?, ?)
	`, input.NodeID, input.CheckType, input.LatencyMS, success, input.ErrorSummary, input.CheckedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return HealthCheckSample{}, err
	}
	input.ID, _ = result.LastInsertId()
	return input, nil
}

func (s *Store) LatestHealthCheckSamples(input HealthCheckQuery) ([]HealthCheckSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	input.Limit = queryLimit(input.Limit, 100, 500)
	where, args := healthCheckWhere(input)
	rows, err := s.db.Query(`
		SELECT id, node_id, check_type, latency_ms, success, error_summary, checked_at
		FROM (
			SELECT
				id, node_id, check_type, latency_ms, success, error_summary, checked_at,
				ROW_NUMBER() OVER (
					PARTITION BY node_id, check_type
					ORDER BY checked_at DESC, id DESC
				) AS rank
			FROM health_check_samples
			WHERE `+where+`
		)
		WHERE rank = 1
		ORDER BY checked_at DESC, id DESC
		LIMIT ?
	`, append(args, input.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanHealthCheckSamples(rows)
}

func (s *Store) HealthCheckTrend(input HealthCheckQuery) (HealthTrendSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	input.Limit = queryLimit(input.Limit, 100, 1000)
	where, args := healthCheckWhere(input)
	var total int
	var successCount int
	var latencySum int
	if err := s.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN success = 1 THEN latency_ms ELSE 0 END), 0)
		FROM health_check_samples
		WHERE `+where,
		args...,
	).Scan(&total, &successCount, &latencySum); err != nil {
		return HealthTrendSummary{}, err
	}
	rows, err := s.db.Query(`
		SELECT id, node_id, check_type, latency_ms, success, error_summary, checked_at
		FROM health_check_samples
		WHERE `+where+`
		ORDER BY checked_at DESC, id DESC
		LIMIT ?
	`, append(args, input.Limit)...)
	if err != nil {
		return HealthTrendSummary{}, err
	}
	defer rows.Close()
	samples, err := scanHealthCheckSamples(rows)
	if err != nil {
		return HealthTrendSummary{}, err
	}
	averageLatency := 0
	if successCount > 0 {
		averageLatency = latencySum / successCount
	}
	return HealthTrendSummary{
		Samples:          samples,
		Total:            total,
		SuccessCount:     successCount,
		FailureCount:     total - successCount,
		AverageLatencyMS: averageLatency,
		Limit:            input.Limit,
	}, nil
}

func (s *Store) CleanupHealthHistory(input HealthHistoryRetention) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	if !input.Before.IsZero() {
		result, err := s.db.Exec(
			`DELETE FROM health_check_samples WHERE checked_at < ?`,
			input.Before.UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return 0, err
		}
		count, _ := result.RowsAffected()
		deleted += int(count)
	}
	if input.MaxPerNode > 0 {
		result, err := s.db.Exec(`
			DELETE FROM health_check_samples
			WHERE id IN (
				SELECT id
				FROM (
					SELECT
						id,
						ROW_NUMBER() OVER (
							PARTITION BY node_id, check_type
							ORDER BY checked_at DESC, id DESC
						) AS rank
					FROM health_check_samples
				)
				WHERE rank > ?
			)
		`, input.MaxPerNode)
		if err != nil {
			return 0, err
		}
		count, _ := result.RowsAffected()
		deleted += int(count)
	}
	return deleted, nil
}

func healthCheckWhere(input HealthCheckQuery) (string, []any) {
	clauses := []string{"1 = 1"}
	args := []any{}
	if value := strings.TrimSpace(input.NodeID); value != "" {
		clauses = append(clauses, "node_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(input.CheckType); value != "" {
		clauses = append(clauses, "check_type = ?")
		args = append(args, value)
	}
	return strings.Join(clauses, " AND "), args
}

func scanHealthCheckSamples(rows *sql.Rows) ([]HealthCheckSample, error) {
	items := []HealthCheckSample{}
	for rows.Next() {
		var item HealthCheckSample
		var success int
		var checkedAt string
		if err := rows.Scan(
			&item.ID,
			&item.NodeID,
			&item.CheckType,
			&item.LatencyMS,
			&success,
			&item.ErrorSummary,
			&checkedAt,
		); err != nil {
			return nil, err
		}
		item.Success = success == 1
		if parsed, err := time.Parse(time.RFC3339Nano, checkedAt); err == nil {
			item.CheckedAt = parsed
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
