package repository

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"io"
	"path"
	"sort"
	"strings"
	"sync"
)

//go:embed rule_source_snapshots/*.json.gz
var ruleSourceSnapshotFS embed.FS
var ruleSourceSnapshotCache sync.Map

func builtInRuleSourceIndexSnapshot(repositoryID string) (RuleSourceIndex, bool) {
	repositoryID = strings.TrimSpace(repositoryID)
	if repositoryID == "" {
		return RuleSourceIndex{}, false
	}
	if cached, ok := ruleSourceSnapshotCache.Load(repositoryID); ok {
		index, ok := cached.(RuleSourceIndex)
		return index, ok
	}
	data, err := ruleSourceSnapshotFS.ReadFile("rule_source_snapshots/" + repositoryID + ".json.gz")
	if err != nil {
		return RuleSourceIndex{}, false
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return RuleSourceIndex{}, false
	}
	defer gzipReader.Close()
	data, err = io.ReadAll(gzipReader)
	if err != nil {
		return RuleSourceIndex{}, false
	}
	var index RuleSourceIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return RuleSourceIndex{}, false
	}
	if index.RepositoryID != repositoryID || index.RepositoryID == "" {
		return RuleSourceIndex{}, false
	}
	normalizeRuleSourceIndexEntries(index.Entries)
	sort.Slice(index.Entries, func(i, j int) bool {
		return index.Entries[i].LogicalPath < index.Entries[j].LogicalPath
	})
	index.Path = ""
	index.Directories = nil
	index.Offset = 0
	index.Limit = 0
	index.Total = 0
	index.NextOffset = 0
	index.HasMore = false
	ruleSourceSnapshotCache.Store(repositoryID, index)
	return index, true
}

func normalizeRuleSourceIndexEntries(entries []RuleSourceIndexEntry) {
	for entryIndex := range entries {
		entry := &entries[entryIndex]
		entry.LogicalPath = normalizeRepositoryRootPath(entry.LogicalPath)
		if entry.Name == "" {
			entry.Name = path.Base(entry.LogicalPath)
		}
		if entry.Files == nil {
			entry.Files = map[Core]RuleSourceIndexFile{}
		}
		for core, file := range entry.Files {
			file.Core = core
			file.Path = normalizeRepositoryRootPath(file.Path)
			file.LogicalPath = normalizeRepositoryRootPath(file.LogicalPath)
			if file.Name == "" {
				file.Name = path.Base(file.LogicalPath)
			}
			entry.Files[core] = file
		}
	}
}

func builtInRuleSourceIndexSnapshotPage(repositoryID string, requestedPath string, offset int, limit int) (RuleSourceIndex, bool) {
	snapshot, ok := builtInRuleSourceIndexSnapshot(repositoryID)
	if !ok {
		return RuleSourceIndex{}, false
	}
	indexPath := normalizeRepositoryRootPath(requestedPath)
	indexes := splitRuleSourceIndexByDirectory(snapshot)
	index, ok := indexes[indexPath]
	if !ok {
		index = RuleSourceIndex{
			RepositoryID: snapshot.RepositoryID,
			Owner:        snapshot.Owner,
			Repository:   snapshot.Repository,
			Path:         indexPath,
			Refs:         snapshot.Refs,
			RefreshedAt:  snapshot.RefreshedAt,
			Directories:  []RuleSourceIndexDirectory{},
			Entries:      []RuleSourceIndexEntry{},
		}
	}
	index.Entries, index.Offset, index.Limit, index.Total, index.NextOffset, index.HasMore = paginateRuleSourceIndexEntries(index.Entries, offset, limit)
	return index, true
}

func builtInRuleSourceIndexSnapshotFlatPage(repositoryID string, offset int, limit int) (RuleSourceIndex, bool) {
	snapshot, ok := builtInRuleSourceIndexSnapshot(repositoryID)
	if !ok {
		return RuleSourceIndex{}, false
	}
	snapshot.Path = ""
	snapshot.Directories = nil
	snapshot.Entries, snapshot.Offset, snapshot.Limit, snapshot.Total, snapshot.NextOffset, snapshot.HasMore = paginateRuleSourceIndexEntries(snapshot.Entries, offset, limit)
	return snapshot, true
}

func builtInRuleSourceIndexSnapshotSearch(repositoryID string, query string, limit int, filters RuleSourceIndexSearchFilters) (RuleSourceIndex, bool) {
	snapshot, ok := builtInRuleSourceIndexSnapshot(repositoryID)
	if !ok {
		return RuleSourceIndex{}, false
	}
	query = strings.ToLower(strings.TrimSpace(query))
	matches := []RuleSourceIndexEntry{}
	for _, entry := range snapshot.Entries {
		if query != "" && !strings.Contains(ruleSourceIndexEntrySearchText(entry), query) {
			continue
		}
		if !ruleSourceIndexEntryMatchesFilters(entry, filters) {
			continue
		}
		matches = append(matches, entry)
	}
	snapshot.Path = ""
	snapshot.Directories = []RuleSourceIndexDirectory{}
	snapshot.Entries, snapshot.Offset, snapshot.Limit, snapshot.Total, snapshot.NextOffset, snapshot.HasMore = paginateRuleSourceIndexEntries(matches, filters.Offset, limit)
	return snapshot, true
}

func builtInRuleSourceIndexSnapshotEntry(repositoryID string, logicalPath string) (RuleSourceIndexEntry, bool) {
	snapshot, ok := builtInRuleSourceIndexSnapshot(repositoryID)
	if !ok {
		return RuleSourceIndexEntry{}, false
	}
	logicalPath = normalizeRepositoryRootPath(logicalPath)
	for _, entry := range snapshot.Entries {
		if entry.LogicalPath == logicalPath {
			return entry, true
		}
	}
	return RuleSourceIndexEntry{}, false
}

func paginateRuleSourceIndexEntries(entries []RuleSourceIndexEntry, offset int, limit int) ([]RuleSourceIndexEntry, int, int, int, int, bool) {
	total := len(entries)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	nextOffset := end
	return entries[offset:end], offset, limit, total, nextOffset, nextOffset < total
}

func ruleSourceIndexEntryMatchesFilters(entry RuleSourceIndexEntry, filters RuleSourceIndexSearchFilters) bool {
	if value := normalizeRepositoryRootPath(filters.PathPrefix); value != "" && entry.LogicalPath != value && !strings.HasPrefix(entry.LogicalPath, value+"/") {
		return false
	}
	if filters.Core == "" &&
		strings.TrimSpace(filters.Format) == "" &&
		strings.TrimSpace(filters.Behavior) == "" &&
		filters.Kind == "" {
		return true
	}
	for core, file := range entry.Files {
		if filters.Core != "" && core != filters.Core {
			continue
		}
		if value := strings.TrimSpace(filters.Format); value != "" && file.Format != value {
			continue
		}
		if value := strings.TrimSpace(filters.Behavior); value != "" && file.Behavior != value {
			continue
		}
		if filters.Kind != "" && file.Kind != filters.Kind {
			continue
		}
		return true
	}
	return false
}
