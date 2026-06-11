package repository

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func nodeCacheSchema() sqliteSchema {
	return sqliteSchema{
		Name: "node_cache",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS node_cache_nodes (
				node_id TEXT PRIMARY KEY,
				dedup_key TEXT NOT NULL UNIQUE,
				tag TEXT NOT NULL,
				protocol TEXT NOT NULL,
				address TEXT NOT NULL,
				port INTEGER NOT NULL DEFAULT 0,
				source TEXT NOT NULL,
				disabled INTEGER NOT NULL DEFAULT 0,
				node_json TEXT NOT NULL,
				refreshed_at TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_node_cache_nodes_protocol
				ON node_cache_nodes(protocol, tag)`,
			`CREATE INDEX IF NOT EXISTS idx_node_cache_nodes_address
				ON node_cache_nodes(address, port)`,
			`CREATE INDEX IF NOT EXISTS idx_node_cache_nodes_source
				ON node_cache_nodes(source, refreshed_at)`,
			`CREATE INDEX IF NOT EXISTS idx_node_cache_nodes_updated
				ON node_cache_nodes(updated_at)`,
			`CREATE TABLE IF NOT EXISTS node_cache_sources (
				node_id TEXT NOT NULL,
				source_type TEXT NOT NULL,
				source_id TEXT NOT NULL,
				subscription_id TEXT NOT NULL DEFAULT '',
				node_set_id TEXT NOT NULL DEFAULT '',
				refreshed_at TEXT NOT NULL,
				PRIMARY KEY (node_id, source_type, source_id),
				FOREIGN KEY (node_id) REFERENCES node_cache_nodes(node_id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS idx_node_cache_sources_lookup
				ON node_cache_sources(source_type, source_id, refreshed_at)`,
			`CREATE INDEX IF NOT EXISTS idx_node_cache_sources_subscription
				ON node_cache_sources(subscription_id, refreshed_at)`,
			`CREATE INDEX IF NOT EXISTS idx_node_cache_sources_node_set
				ON node_cache_sources(node_set_id, refreshed_at)`,
			`CREATE TABLE IF NOT EXISTS node_cache_tags (
				node_id TEXT NOT NULL,
				tag TEXT NOT NULL,
				PRIMARY KEY (node_id, tag),
				FOREIGN KEY (node_id) REFERENCES node_cache_nodes(node_id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS idx_node_cache_tags_tag
				ON node_cache_tags(tag, node_id)`,
		},
	}
}

func (s *Store) UpsertNodeCache(input NodeCacheUpsert) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sourceType := strings.TrimSpace(string(input.SourceType))
	sourceID := strings.TrimSpace(input.SourceID)
	if sourceType == "" || sourceID == "" {
		return fmt.Errorf("node cache source type and source id are required")
	}
	refreshedAt := input.RefreshedAt
	if refreshedAt.IsZero() {
		refreshedAt = time.Now().UTC()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`DELETE FROM node_cache_sources WHERE source_type = ? AND source_id = ?`,
		sourceType,
		sourceID,
	); err != nil {
		return err
	}
	for _, node := range input.Nodes {
		if strings.TrimSpace(node.Tag) == "" || strings.TrimSpace(node.Type) == "" {
			continue
		}
		if err := upsertNodeCacheNode(tx, input, node, refreshedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) QueryNodeCache(input NodeCacheQuery) (NodeCachePage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	input.Offset = queryOffset(input.Offset)
	input.Limit = queryLimit(input.Limit, 100, 500)
	where, args := nodeCacheWhere(input)
	var total int
	if err := s.db.QueryRow(
		`SELECT COUNT(*)
		FROM node_cache_nodes n
		WHERE `+where,
		args...,
	).Scan(&total); err != nil {
		return NodeCachePage{}, err
	}
	rows, err := s.db.Query(
		`SELECT n.node_json
		FROM node_cache_nodes n
		WHERE `+where+`
		ORDER BY n.tag, n.node_id
		LIMIT ? OFFSET ?`,
		append(args, input.Limit, input.Offset)...,
	)
	if err != nil {
		return NodeCachePage{}, err
	}
	defer rows.Close()
	nodes := []NormalizedNode{}
	for rows.Next() {
		var nodeJSON string
		if err := rows.Scan(&nodeJSON); err != nil {
			return NodeCachePage{}, err
		}
		var node NormalizedNode
		if err := json.Unmarshal([]byte(nodeJSON), &node); err != nil {
			return NodeCachePage{}, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return NodeCachePage{}, err
	}
	nextOffset := input.Offset + len(nodes)
	return NodeCachePage{
		Nodes:      nodes,
		Offset:     input.Offset,
		Limit:      input.Limit,
		Total:      total,
		NextOffset: nextOffset,
		HasMore:    nextOffset < total,
	}, nil
}

func (s *Store) FindNodeCacheNodeByTag(tag string) (string, NormalizedNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var nodeID string
	var nodeJSON string
	err := s.db.QueryRow(`
		SELECT node_id, node_json
		FROM node_cache_nodes
		WHERE tag = ?
		ORDER BY updated_at DESC, node_id
		LIMIT 1
	`, strings.TrimSpace(tag)).Scan(&nodeID, &nodeJSON)
	if err != nil {
		return "", NormalizedNode{}, convertNotFound(err)
	}
	var node NormalizedNode
	if err := json.Unmarshal([]byte(nodeJSON), &node); err != nil {
		return "", NormalizedNode{}, err
	}
	return nodeID, node, nil
}

func upsertNodeCacheNode(tx *sql.Tx, input NodeCacheUpsert, node NormalizedNode, refreshedAt time.Time) error {
	dedupKey := nodeDedupKey(node)
	nodeID := "node_" + hashString(dedupKey)[:16]
	nodeJSON, err := json.Marshal(node)
	if err != nil {
		return err
	}
	timestamp := refreshedAt.UTC().Format(time.RFC3339Nano)
	source := strings.TrimSpace(node.Source)
	if source == "" {
		source = input.SourceID
	}
	if _, err := tx.Exec(`
		INSERT INTO node_cache_nodes (
			node_id, dedup_key, tag, protocol, address, port, source, disabled,
			node_json, refreshed_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)
		ON CONFLICT(dedup_key) DO UPDATE SET
			tag = excluded.tag,
			protocol = excluded.protocol,
			address = excluded.address,
			port = excluded.port,
			source = excluded.source,
			node_json = excluded.node_json,
			refreshed_at = excluded.refreshed_at,
			updated_at = excluded.updated_at
	`, nodeID, dedupKey, node.Tag, node.Type, node.Server, node.ServerPort, source, string(nodeJSON), timestamp, timestamp, timestamp); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO node_cache_sources (
			node_id, source_type, source_id, subscription_id, node_set_id, refreshed_at
		)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, source_type, source_id) DO UPDATE SET
			subscription_id = excluded.subscription_id,
			node_set_id = excluded.node_set_id,
			refreshed_at = excluded.refreshed_at
	`, nodeID, string(input.SourceType), input.SourceID, input.SubscriptionID, input.NodeSetID, timestamp); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM node_cache_tags WHERE node_id = ?`, nodeID); err != nil {
		return err
	}
	for _, tag := range nodeCacheTags(node) {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO node_cache_tags (node_id, tag) VALUES (?, ?)`,
			nodeID,
			tag,
		); err != nil {
			return err
		}
	}
	return nil
}

func nodeCacheWhere(input NodeCacheQuery) (string, []any) {
	clauses := []string{"1 = 1"}
	args := []any{}
	if value := strings.TrimSpace(input.Query); value != "" {
		pattern := "%" + strings.ToLower(value) + "%"
		clauses = append(clauses, `(LOWER(n.tag) LIKE ? OR LOWER(n.protocol) LIKE ? OR LOWER(n.address) LIKE ? OR LOWER(n.source) LIKE ?)`)
		args = append(args, pattern, pattern, pattern, pattern)
	}
	if value := strings.TrimSpace(input.Protocol); value != "" {
		clauses = append(clauses, "n.protocol = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(input.Address); value != "" {
		clauses = append(clauses, "n.address LIKE ?")
		args = append(args, "%"+value+"%")
	}
	if value := strings.TrimSpace(input.Tag); value != "" {
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM node_cache_tags t
			WHERE t.node_id = n.node_id AND t.tag LIKE ?
		)`)
		args = append(args, "%"+value+"%")
	}
	if value := strings.TrimSpace(input.Source); value != "" {
		clauses = append(clauses, "n.source LIKE ?")
		args = append(args, "%"+value+"%")
	}
	if value := strings.TrimSpace(input.SubscriptionID); value != "" {
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM node_cache_sources s
			WHERE s.node_id = n.node_id AND s.subscription_id = ?
		)`)
		args = append(args, value)
	}
	if value := strings.TrimSpace(input.NodeSetID); value != "" {
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM node_cache_sources s
			WHERE s.node_id = n.node_id AND s.node_set_id = ?
		)`)
		args = append(args, value)
	}
	return strings.Join(clauses, " AND "), args
}

func nodeDedupKey(node NormalizedNode) string {
	parts := []string{
		strings.ToLower(strings.TrimSpace(node.Type)),
		strings.ToLower(strings.TrimSpace(node.Server)),
		strconv.Itoa(node.ServerPort),
		strings.TrimSpace(node.Tag),
	}
	return strings.Join(parts, "|")
}

func nodeCacheTags(node NormalizedNode) []string {
	values := []string{}
	for _, value := range []string{node.Tag, node.Source, node.Type} {
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}
	return uniqueStrings(values)
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
