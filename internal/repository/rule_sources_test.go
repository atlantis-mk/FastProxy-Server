package repository

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestStoreListsBuiltInAndCustomRuleSourceRepositories(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	created, err := store.CreateRuleSourceRepository(RuleSourceRepository{
		Metadata: Metadata{
			Name: "Custom Rules",
		},
		Owner:      "example",
		Repository: "rules",
		CoreMappings: []RuleSourceCoreMapping{
			{Core: CoreSingBox, Ref: "main", RootPath: "sing"},
		},
	})
	if err != nil {
		t.Fatalf("CreateRuleSourceRepository() error = %v", err)
	}

	items, err := store.ListRuleSourceRepositories()
	if err != nil {
		t.Fatalf("ListRuleSourceRepositories() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if !items[0].BuiltIn {
		t.Fatalf("expected built-in repository first, got %+v", items[0])
	}
	if items[1].ID != created.ID {
		t.Fatalf("custom repository ID = %q, want %q", items[1].ID, created.ID)
	}
	if len(items[1].SupportedCores) != 1 || items[1].SupportedCores[0] != CoreSingBox {
		t.Fatalf("custom supported cores = %#v, want sing-box only", items[1].SupportedCores)
	}
}

func TestStoreRejectsInvalidRuleSourceMappings(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = store.CreateRuleSourceRepository(RuleSourceRepository{
		Metadata: Metadata{
			Name: "Broken Rules",
		},
		Owner:      "example",
		Repository: "rules",
		CoreMappings: []RuleSourceCoreMapping{
			{Core: CoreSingBox, Ref: "main"},
			{Core: CoreSingBox, Ref: "backup"},
		},
	})
	if !errors.Is(err, ErrInvalidRuleSourceRepository) {
		t.Fatalf("CreateRuleSourceRepository() error = %v, want ErrInvalidRuleSourceRepository", err)
	}
}

func TestStoreRejectsMihomoRuleProviderRepositoryForUnsupportedCore(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	repo, err := store.CreateRuleSourceRepository(RuleSourceRepository{
		Metadata: Metadata{
			Name: "Sing Rules",
		},
		Owner:      "example",
		Repository: "rules",
		CoreMappings: []RuleSourceCoreMapping{
			{Core: CoreSingBox, Ref: "main"},
		},
	})
	if err != nil {
		t.Fatalf("CreateRuleSourceRepository() error = %v", err)
	}

	_, err = store.CreateMihomoRuleProvider(MihomoRuleProviderResource{
		Metadata: Metadata{
			Name: "OpenAI",
		},
		Provider:     "openai",
		SourceMode:   RuleAssetSourceModeRepositoryFile,
		RepositoryID: repo.ID,
		Path:         "openai.yaml",
	})
	if !errors.Is(err, ErrUnsupportedRepositoryCore) {
		t.Fatalf("CreateMihomoRuleProvider() error = %v, want ErrUnsupportedRepositoryCore", err)
	}
}

func TestStoreRejectsInvalidMihomoRuleProvider(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = store.CreateMihomoRuleProvider(MihomoRuleProviderResource{
		Metadata: Metadata{
			Name: "Duplicate",
		},
		Provider:   "openai",
		SourceMode: RuleAssetSourceModeRemote,
	})
	if !errors.Is(err, ErrInvalidMihomoRuleProvider) {
		t.Fatalf("CreateMihomoRuleProvider() error = %v, want ErrInvalidMihomoRuleProvider", err)
	}
}

func TestStoreCreatesNativeRuleResources(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	singBoxItem, err := store.CreateSingBoxRuleSet(SingBoxRuleSetResource{
		Metadata: Metadata{
			Name: "OpenAI",
		},
		Tag:        "openai",
		SourceMode: RuleAssetSourceModeRemote,
		URL:        "https://example.com/openai.srs",
		Format:     "source",
	})
	if err != nil {
		t.Fatalf("CreateSingBoxRuleSet() error = %v", err)
	}
	if singBoxItem.Tag != "openai" {
		t.Fatalf("Tag = %q, want openai", singBoxItem.Tag)
	}

	mihomoItem, err := store.CreateMihomoRuleProvider(MihomoRuleProviderResource{
		Metadata: Metadata{
			Name: "OpenAI",
		},
		Provider:   "openai",
		SourceMode: RuleAssetSourceModeRemote,
		URL:        "https://example.com/openai.yaml",
		Behavior:   "domain",
		Format:     "yaml",
	})
	if err != nil {
		t.Fatalf("CreateMihomoRuleProvider() error = %v", err)
	}
	if mihomoItem.Provider != "openai" {
		t.Fatalf("Provider = %q, want openai", mihomoItem.Provider)
	}
}

func TestRuleSourceRepositoryBrowserBrowseGitHubTree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != "main" {
			t.Fatalf("ref = %q, want main", r.URL.Query().Get("ref"))
		}
		if r.URL.Path != "/repos/example/rules/contents/sing" {
			t.Fatalf("path = %q, want /repos/example/rules/contents/sing", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[
			{"name":"geo","path":"sing/geo","type":"dir"},
			{"name":"openai.srs","path":"sing/openai.srs","type":"file"}
		]`))
	}))
	defer server.Close()

	browser := NewRuleSourceRepositoryBrowser()
	browser.client = server.Client()
	browser.githubAPIBase = server.URL

	tree, err := browser.Browse(RuleSourceRepository{
		Metadata:   Metadata{ID: "custom"},
		Provider:   RuleSourceRepositoryProviderGitHub,
		Owner:      "example",
		Repository: "rules",
		CoreMappings: []RuleSourceCoreMapping{
			{Core: CoreSingBox, Ref: "main", RootPath: "sing"},
		},
	}, CoreSingBox, "")
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}
	if len(tree.Entries) != 2 {
		t.Fatalf("len(tree.Entries) = %d, want 2", len(tree.Entries))
	}
	if tree.Entries[0].Path != "geo" || tree.Entries[1].Path != "openai.srs" {
		t.Fatalf("tree entries = %#v, want paths relative to root", tree.Entries)
	}
}

func TestRuleSourceRepositoryBrowserRejectsUnsupportedCoreAndMissingPath(t *testing.T) {
	browser := NewRuleSourceRepositoryBrowser()
	repo := RuleSourceRepository{
		Metadata:   Metadata{ID: "custom"},
		Provider:   RuleSourceRepositoryProviderGitHub,
		Owner:      "example",
		Repository: "rules",
		CoreMappings: []RuleSourceCoreMapping{
			{Core: CoreSingBox, Ref: "main"},
		},
	}

	if _, err := browser.Browse(repo, CoreMihomo, ""); !errors.Is(err, ErrUnsupportedRepositoryCore) {
		t.Fatalf("Browse(unsupported core) error = %v, want ErrUnsupportedRepositoryCore", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	browser.client = server.Client()
	browser.githubAPIBase = server.URL
	if _, err := browser.Browse(repo, CoreSingBox, "missing"); !errors.Is(err, ErrRuleSourceTreeLookup) {
		t.Fatalf("Browse(missing path) error = %v, want ErrRuleSourceTreeLookup", err)
	}
}

func TestRuleSourceRepositoryBrowserRefreshSelectableFilesDeduplicatesBehavior(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/example/rules/git/trees/meta":
			_, _ = w.Write([]byte(`{
				"tree": [
					{"path":"rules","sha":"rules-sha","type":"tree"}
				]
			}`))
		case "/repos/example/rules/git/trees/rules-sha":
			_, _ = w.Write([]byte(`{
				"tree": [
					{"path":"domain/openai.yaml","type":"blob"},
					{"path":"classical/telegram.yaml","type":"blob"},
					{"path":"ipcidr/private.yaml","type":"blob"},
					{"path":"readme.md","type":"blob"},
					{"path":"subdir","sha":"subdir-sha","type":"tree"}
				]
			}`))
		case "/repos/example/rules/git/trees/subdir-sha":
			_, _ = w.Write([]byte(`{"tree":[]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	browser := NewRuleSourceRepositoryBrowser()
	browser.client = server.Client()
	browser.githubAPIBase = server.URL

	files, err := browser.RefreshSelectableFiles(RuleSourceRepository{
		Metadata:   Metadata{ID: "custom"},
		Provider:   RuleSourceRepositoryProviderGitHub,
		Owner:      "example",
		Repository: "rules",
		CoreMappings: []RuleSourceCoreMapping{
			{Core: CoreMihomo, Ref: "meta", RootPath: "rules"},
		},
	}, CoreMihomo)
	if err != nil {
		t.Fatalf("RefreshSelectableFiles() error = %v", err)
	}
	if len(files.Files) != 3 {
		t.Fatalf("len(files.Files) = %d, want 3", len(files.Files))
	}
	if files.Files[0].Path != "classical/telegram.yaml" || files.Files[0].Behavior != "classical" {
		t.Fatalf("first file = %#v, want classical telegram", files.Files[0])
	}
	if files.Files[2].Path != "ipcidr/private.yaml" || files.Files[2].Behavior != "ipcidr" {
		t.Fatalf("last file = %#v, want ipcidr private", files.Files[2])
	}
}

func TestRuleSourceRepositoryBrowserRefreshIndexMergesSingAndMetaBranches(t *testing.T) {
	requestedRefs := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedRefs = append(requestedRefs, pathBase(r.URL.Path))
		switch r.URL.Path {
		case "/repos/MetaCubeX/meta-rules-dat/git/trees/sing":
			_, _ = w.Write([]byte(`{
				"tree": [
					{"path":"asn","sha":"sing-asn-sha","type":"tree"},
					{"path":"geo-lite","sha":"sing-geo-lite-sha","type":"tree"},
					{"path":"geo","sha":"sing-geo-sha","type":"tree"},
					{"path":"README.md","type":"blob"}
				]
			}`))
		case "/repos/MetaCubeX/meta-rules-dat/git/trees/sing-asn-sha":
			_, _ = w.Write([]byte(`{"tree":[{"path":"AS1.srs","type":"blob"}]}`))
		case "/repos/MetaCubeX/meta-rules-dat/git/trees/sing-geo-lite-sha":
			_, _ = w.Write([]byte(`{"tree":[{"path":"geosite/cn.srs","type":"blob"}]}`))
		case "/repos/MetaCubeX/meta-rules-dat/git/trees/sing-geo-sha":
			_, _ = w.Write([]byte(`{
				"tree": [
					{"path":"geosite/cn.srs","type":"blob"},
					{"path":"geoip/cn.srs","type":"blob"}
				]
			}`))
		case "/repos/MetaCubeX/meta-rules-dat/git/trees/meta":
			_, _ = w.Write([]byte(`{
				"tree": [
					{"path":"asn","sha":"meta-asn-sha","type":"tree"},
					{"path":"geo-lite","sha":"meta-geo-lite-sha","type":"tree"},
					{"path":"geo","sha":"meta-geo-sha","type":"tree"}
				]
			}`))
		case "/repos/MetaCubeX/meta-rules-dat/git/trees/meta-asn-sha":
			_, _ = w.Write([]byte(`{"tree":[{"path":"AS1.mrs","type":"blob"}]}`))
		case "/repos/MetaCubeX/meta-rules-dat/git/trees/meta-geo-lite-sha":
			_, _ = w.Write([]byte(`{"tree":[{"path":"geosite/cn.mrs","type":"blob"}]}`))
		case "/repos/MetaCubeX/meta-rules-dat/git/trees/meta-geo-sha":
			_, _ = w.Write([]byte(`{
				"tree": [
					{"path":"geosite/cn.mrs","type":"blob"},
					{"path":"geoip/cn.mrs","type":"blob"},
					{"path":"geosite/private.yaml","type":"blob"}
				]
			}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	browser := NewRuleSourceRepositoryBrowser()
	browser.client = server.Client()
	browser.githubAPIBase = server.URL

	index, err := browser.RefreshIndex(BuiltInRuleSourceRepositories()[0])
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	if !slices.Contains(requestedRefs, "sing-geo-sha") || !slices.Contains(requestedRefs, "meta-geo-sha") || !slices.Contains(requestedRefs, "sing-geo-lite-sha") {
		t.Fatalf("requested refs = %#v, want geo and geo-lite directories to be fetched", requestedRefs)
	}
	if len(index.Entries) != 5 {
		t.Fatalf("len(index.Entries) = %d, want 5", len(index.Entries))
	}
	byPath := map[string]RuleSourceIndexEntry{}
	for _, entry := range index.Entries {
		byPath[entry.LogicalPath] = entry
	}
	geositeCN := byPath["geo/geosite/cn"]
	if geositeCN.Files[CoreSingBox].Format != "binary" || geositeCN.Files[CoreMihomo].Format != "mrs" {
		t.Fatalf("geo/geosite/cn files = %#v, want sing binary and mihomo mrs", geositeCN.Files)
	}
	if byPath["geo/geoip/cn"].Files[CoreMihomo].Behavior != "ipcidr" {
		t.Fatalf("geo/geoip/cn behavior = %#v, want ipcidr", byPath["geo/geoip/cn"].Files[CoreMihomo].Behavior)
	}
	if _, ok := byPath["geo/geosite/private"].Files[CoreSingBox]; ok {
		t.Fatalf("geo/geosite/private should only have mihomo file: %#v", byPath["geo/geosite/private"].Files)
	}
	if byPath["geo-lite/geosite/cn"].Files[CoreMihomo].Format != "mrs" {
		t.Fatalf("geo-lite/geosite/cn files = %#v, want mihomo mrs", byPath["geo-lite/geosite/cn"].Files)
	}
}

func TestStorePersistsRuleSourceIndexAndReturnsEmptyBuiltInIndex(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	empty, err := store.GetRuleSourceIndex("metacubex-meta-rules-dat")
	if err != nil {
		t.Fatalf("GetRuleSourceIndex(empty built-in) error = %v", err)
	}
	if empty.RepositoryID != "metacubex-meta-rules-dat" || len(empty.Entries) != 0 {
		t.Fatalf("empty index = %#v, want built-in metadata with no entries", empty)
	}

	staleDir := filepath.Join(root, "repository", "rule-source-indexes", "metacubex-meta-rules-dat")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(staleDir) error = %v", err)
	}
	staleFiles := []string{
		filepath.Join(staleDir, "index.json"),
		filepath.Join(root, "repository", "rule-source-indexes", "metacubex-meta-rules-dat.json"),
		filepath.Join(root, "repository", "rule-source-indexes", "metacubex-meta-rules-dat.jsonl"),
		filepath.Join(root, "repository", "rule-source-indexes", "metacubex-meta-rules-dat.search.jsonl"),
		filepath.Join(root, "repository", "rule-source-indexes", "metacubex-meta-rules-dat-search.jsonl"),
	}
	for _, staleFile := range staleFiles {
		if err := os.WriteFile(staleFile, []byte("{}"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", staleFile, err)
		}
	}

	written, err := store.UpsertRuleSourceIndex(RuleSourceIndex{
		RepositoryID: "metacubex-meta-rules-dat",
		Owner:        "MetaCubeX",
		Repository:   "meta-rules-dat",
		Refs:         map[Core]string{CoreSingBox: "sing", CoreMihomo: "meta"},
		Entries: []RuleSourceIndexEntry{{
			LogicalPath: "geo/geosite/cn",
			Name:        "cn",
			Files: map[Core]RuleSourceIndexFile{
				CoreSingBox: {
					Core:        CoreSingBox,
					Path:        "geo/geosite/cn.srs",
					LogicalPath: "geo/geosite/cn",
					Name:        "cn",
					Kind:        KindSingBoxRuleSet,
					Format:      "binary",
					RawURL:      "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geosite/cn.srs",
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("UpsertRuleSourceIndex() error = %v", err)
	}
	if written.RefreshedAt.IsZero() {
		t.Fatalf("written.RefreshedAt should be set")
	}
	if _, err := os.Stat(filepath.Join(root, "repository", "rule-source-indexes", "rule-source-indexes.sqlite")); err != nil {
		t.Fatalf("SQLite index should exist: %v", err)
	}
	for _, staleFile := range staleFiles {
		if _, err := os.Stat(staleFile); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale cache %s should be removed, stat error = %v", staleFile, err)
		}
	}

	read, err := store.GetRuleSourceIndex("metacubex-meta-rules-dat")
	if err != nil {
		t.Fatalf("GetRuleSourceIndex(written) error = %v", err)
	}
	if len(read.Directories) != 1 || read.Directories[0].Path != "geo" || len(read.Entries) != 0 {
		t.Fatalf("root index = %#v, want geo directory and no direct entries", read)
	}

	geosite, err := store.GetRuleSourceIndex("metacubex-meta-rules-dat", "geo/geosite")
	if err != nil {
		t.Fatalf("GetRuleSourceIndex(geo/geosite) error = %v", err)
	}
	if len(geosite.Entries) != 1 || geosite.Entries[0].LogicalPath != "geo/geosite/cn" {
		t.Fatalf("geo/geosite index = %#v, want persisted entry", geosite)
	}

	entry, err := store.FindRuleSourceIndexEntry("metacubex-meta-rules-dat", "geo/geosite/cn")
	if err != nil {
		t.Fatalf("FindRuleSourceIndexEntry() error = %v", err)
	}
	if entry.Files[CoreSingBox].Path != "geo/geosite/cn.srs" {
		t.Fatalf("entry = %#v, want sing-box file", entry)
	}

	search, err := store.SearchRuleSourceIndex("metacubex-meta-rules-dat", "geosite/cn", 10)
	if err != nil {
		t.Fatalf("SearchRuleSourceIndex() error = %v", err)
	}
	if len(search.Entries) != 1 || search.Entries[0].LogicalPath != "geo/geosite/cn" {
		t.Fatalf("search = %#v, want geo/geosite/cn", search)
	}
}

func TestStoreReinitializesCorruptSQLiteAndClearsRepositoryResources(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	created, err := store.CreateRuleSourceRepository(RuleSourceRepository{
		Metadata: Metadata{
			ID:   "custom-rules",
			Name: "Custom Rules",
		},
		Provider:   RuleSourceRepositoryProviderGitHub,
		Owner:      "example",
		Repository: "rules",
		CoreMappings: []RuleSourceCoreMapping{{
			Core: CoreSingBox,
			Ref:  "main",
		}},
	})
	if err != nil {
		t.Fatalf("CreateRuleSourceRepository() error = %v", err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	dbPath := filepath.Join(root, "repository", "rule-source-indexes", "rule-source-indexes.sqlite")
	if err := os.WriteFile(dbPath, []byte("not sqlite"), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt sqlite) error = %v", err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore(corrupt sqlite) error = %v", err)
	}
	if _, err := reopened.GetRuleSourceRepository(created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRuleSourceRepository() error = %v, want ErrNotFound", err)
	}
	var eventCount int
	if err := reopened.db.QueryRow(`
		SELECT COUNT(*)
		FROM local_data_diagnostic_events
		WHERE event_type = 'sqlite_database_reinitialized'
	`).Scan(&eventCount); err != nil {
		t.Fatalf("query diagnostic events error = %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("diagnostic event count = %d, want 1", eventCount)
	}
}

func TestStorePersistsCustomSelectableFilesInSQLite(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	repo, err := store.CreateRuleSourceRepository(RuleSourceRepository{
		Metadata: Metadata{
			ID:   "custom-rules",
			Name: "Custom Rules",
		},
		Provider:   RuleSourceRepositoryProviderGitHub,
		Owner:      "example",
		Repository: "rules",
		CoreMappings: []RuleSourceCoreMapping{{
			Core: CoreMihomo,
			Ref:  "main",
		}},
	})
	if err != nil {
		t.Fatalf("CreateRuleSourceRepository() error = %v", err)
	}

	files, err := store.UpsertRuleSourceSelectableFiles(RuleSourceSelectableFiles{
		RepositoryID: repo.ID,
		Core:         CoreMihomo,
		Ref:          "main",
		Files: []RuleSourceSelectableFile{{
			Name:     "telegram.yaml",
			Path:     "classical/telegram.yaml",
			Kind:     KindMihomoRuleProvider,
			Behavior: "classical",
		}, {
			Name:     "youtube.yaml",
			Path:     "domain/youtube.yaml",
			Kind:     KindMihomoRuleProvider,
			Behavior: "domain",
		}},
	})
	if err != nil {
		t.Fatalf("UpsertRuleSourceSelectableFiles() error = %v", err)
	}
	if len(files.Files) != 2 || files.Files[0].Path != "classical/telegram.yaml" {
		t.Fatalf("files = %#v, want persisted selectable file", files)
	}

	index, err := store.GetRuleSourceIndex(repo.ID, "classical")
	if err != nil {
		t.Fatalf("GetRuleSourceIndex(classical) error = %v", err)
	}
	if len(index.Entries) != 1 || index.Entries[0].Files[CoreMihomo].Behavior != "classical" {
		t.Fatalf("index = %#v, want selectable file in SQLite index", index)
	}
	search, err := store.SearchRuleSourceIndex(repo.ID, "telegram", 10)
	if err != nil {
		t.Fatalf("SearchRuleSourceIndex() error = %v", err)
	}
	if len(search.Entries) != 1 || search.Entries[0].LogicalPath != "classical/telegram.yaml" {
		t.Fatalf("search = %#v, want telegram result", search)
	}
	paged, err := store.SearchRuleSourceIndex(repo.ID, "", 1, RuleSourceIndexSearchFilters{
		Core:   CoreMihomo,
		Kind:   KindMihomoRuleProvider,
		Offset: 1,
	})
	if err != nil {
		t.Fatalf("SearchRuleSourceIndex(paged) error = %v", err)
	}
	if len(paged.Entries) != 1 || paged.Offset != 1 || paged.NextOffset != 2 || paged.HasMore {
		t.Fatalf("paged = %#v, want second one-item page", paged)
	}
	filtered, err := store.SearchRuleSourceIndex(repo.ID, "", 10, RuleSourceIndexSearchFilters{
		Core:       CoreMihomo,
		Behavior:   "classical",
		Kind:       KindMihomoRuleProvider,
		PathPrefix: "classical",
	})
	if err != nil {
		t.Fatalf("SearchRuleSourceIndex(filtered) error = %v", err)
	}
	if len(filtered.Entries) != 1 || filtered.Entries[0].LogicalPath != "classical/telegram.yaml" {
		t.Fatalf("filtered = %#v, want telegram result", filtered)
	}
	mismatched, err := store.SearchRuleSourceIndex(repo.ID, "", 10, RuleSourceIndexSearchFilters{
		Core: CoreSingBox,
	})
	if err != nil {
		t.Fatalf("SearchRuleSourceIndex(mismatched) error = %v", err)
	}
	if len(mismatched.Entries) != 0 {
		t.Fatalf("mismatched = %#v, want no entries", mismatched)
	}
}

func pathBase(value string) string {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	return parts[len(parts)-1]
}
