package repository

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

const maxOperationEventContextBytes = 4096

func operationEventSchema() sqliteSchema {
	return sqliteSchema{
		Name: "operation_events",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS operation_events (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				severity TEXT NOT NULL,
				event_type TEXT NOT NULL,
				resource_type TEXT NOT NULL DEFAULT '',
				resource_id TEXT NOT NULL DEFAULT '',
				profile_id TEXT NOT NULL DEFAULT '',
				core TEXT NOT NULL DEFAULT '',
				message TEXT NOT NULL,
				error_code TEXT NOT NULL DEFAULT '',
				context_json TEXT NOT NULL DEFAULT '{}',
				created_at TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_operation_events_created_at
				ON operation_events(created_at DESC, id DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_operation_events_severity
				ON operation_events(severity, created_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_operation_events_type
				ON operation_events(event_type, created_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_operation_events_resource
				ON operation_events(resource_type, resource_id, created_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_operation_events_profile
				ON operation_events(profile_id, created_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_operation_events_core
				ON operation_events(core, created_at DESC)`,
		},
	}
}

func (s *Store) RecordOperationEvent(input OperationEvent) (OperationEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	input = normalizeOperationEvent(input)
	contextJSON, err := safeOperationEventContext(input.Context)
	if err != nil {
		return OperationEvent{}, err
	}
	result, err := s.db.Exec(`
		INSERT INTO operation_events (
			severity, event_type, resource_type, resource_id, profile_id, core,
			message, error_code, context_json, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		input.Severity,
		input.EventType,
		input.ResourceType,
		input.ResourceID,
		input.ProfileID,
		string(input.Core),
		input.Message,
		input.ErrorCode,
		contextJSON,
		input.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return OperationEvent{}, err
	}
	input.ID, _ = result.LastInsertId()
	input.Context = decodeOperationEventContext(contextJSON)
	return input, nil
}

func (s *Store) QueryOperationEvents(input OperationEventQuery) (OperationEventPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	input.Offset = queryOffset(input.Offset)
	input.Limit = queryLimit(input.Limit, 100, 500)
	where, args := operationEventWhere(input)
	var total int
	if err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM operation_events
		WHERE `+where,
		args...,
	).Scan(&total); err != nil {
		return OperationEventPage{}, err
	}
	rows, err := s.db.Query(`
		SELECT id, severity, event_type, resource_type, resource_id, profile_id, core,
			message, error_code, context_json, created_at
		FROM operation_events
		WHERE `+where+`
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, append(args, input.Limit, input.Offset)...)
	if err != nil {
		return OperationEventPage{}, err
	}
	defer rows.Close()
	events, err := scanOperationEvents(rows)
	if err != nil {
		return OperationEventPage{}, err
	}
	nextOffset := input.Offset + len(events)
	return OperationEventPage{
		Events:     events,
		Offset:     input.Offset,
		Limit:      input.Limit,
		Total:      total,
		NextOffset: nextOffset,
		HasMore:    nextOffset < total,
	}, nil
}

func normalizeOperationEvent(input OperationEvent) OperationEvent {
	input.Severity = strings.TrimSpace(input.Severity)
	if input.Severity == "" {
		input.Severity = "info"
	}
	input.EventType = strings.TrimSpace(input.EventType)
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.ProfileID = strings.TrimSpace(input.ProfileID)
	input.Message = strings.TrimSpace(input.Message)
	input.ErrorCode = strings.TrimSpace(input.ErrorCode)
	if input.CreatedAt.IsZero() {
		input.CreatedAt = time.Now().UTC()
	}
	return input
}

func safeOperationEventContext(input map[string]any) (string, error) {
	if input == nil {
		input = map[string]any{}
	}
	redacted := redactEventContext(input)
	data, err := json.Marshal(redacted)
	if err != nil {
		return "", err
	}
	if len(data) > maxOperationEventContextBytes {
		data, err = json.Marshal(map[string]any{
			"truncated": true,
			"keys":      sortedMapKeys(redacted),
		})
		if err != nil {
			return "", err
		}
	}
	return string(data), nil
}

func redactEventContext(input map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range input {
		if isSensitiveEventKey(key) {
			result[key] = "[redacted]"
			continue
		}
		switch typed := value.(type) {
		case map[string]any:
			result[key] = redactEventContext(typed)
		default:
			result[key] = value
		}
	}
	return result
}

func isSensitiveEventKey(key string) bool {
	key = strings.ToLower(key)
	for _, token := range []string{"token", "secret", "password", "credential", "authorization", "config"} {
		if strings.Contains(key, token) {
			return true
		}
	}
	return false
}

func sortedMapKeys(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func operationEventWhere(input OperationEventQuery) (string, []any) {
	clauses := []string{"1 = 1"}
	args := []any{}
	if !input.Since.IsZero() {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, input.Since.UTC().Format(time.RFC3339Nano))
	}
	if !input.Until.IsZero() {
		clauses = append(clauses, "created_at <= ?")
		args = append(args, input.Until.UTC().Format(time.RFC3339Nano))
	}
	if value := strings.TrimSpace(input.Severity); value != "" {
		clauses = append(clauses, "severity = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(input.EventType); value != "" {
		clauses = append(clauses, "event_type = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(input.ResourceType); value != "" {
		clauses = append(clauses, "resource_type = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(input.ResourceID); value != "" {
		clauses = append(clauses, "resource_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(input.ProfileID); value != "" {
		clauses = append(clauses, "profile_id = ?")
		args = append(args, value)
	}
	if input.Core != "" {
		clauses = append(clauses, "core = ?")
		args = append(args, string(input.Core))
	}
	return strings.Join(clauses, " AND "), args
}

func scanOperationEvents(rows *sql.Rows) ([]OperationEvent, error) {
	events := []OperationEvent{}
	for rows.Next() {
		var event OperationEvent
		var core string
		var contextJSON string
		var createdAt string
		if err := rows.Scan(
			&event.ID,
			&event.Severity,
			&event.EventType,
			&event.ResourceType,
			&event.ResourceID,
			&event.ProfileID,
			&core,
			&event.Message,
			&event.ErrorCode,
			&contextJSON,
			&createdAt,
		); err != nil {
			return nil, err
		}
		event.Core = Core(core)
		event.Context = decodeOperationEventContext(contextJSON)
		if parsed, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			event.CreatedAt = parsed
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func decodeOperationEventContext(value string) map[string]any {
	var context map[string]any
	if err := json.Unmarshal([]byte(value), &context); err != nil || context == nil {
		return map[string]any{}
	}
	return context
}
