package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidRuleSourceRepository = errors.New("invalid rule source repository")
	ErrInvalidSingBoxRuleSet       = errors.New("invalid sing-box rule set")
	ErrInvalidMihomoRuleProvider   = errors.New("invalid mihomo rule provider")
	ErrUnsupportedRepositoryCore   = errors.New("repository does not support requested core")
	ErrBuiltInRepositoryReadOnly   = errors.New("built-in repositories are read-only")
	ErrRuleSourceTreeLookup        = errors.New("rule source tree lookup failed")
)

func BuiltInRuleSourceRepositories() []RuleSourceRepository {
	now := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	items := []RuleSourceRepository{
		{
			Metadata: Metadata{
				ID:          "metacubex-meta-rules-dat",
				Kind:        KindRuleSourceRepo,
				Name:        "MetaCubeX/meta-rules-dat",
				Description: "Built-in MetaCubeX rule source repository",
				OriginType:  OriginManual,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			Provider:   RuleSourceRepositoryProviderGitHub,
			Owner:      "MetaCubeX",
			Repository: "meta-rules-dat",
			BuiltIn:    true,
			CoreMappings: []RuleSourceCoreMapping{
				{Core: CoreSingBox, Ref: "sing"},
				{Core: CoreMihomo, Ref: "meta"},
			},
			SupportedCores: []Core{CoreMihomo, CoreSingBox},
		},
	}
	return items
}

func normalizeRuleSourceRepository(input RuleSourceRepository, current RuleSourceRepository) (RuleSourceRepository, error) {
	now := time.Now().UTC()
	item := input
	item.Kind = KindRuleSourceRepo
	item.Name = chooseName(item.Name, current.Name, "Untitled rule source repository")
	item.ID = chooseString(item.ID, current.ID, item.Name)
	item.OriginType = chooseOrigin(item.OriginType, current.OriginType)
	item.Description = chooseString(item.Description, current.Description)
	if item.Provider == "" {
		item.Provider = chooseRepositoryProvider(current.Provider)
	}
	item.Owner = chooseString(item.Owner, current.Owner)
	item.Repository = chooseString(item.Repository, current.Repository)
	item.BuiltIn = current.BuiltIn
	if current.CreatedAt.IsZero() {
		item.CreatedAt = now
	} else {
		item.CreatedAt = current.CreatedAt
	}
	item.UpdatedAt = now

	mappings, err := normalizeRuleSourceCoreMappings(item.CoreMappings)
	if err != nil {
		return RuleSourceRepository{}, err
	}
	item.CoreMappings = mappings
	item.SupportedCores = supportedCoresFromMappings(mappings)

	if item.Provider != RuleSourceRepositoryProviderGitHub {
		return RuleSourceRepository{}, fmt.Errorf("%w: provider %q is not supported", ErrInvalidRuleSourceRepository, item.Provider)
	}
	if strings.TrimSpace(item.Owner) == "" || strings.TrimSpace(item.Repository) == "" {
		return RuleSourceRepository{}, fmt.Errorf("%w: owner and repository are required", ErrInvalidRuleSourceRepository)
	}
	return item, nil
}

func chooseRepositoryProvider(current RuleSourceRepositoryProvider) RuleSourceRepositoryProvider {
	if current != "" {
		return current
	}
	return RuleSourceRepositoryProviderGitHub
}

func normalizeRuleSourceCoreMappings(input []RuleSourceCoreMapping) ([]RuleSourceCoreMapping, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("%w: at least one core mapping is required", ErrInvalidRuleSourceRepository)
	}
	items := make([]RuleSourceCoreMapping, 0, len(input))
	seen := map[Core]struct{}{}
	for _, mapping := range input {
		mapping.Core = Core(strings.TrimSpace(string(mapping.Core)))
		mapping.Ref = strings.TrimSpace(mapping.Ref)
		mapping.RootPath = normalizeRepositoryRootPath(mapping.RootPath)
		if mapping.Core == "" {
			return nil, fmt.Errorf("%w: core mapping core is required", ErrInvalidRuleSourceRepository)
		}
		if mapping.Ref == "" {
			return nil, fmt.Errorf("%w: core %s requires ref", ErrInvalidRuleSourceRepository, mapping.Core)
		}
		if _, ok := seen[mapping.Core]; ok {
			return nil, fmt.Errorf("%w: duplicate core mapping for %s", ErrInvalidRuleSourceRepository, mapping.Core)
		}
		seen[mapping.Core] = struct{}{}
		items = append(items, mapping)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Core < items[j].Core
	})
	return items, nil
}

func normalizeRepositoryRootPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == "/" {
		return ""
	}
	clean := path.Clean("/" + value)
	return strings.TrimPrefix(clean, "/")
}

func supportedCoresFromMappings(mappings []RuleSourceCoreMapping) []Core {
	cores := make([]Core, 0, len(mappings))
	for _, mapping := range mappings {
		cores = append(cores, mapping.Core)
	}
	sort.Slice(cores, func(i, j int) bool {
		return cores[i] < cores[j]
	})
	return cores
}

func normalizeSingBoxRuleSet(input SingBoxRuleSetResource, current SingBoxRuleSetResource) (SingBoxRuleSetResource, error) {
	now := time.Now().UTC()
	item := input
	item.Kind = KindSingBoxRuleSet
	item.Tag = strings.TrimSpace(item.Tag)
	item.Name = chooseName(item.Name, current.Name, item.Tag, "Untitled sing-box rule-set")
	item.ID = chooseString(item.ID, current.ID, item.Tag, item.Name)
	item.OriginType = chooseOrigin(item.OriginType, current.OriginType)
	item.Description = chooseString(item.Description, current.Description)
	if current.CreatedAt.IsZero() {
		item.CreatedAt = now
	} else {
		item.CreatedAt = current.CreatedAt
	}
	item.UpdatedAt = now

	item.SourceMode = RuleAssetSourceMode(strings.TrimSpace(string(item.SourceMode)))
	item.RepositoryID = strings.TrimSpace(item.RepositoryID)
	item.Ref = strings.TrimSpace(item.Ref)
	item.Path = normalizeRepositoryRootPath(item.Path)
	item.URL = strings.TrimSpace(item.URL)
	item.LocalPath = strings.TrimSpace(item.LocalPath)
	item.Format = strings.TrimSpace(item.Format)
	item.UpdateInterval = strings.TrimSpace(item.UpdateInterval)

	if item.Tag == "" {
		return SingBoxRuleSetResource{}, fmt.Errorf("%w: tag is required", ErrInvalidSingBoxRuleSet)
	}
	if item.Format == "" {
		item.Format = inferSingBoxRuleSetFormat(filepath.Ext(ruleAssetSourcePath(item.SourceMode, item.Path, item.URL, item.LocalPath)))
	}
	if item.Format != "source" && item.Format != "binary" {
		return SingBoxRuleSetResource{}, fmt.Errorf("%w: sing-box format must be source or binary", ErrInvalidSingBoxRuleSet)
	}
	switch item.SourceMode {
	case RuleAssetSourceModeRepositoryFile:
		if item.RepositoryID == "" || item.Path == "" {
			return SingBoxRuleSetResource{}, fmt.Errorf("%w: repository-file rule set requires repositoryId and path", ErrInvalidSingBoxRuleSet)
		}
	case RuleAssetSourceModeRemote:
		if item.URL == "" {
			return SingBoxRuleSetResource{}, fmt.Errorf("%w: remote rule set requires url", ErrInvalidSingBoxRuleSet)
		}
	case RuleAssetSourceModeLocal:
		if item.LocalPath == "" {
			return SingBoxRuleSetResource{}, fmt.Errorf("%w: local rule set requires localPath", ErrInvalidSingBoxRuleSet)
		}
	default:
		return SingBoxRuleSetResource{}, fmt.Errorf("%w: unsupported sourceMode %q", ErrInvalidSingBoxRuleSet, item.SourceMode)
	}
	return item, nil
}

func normalizeMihomoRuleProvider(input MihomoRuleProviderResource, current MihomoRuleProviderResource) (MihomoRuleProviderResource, error) {
	now := time.Now().UTC()
	item := input
	item.Kind = KindMihomoRuleProvider
	item.Provider = strings.TrimSpace(item.Provider)
	item.Name = chooseName(item.Name, current.Name, item.Provider, "Untitled mihomo rule-provider")
	item.ID = chooseString(item.ID, current.ID, item.Provider, item.Name)
	item.OriginType = chooseOrigin(item.OriginType, current.OriginType)
	item.Description = chooseString(item.Description, current.Description)
	if current.CreatedAt.IsZero() {
		item.CreatedAt = now
	} else {
		item.CreatedAt = current.CreatedAt
	}
	item.UpdatedAt = now

	item.SourceMode = RuleAssetSourceMode(strings.TrimSpace(string(item.SourceMode)))
	item.RepositoryID = strings.TrimSpace(item.RepositoryID)
	item.Ref = strings.TrimSpace(item.Ref)
	item.Path = normalizeRepositoryRootPath(item.Path)
	item.URL = strings.TrimSpace(item.URL)
	item.LocalPath = strings.TrimSpace(item.LocalPath)
	item.Behavior = strings.TrimSpace(item.Behavior)
	item.Format = strings.TrimSpace(item.Format)
	item.Interval = strings.TrimSpace(item.Interval)

	if item.Provider == "" {
		return MihomoRuleProviderResource{}, fmt.Errorf("%w: provider is required", ErrInvalidMihomoRuleProvider)
	}
	sourcePath := ruleAssetSourcePath(item.SourceMode, item.Path, item.URL, item.LocalPath)
	if item.Format == "" {
		item.Format = inferMihomoRuleProviderFormat(filepath.Ext(sourcePath))
	}
	if item.Format == "" {
		item.Format = "yaml"
	}
	if !slices.Contains([]string{"yaml", "text", "mrs"}, item.Format) {
		return MihomoRuleProviderResource{}, fmt.Errorf("%w: mihomo format must be yaml, text, or mrs", ErrInvalidMihomoRuleProvider)
	}
	if item.Behavior == "" {
		item.Behavior = inferMihomoRuleProviderBehavior(sourcePath)
	}
	if !slices.Contains([]string{"domain", "ipcidr", "classical"}, item.Behavior) {
		return MihomoRuleProviderResource{}, fmt.Errorf("%w: mihomo behavior must be domain, ipcidr, or classical", ErrInvalidMihomoRuleProvider)
	}
	if item.Format == "mrs" && item.Behavior == "classical" {
		return MihomoRuleProviderResource{}, fmt.Errorf("%w: mrs format does not support classical behavior", ErrInvalidMihomoRuleProvider)
	}
	switch item.SourceMode {
	case RuleAssetSourceModeRepositoryFile:
		if item.RepositoryID == "" || item.Path == "" {
			return MihomoRuleProviderResource{}, fmt.Errorf("%w: repository-file provider requires repositoryId and path", ErrInvalidMihomoRuleProvider)
		}
	case RuleAssetSourceModeRemote:
		if item.URL == "" {
			return MihomoRuleProviderResource{}, fmt.Errorf("%w: remote provider requires url", ErrInvalidMihomoRuleProvider)
		}
	case RuleAssetSourceModeLocal:
		if item.LocalPath == "" {
			return MihomoRuleProviderResource{}, fmt.Errorf("%w: local provider requires localPath", ErrInvalidMihomoRuleProvider)
		}
	default:
		return MihomoRuleProviderResource{}, fmt.Errorf("%w: unsupported sourceMode %q", ErrInvalidMihomoRuleProvider, item.SourceMode)
	}
	return item, nil
}

func ruleAssetSourcePath(mode RuleAssetSourceMode, repositoryPath string, rawURL string, localPath string) string {
	switch mode {
	case RuleAssetSourceModeRepositoryFile:
		return repositoryPath
	case RuleAssetSourceModeRemote:
		if parsed, err := url.Parse(rawURL); err == nil && parsed.Path != "" {
			return parsed.Path
		}
		return rawURL
	case RuleAssetSourceModeLocal:
		return localPath
	default:
		return ""
	}
}

func builtInRuleSourceRepository(id string) (RuleSourceRepository, bool) {
	index := slices.IndexFunc(BuiltInRuleSourceRepositories(), func(item RuleSourceRepository) bool {
		return item.ID == id
	})
	if index < 0 {
		return RuleSourceRepository{}, false
	}
	return BuiltInRuleSourceRepositories()[index], true
}

func findRuleSourceCoreMapping(repo RuleSourceRepository, core Core) (RuleSourceCoreMapping, error) {
	for _, mapping := range repo.CoreMappings {
		if mapping.Core == core {
			return mapping, nil
		}
	}
	return RuleSourceCoreMapping{}, fmt.Errorf("%w: repository %s has no mapping for %s", ErrUnsupportedRepositoryCore, repo.ID, core)
}

type RuleSourceRepositoryBrowser struct {
	client              *http.Client
	githubAPIBase       string
	githubTokenProvider func() string
}

func NewRuleSourceRepositoryBrowser() *RuleSourceRepositoryBrowser {
	return &RuleSourceRepositoryBrowser{
		client:        &http.Client{Timeout: 20 * time.Second},
		githubAPIBase: "https://api.github.com",
	}
}

func (b *RuleSourceRepositoryBrowser) SetGitHubTokenProvider(provider func() string) {
	b.githubTokenProvider = provider
}

func (b *RuleSourceRepositoryBrowser) Browse(repo RuleSourceRepository, core Core, requestedPath string) (RuleSourceTree, error) {
	mapping, err := findRuleSourceCoreMapping(repo, core)
	if err != nil {
		return RuleSourceTree{}, err
	}
	switch repo.Provider {
	case RuleSourceRepositoryProviderGitHub:
		return b.browseGitHub(repo, mapping, requestedPath)
	default:
		return RuleSourceTree{}, fmt.Errorf("%w: unsupported provider %q", ErrRuleSourceTreeLookup, repo.Provider)
	}
}

func (b *RuleSourceRepositoryBrowser) RefreshSelectableFiles(repo RuleSourceRepository, core Core) (RuleSourceSelectableFiles, error) {
	mapping, err := findRuleSourceCoreMapping(repo, core)
	if err != nil {
		return RuleSourceSelectableFiles{}, err
	}
	switch repo.Provider {
	case RuleSourceRepositoryProviderGitHub:
		return b.refreshGitHubSelectableFiles(repo, mapping)
	default:
		return RuleSourceSelectableFiles{}, fmt.Errorf("%w: unsupported provider %q", ErrRuleSourceTreeLookup, repo.Provider)
	}
}

func (b *RuleSourceRepositoryBrowser) RefreshIndex(repo RuleSourceRepository) (RuleSourceIndex, error) {
	return b.refreshIndex(repo, true)
}

func (b *RuleSourceRepositoryBrowser) RefreshRemoteIndex(repo RuleSourceRepository) (RuleSourceIndex, error) {
	return b.refreshIndex(repo, false)
}

func (b *RuleSourceRepositoryBrowser) refreshIndex(repo RuleSourceRepository, allowSnapshotFallback bool) (RuleSourceIndex, error) {
	pathsByCore := map[Core][]string{}
	for _, mapping := range repo.CoreMappings {
		switch repo.Provider {
		case RuleSourceRepositoryProviderGitHub:
			files, err := b.refreshGitHubIndexFiles(repo, mapping)
			if err != nil {
				if allowSnapshotFallback {
					if snapshot, ok := builtInRuleSourceIndexSnapshot(repo.ID); ok {
						return snapshot, nil
					}
				}
				return RuleSourceIndex{}, err
			}
			for _, file := range files {
				pathsByCore[mapping.Core] = append(pathsByCore[mapping.Core], file.Path)
			}
		default:
			return RuleSourceIndex{}, fmt.Errorf("%w: unsupported provider %q", ErrRuleSourceTreeLookup, repo.Provider)
		}
	}
	return BuildRuleSourceIndexFromFiles(repo, pathsByCore), nil
}

func BuildRuleSourceIndexFromFiles(repo RuleSourceRepository, pathsByCore map[Core][]string) RuleSourceIndex {
	entriesByLogicalPath := map[string]RuleSourceIndexEntry{}
	refs := map[Core]string{}
	for _, mapping := range repo.CoreMappings {
		refs[mapping.Core] = mapping.Ref
		for _, filePath := range pathsByCore[mapping.Core] {
			if ruleSourceIndexPathIgnored(filePath) {
				continue
			}
			file, ok := inferIndexFile(repo, mapping.Core, filePath)
			if !ok {
				continue
			}
			entry := entriesByLogicalPath[file.LogicalPath]
			if entry.Files == nil {
				entry.Files = map[Core]RuleSourceIndexFile{}
			}
			entry.LogicalPath = file.LogicalPath
			entry.Name = file.Name
			entry.Files[file.Core] = file
			entriesByLogicalPath[file.LogicalPath] = entry
		}
	}
	entries := make([]RuleSourceIndexEntry, 0, len(entriesByLogicalPath))
	for _, entry := range entriesByLogicalPath {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LogicalPath < entries[j].LogicalPath
	})

	return RuleSourceIndex{
		RepositoryID: repo.ID,
		Owner:        repo.Owner,
		Repository:   repo.Repository,
		Refs:         refs,
		RefreshedAt:  time.Now().UTC(),
		Entries:      entries,
	}
}

func ruleSourceIndexPathIgnored(filePath string) bool {
	return strings.HasPrefix(normalizeRepositoryRootPath(filePath), "asn/")
}

func (b *RuleSourceRepositoryBrowser) browseGitHub(repo RuleSourceRepository, mapping RuleSourceCoreMapping, requestedPath string) (RuleSourceTree, error) {
	normalizedPath := normalizeRepositoryRootPath(requestedPath)
	fullPath := normalizeRepositoryRootPath(path.Join(mapping.RootPath, normalizedPath))
	endpoint := strings.TrimRight(b.githubAPIBase, "/") +
		"/repos/" + url.PathEscape(repo.Owner) +
		"/" + url.PathEscape(repo.Repository) +
		"/contents"
	if fullPath != "" {
		endpoint += "/" + url.PathEscape(fullPath)
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return RuleSourceTree{}, err
	}
	query := req.URL.Query()
	query.Set("ref", mapping.Ref)
	req.URL.RawQuery = query.Encode()
	b.setGitHubRequestHeaders(req)

	resp, err := b.client.Do(req)
	if err != nil {
		return RuleSourceTree{}, fmt.Errorf("%w: %s", ErrRuleSourceTreeLookup, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return RuleSourceTree{}, fmt.Errorf("%w: path %q was not found for repository %s@%s", ErrRuleSourceTreeLookup, normalizedPath, repo.ID, mapping.Ref)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return RuleSourceTree{}, fmt.Errorf("%w: github api returned %s", ErrRuleSourceTreeLookup, resp.Status)
	}

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return RuleSourceTree{}, err
	}

	entries := []RuleSourceTreeEntry{}
	if len(raw) > 0 && raw[0] == '[' {
		var items []struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &items); err != nil {
			return RuleSourceTree{}, err
		}
		for _, item := range items {
			entries = append(entries, RuleSourceTreeEntry{
				Name: item.Name,
				Path: trimRepositoryRootPath(mapping.RootPath, item.Path),
				Type: item.Type,
			})
		}
	} else {
		var item struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return RuleSourceTree{}, err
		}
		entries = append(entries, RuleSourceTreeEntry{
			Name: item.Name,
			Path: trimRepositoryRootPath(mapping.RootPath, item.Path),
			Type: item.Type,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type == entries[j].Type {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Type == "dir"
	})

	return RuleSourceTree{
		RepositoryID: repo.ID,
		Core:         mapping.Core,
		Ref:          mapping.Ref,
		Path:         normalizedPath,
		Entries:      entries,
	}, nil
}

func (b *RuleSourceRepositoryBrowser) refreshGitHubSelectableFiles(repo RuleSourceRepository, mapping RuleSourceCoreMapping) (RuleSourceSelectableFiles, error) {
	payload, err := b.fetchGitHubRecursiveTree(repo, mapping)
	if err != nil {
		return RuleSourceSelectableFiles{}, err
	}

	candidatesByPath := map[string]selectableFileCandidate{}
	for _, item := range payload.Tree {
		if item.Type != "blob" {
			continue
		}
		relativePath := trimRepositoryRootPath(mapping.RootPath, item.Path)
		if relativePath == "" || strings.HasPrefix(relativePath, "..") || strings.Contains(relativePath, "/.") {
			continue
		}
		for _, candidate := range inferSelectableFileCandidates(mapping.Core, relativePath) {
			current, exists := candidatesByPath[candidate.file.Path]
			if !exists || candidate.priority > current.priority {
				candidatesByPath[candidate.file.Path] = candidate
			}
		}
	}

	files := make([]RuleSourceSelectableFile, 0, len(candidatesByPath))
	for _, candidate := range candidatesByPath {
		files = append(files, candidate.file)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return RuleSourceSelectableFiles{
		RepositoryID: repo.ID,
		Core:         mapping.Core,
		Ref:          mapping.Ref,
		RefreshedAt:  time.Now().UTC(),
		Files:        files,
	}, nil
}

func (b *RuleSourceRepositoryBrowser) refreshGitHubIndexFiles(repo RuleSourceRepository, mapping RuleSourceCoreMapping) ([]RuleSourceIndexFile, error) {
	payload, err := b.fetchGitHubRecursiveTree(repo, mapping)
	if err != nil {
		return nil, err
	}
	files := []RuleSourceIndexFile{}
	for _, item := range payload.Tree {
		if item.Type != "blob" {
			continue
		}
		relativePath := trimRepositoryRootPath(mapping.RootPath, item.Path)
		if relativePath == "" || strings.HasPrefix(relativePath, "..") || strings.Contains(relativePath, "/.") {
			continue
		}
		file, ok := inferIndexFile(repo, mapping.Core, relativePath)
		if !ok {
			continue
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

type githubTreePayload struct {
	Tree []struct {
		Path string `json:"path"`
		SHA  string `json:"sha"`
		Type string `json:"type"`
	} `json:"tree"`
}

func (b *RuleSourceRepositoryBrowser) fetchGitHubRecursiveTree(repo RuleSourceRepository, mapping RuleSourceCoreMapping) (githubTreePayload, error) {
	root, err := b.fetchGitHubTree(repo, mapping.Ref)
	if err != nil {
		return githubTreePayload{}, err
	}

	flattened := githubTreePayload{}
	type pendingTree struct {
		prefix string
		sha    string
	}
	pending := []pendingTree{}
	for _, item := range root.Tree {
		fullPath := normalizeRepositoryRootPath(item.Path)
		switch item.Type {
		case "blob":
			flattened.Tree = append(flattened.Tree, struct {
				Path string `json:"path"`
				SHA  string `json:"sha"`
				Type string `json:"type"`
			}{Path: fullPath, SHA: item.SHA, Type: item.Type})
		case "tree":
			pending = append(pending, pendingTree{prefix: fullPath, sha: item.SHA})
		}
	}

	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		tree, err := b.fetchGitHubTree(repo, current.sha)
		if err != nil {
			return githubTreePayload{}, err
		}
		for _, item := range tree.Tree {
			fullPath := normalizeRepositoryRootPath(path.Join(current.prefix, item.Path))
			switch item.Type {
			case "blob":
				flattened.Tree = append(flattened.Tree, struct {
					Path string `json:"path"`
					SHA  string `json:"sha"`
					Type string `json:"type"`
				}{Path: fullPath, SHA: item.SHA, Type: item.Type})
			case "tree":
				pending = append(pending, pendingTree{prefix: fullPath, sha: item.SHA})
			}
		}
	}

	return flattened, nil
}

func (b *RuleSourceRepositoryBrowser) fetchGitHubTree(repo RuleSourceRepository, refOrSHA string) (githubTreePayload, error) {
	endpoint := strings.TrimRight(b.githubAPIBase, "/") +
		"/repos/" + url.PathEscape(repo.Owner) +
		"/" + url.PathEscape(repo.Repository) +
		"/git/trees/" + url.PathEscape(refOrSHA)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return githubTreePayload{}, err
	}
	b.setGitHubRequestHeaders(req)

	resp, err := b.client.Do(req)
	if err != nil {
		return githubTreePayload{}, fmt.Errorf("%w: %s", ErrRuleSourceTreeLookup, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return githubTreePayload{}, fmt.Errorf("%w: tree for repository %s@%s was not found", ErrRuleSourceTreeLookup, repo.ID, refOrSHA)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return githubTreePayload{}, fmt.Errorf("%w: github api returned %s", ErrRuleSourceTreeLookup, resp.Status)
	}

	var payload githubTreePayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return githubTreePayload{}, err
	}
	return payload, nil
}

func (b *RuleSourceRepositoryBrowser) setGitHubRequestHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	if b.githubTokenProvider == nil {
		return
	}
	if token := strings.TrimSpace(b.githubTokenProvider()); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

type selectableFileCandidate struct {
	file     RuleSourceSelectableFile
	priority int
}

func inferSelectableFileCandidates(core Core, relativePath string) []selectableFileCandidate {
	name := path.Base(relativePath)
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, filepath.Ext(name))
	normalized := strings.ToLower(relativePath)

	switch core {
	case CoreSingBox:
		switch ext {
		case ".srs":
			return []selectableFileCandidate{{
				file: RuleSourceSelectableFile{
					Name:   base,
					Path:   relativePath,
					Kind:   KindSingBoxRuleSet,
					Format: "binary",
				},
				priority: 100,
			}}
		case ".json", ".yaml", ".yml":
			return []selectableFileCandidate{{
				file: RuleSourceSelectableFile{
					Name:   base,
					Path:   relativePath,
					Kind:   KindSingBoxRuleSet,
					Format: "source",
				},
				priority: 60,
			}}
		}
	case CoreMihomo:
		switch ext {
		case ".yaml", ".yml", ".txt", ".list", ".mrs":
			candidates := []selectableFileCandidate{{
				file: RuleSourceSelectableFile{
					Name:     base,
					Path:     relativePath,
					Kind:     KindMihomoRuleProvider,
					Format:   inferMihomoRuleProviderFormat(ext),
					Behavior: inferMihomoRuleProviderBehavior(relativePath),
				},
				priority: 10,
			}}
			if strings.Contains(normalized, "classical") {
				candidates = append(candidates, selectableFileCandidate{
					file: RuleSourceSelectableFile{
						Name:     base,
						Path:     relativePath,
						Kind:     KindMihomoRuleProvider,
						Format:   inferMihomoRuleProviderFormat(ext),
						Behavior: "classical",
					},
					priority: 90,
				})
			}
			if strings.Contains(normalized, "ipcidr") || strings.Contains(normalized, "cidr") || strings.Contains(normalized, "/ip/") {
				candidates = append(candidates, selectableFileCandidate{
					file: RuleSourceSelectableFile{
						Name:     base,
						Path:     relativePath,
						Kind:     KindMihomoRuleProvider,
						Format:   inferMihomoRuleProviderFormat(ext),
						Behavior: "ipcidr",
					},
					priority: 80,
				})
			}
			return candidates
		}
	}

	return nil
}

func inferIndexFile(repo RuleSourceRepository, core Core, relativePath string) (RuleSourceIndexFile, bool) {
	name := path.Base(relativePath)
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, filepath.Ext(name))
	logicalPath := strings.TrimSuffix(relativePath, filepath.Ext(relativePath))
	rawURL, err := BuildRepositoryRawURL(repo, core, relativePath, "")
	if err != nil {
		return RuleSourceIndexFile{}, false
	}
	switch core {
	case CoreSingBox:
		format := inferSingBoxRuleSetFormat(ext)
		if format == "" {
			return RuleSourceIndexFile{}, false
		}
		return RuleSourceIndexFile{
			Core:        core,
			Path:        relativePath,
			LogicalPath: logicalPath,
			Name:        base,
			Kind:        KindSingBoxRuleSet,
			Format:      format,
			RawURL:      rawURL,
		}, true
	case CoreMihomo:
		format := inferMihomoRuleProviderFormat(ext)
		if format == "" {
			return RuleSourceIndexFile{}, false
		}
		behavior := inferMihomoRuleProviderBehavior(relativePath)
		if ext == ".mrs" && behavior == "classical" {
			return RuleSourceIndexFile{}, false
		}
		return RuleSourceIndexFile{
			Core:        core,
			Path:        relativePath,
			LogicalPath: logicalPath,
			Name:        base,
			Kind:        KindMihomoRuleProvider,
			Format:      format,
			Behavior:    behavior,
			RawURL:      rawURL,
		}, true
	default:
		return RuleSourceIndexFile{}, false
	}
}

func inferSingBoxRuleSetFormat(ext string) string {
	switch strings.ToLower(ext) {
	case ".srs":
		return "binary"
	case ".json", ".yaml", ".yml":
		return "source"
	default:
		return ""
	}
}

func inferMihomoRuleProviderFormat(ext string) string {
	switch strings.ToLower(ext) {
	case ".yaml", ".yml":
		return "yaml"
	case ".txt", ".list":
		return "text"
	case ".mrs":
		return "mrs"
	default:
		return ""
	}
}

func inferMihomoRuleProviderBehavior(relativePath string) string {
	normalized := strings.ToLower(relativePath)
	if strings.Contains(normalized, "classical") {
		return "classical"
	}
	if strings.Contains(normalized, "geoip") || strings.Contains(normalized, "ipcidr") || strings.Contains(normalized, "cidr") || strings.Contains(normalized, "/ip/") {
		return "ipcidr"
	}
	return "domain"
}

func emptyRuleSourceIndex(repo RuleSourceRepository, indexPath string) RuleSourceIndex {
	refs := map[Core]string{}
	for _, mapping := range repo.CoreMappings {
		refs[mapping.Core] = mapping.Ref
	}
	return RuleSourceIndex{
		RepositoryID: repo.ID,
		Owner:        repo.Owner,
		Repository:   repo.Repository,
		Path:         normalizeRepositoryRootPath(indexPath),
		Refs:         refs,
		Directories:  []RuleSourceIndexDirectory{},
		Entries:      []RuleSourceIndexEntry{},
	}
}

func trimRepositoryRootPath(rootPath string, value string) string {
	rootPath = normalizeRepositoryRootPath(rootPath)
	value = normalizeRepositoryRootPath(value)
	if rootPath == "" {
		return value
	}
	return strings.TrimPrefix(strings.TrimPrefix(value, rootPath), "/")
}

func BuildRepositoryRawURL(repo RuleSourceRepository, core Core, filePath string, refOverride string) (string, error) {
	mapping, err := findRuleSourceCoreMapping(repo, core)
	if err != nil {
		return "", err
	}
	ref := strings.TrimSpace(refOverride)
	if ref == "" {
		ref = mapping.Ref
	}
	fullPath := normalizeRepositoryRootPath(path.Join(mapping.RootPath, filePath))
	switch repo.Provider {
	case RuleSourceRepositoryProviderGitHub:
		if fullPath == "" {
			return "", fmt.Errorf("%w: repository file path is required", ErrRuleSourceTreeLookup)
		}
		return fmt.Sprintf(
			"https://raw.githubusercontent.com/%s/%s/%s/%s",
			url.PathEscape(repo.Owner),
			url.PathEscape(repo.Repository),
			url.PathEscape(ref),
			strings.ReplaceAll(fullPath, " ", "%20"),
		), nil
	default:
		return "", fmt.Errorf("%w: unsupported provider %q", ErrRuleSourceTreeLookup, repo.Provider)
	}
}
