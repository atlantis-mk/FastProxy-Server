package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("resource not found")
var ErrDuplicateNodeName = errors.New("duplicate node name")

const configResourceKind = "_config"
const globalConfigResourceID = "global"

var defaultDNSNameservers = []string{
	"223.5.5.5",
	"119.29.29.29",
	"tls://223.5.5.5:853",
	"tls://223.6.6.6:853",
	"tls://120.53.53.53",
	"tls://1.12.12.12",
}

var defaultProxyDNSNameservers = []string{
	"https://8.8.8.8/dns-query",
	"https://1.1.1.1/dns-query",
	"https://9.9.9.9/dns-query",
}

var defaultDNSFakeIPFilter = []string{
	"*.lan",
	"*.localdomain",
	"*.example",
	"*.invalid",
	"*.localhost",
	"*.test",
	"*.local",
	"*.home.arpa",
	"*.direct",
	"cable.auth.com",
	"network-test.debian.org",
	"detectportal.firefox.com",
	"resolver1.opendns.com",
	"global.turn.twilio.com",
	"global.stun.twilio.com",
	"app.yinxiang.com",
	"injections.adguard.org",
	"localhost.*.weixin.qq.com",
	"*.blzstatic.cn",
	"*.cmpassport.com",
	"id6.me",
	"open.e.189.cn",
	"opencloud.wostore.cn",
	"id.mail.wo.cn",
	"mdn.open.wo.cn",
	"hmrz.wo.cn",
	"nishub1.10010.com",
	"enrichgw.10010.com",
	"*.wosms.cn",
	"*.jegotrip.com.cn",
	"*.icitymobile.mobi",
	"*.pingan.com.cn",
	"*.cmbchina.com",
	"*.10099.com.cn",
	"*.microdone.cn",
	"PDC._msDCS.*.*",
	"DC._msDCS.*.*",
	"GC._msDCS.*.*",
	"time.*.com",
	"time.*.gov",
	"time.*.edu.cn",
	"time.*.apple.com",
	"time-ios.apple.com",
	"time1.*.com",
	"time2.*.com",
	"time3.*.com",
	"time4.*.com",
	"time5.*.com",
	"time6.*.com",
	"time7.*.com",
	"ntp.*.com",
	"ntp1.*.com",
	"ntp2.*.com",
	"ntp3.*.com",
	"ntp4.*.com",
	"ntp5.*.com",
	"ntp6.*.com",
	"ntp7.*.com",
	"*.time.edu.cn",
	"*.ntp.org.cn",
	"+.pool.ntp.org",
	"time1.cloud.tencent.com",
	"music.163.com",
	"*.music.163.com",
	"*.126.net",
	"musicapi.taihe.com",
	"music.taihe.com",
	"songsearch.kugou.com",
	"trackercdn.kugou.com",
	"*.kuwo.cn",
	"api-jooxtt.sanook.com",
	"api.joox.com",
	"joox.com",
	"y.qq.com",
	"*.y.qq.com",
	"streamoc.music.tc.qq.com",
	"mobileoc.music.tc.qq.com",
	"isure.stream.qqmusic.qq.com",
	"dl.stream.qqmusic.qq.com",
	"aqqmusic.tc.qq.com",
	"amobile.music.tc.qq.com",
	"*.xiami.com",
	"*.music.migu.cn",
	"music.migu.cn",
	"+.msftconnecttest.com",
	"+.msftncsi.com",
	"localhost.ptlogin2.qq.com",
	"localhost.sec.qq.com",
	"+.qq.com",
	"+.tencent.com",
	"+.srv.nintendo.net",
	"*.n.n.srv.nintendo.net",
	"+.cdn.nintendo.net",
	"+.stun.playstation.net",
	"xbox.*.*.microsoft.com",
	"*.*.xboxlive.com",
	"xbox.*.microsoft.com",
	"xnotify.xboxlive.com",
	"+.battle.net",
	"+.battlenet.com.cn",
	"+.wotgame.cn",
	"+.wggames.cn",
	"+.wowsgame.cn",
	"+.wargaming.net",
	"proxy.golang.org",
	"stun.*.*",
	"stun.*.*.*",
	"+.stun.*.*",
	"+.stun.*.*.*",
	"+.stun.*.*.*.*",
	"+.stun.*.*.*.*.*",
	"heartbeat.belkin.com",
	"*.linksys.com",
	"*.linksyssmartwifi.com",
	"*.router.asus.com",
	"mesu.apple.com",
	"swscan.apple.com",
	"swquery.apple.com",
	"swdownload.apple.com",
	"swcdn.apple.com",
	"swdist.apple.com",
	"lens.l.google.com",
	"stun.l.google.com",
	"na.b.g-tun.com",
	"+.nflxvideo.net",
	"*.square-enix.com",
	"*.finalfantasyxiv.com",
	"*.ffxiv.com",
	"*.ff14.sdo.com",
	"ff.dorado.sdo.com",
	"*.mcdn.bilivideo.cn",
	"+.media.dssott.com",
	"shark007.net",
	"Mijia Cloud",
	"+.cmbchina.com",
	"+.cmbimg.com",
	"local.adguard.org",
	"+.sandai.net",
	"+.n0808.com",
	"+.uu.163.com",
	"ps.res.netease.com",
	"+.pub.3gppnetwork.org",
	"*.msftconnecttest.com",
	"*.msftncsi.com",
	"*.*.*.srv.nintendo.net",
	"*.*.stun.playstation.net",
	"*.ipv6.microsoft.com",
	"teredo.*.*.*",
	"teredo.*.*",
	"speedtest.cros.wr.pvp.net",
	"+.jjvip8.com",
	"www.douyu.com",
	"activityapi.huya.com",
	"activityapi.huya.com.w.cdngslb.com",
	"www.bilibili.com",
	"api.bilibili.com",
	"a.w.bilicdn1.com",
}

type Store struct {
	mu      sync.Mutex
	root    string
	dataDir string
	db      *sql.DB
}

func NewStore(root string) (*Store, error) {
	base := filepath.Join(root, "repository")
	store := &Store{
		root:    base,
		dataDir: filepath.Join(base, "rule-source-indexes"),
	}
	db, err := openLocalSQLite(
		store.dataDir,
		repositoryResourceSchema(),
		ruleSourceIndexSchema(),
		nodeCacheSchema(),
		healthHistorySchema(),
		operationEventSchema(),
		profileSchema(),
	)
	if err != nil {
		return nil, err
	}
	store.db = db
	return store, nil
}

func repositoryResourceSchema() sqliteSchema {
	return sqliteSchema{
		Name: "repository_resources",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS repository_resources (
				resource_kind TEXT NOT NULL,
				resource_id TEXT NOT NULL,
				name TEXT NOT NULL,
				resource_json TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				PRIMARY KEY (resource_kind, resource_id)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_repository_resources_kind_updated
				ON repository_resources(resource_kind, updated_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_repository_resources_kind_name
				ON repository_resources(resource_kind, name)`,
		},
	}
}

func ruleSourceIndexSchema() sqliteSchema {
	return sqliteSchema{
		Name: "rule_source_indexes",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS rule_source_index_repositories (
			repository_id TEXT PRIMARY KEY,
			owner TEXT NOT NULL,
			repository TEXT NOT NULL,
			refs_json TEXT NOT NULL,
			refreshed_at TEXT NOT NULL
		)`,
			`CREATE TABLE IF NOT EXISTS rule_source_index_directories (
			repository_id TEXT NOT NULL,
			path TEXT NOT NULL,
			name TEXT NOT NULL,
			parent_path TEXT NOT NULL,
			PRIMARY KEY (repository_id, path),
			FOREIGN KEY (repository_id) REFERENCES rule_source_index_repositories(repository_id) ON DELETE CASCADE
		)`,
			`CREATE INDEX IF NOT EXISTS idx_rule_source_index_directories_parent
			ON rule_source_index_directories(repository_id, parent_path, path)`,
			`CREATE TABLE IF NOT EXISTS rule_source_index_entries (
			repository_id TEXT NOT NULL,
			logical_path TEXT NOT NULL,
			name TEXT NOT NULL,
			parent_path TEXT NOT NULL,
			files_json TEXT NOT NULL,
			search_text TEXT NOT NULL,
			PRIMARY KEY (repository_id, logical_path),
			FOREIGN KEY (repository_id) REFERENCES rule_source_index_repositories(repository_id) ON DELETE CASCADE
		)`,
			`CREATE INDEX IF NOT EXISTS idx_rule_source_index_entries_parent
			ON rule_source_index_entries(repository_id, parent_path, logical_path)`,
			`CREATE INDEX IF NOT EXISTS idx_rule_source_index_entries_search
			ON rule_source_index_entries(repository_id, search_text)`,
		},
	}
}

func profileSchema() sqliteSchema {
	return sqliteSchema{
		Name: "profiles",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS profiles (
				profile_id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				selected_core TEXT NOT NULL,
				profile_json TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_profiles_updated_at
				ON profiles(updated_at DESC)`,
		},
	}
}

func (s *Store) Bootstrap() (Bootstrap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	profiles, err := s.listProfiles()
	if err != nil {
		return Bootstrap{}, err
	}
	subscriptions, err := s.listSubscriptions()
	if err != nil {
		return Bootstrap{}, err
	}
	nodeSets, err := s.listNodeSets()
	if err != nil {
		return Bootstrap{}, err
	}
	routingRuleSets, err := s.listRuleSets()
	if err != nil {
		return Bootstrap{}, err
	}
	repositories, err := s.listRuleSourceRepositories()
	if err != nil {
		return Bootstrap{}, err
	}
	singBoxRuleSets, err := s.listSingBoxRuleSets()
	if err != nil {
		return Bootstrap{}, err
	}
	mihomoRuleProviders, err := s.listMihomoRuleProviders()
	if err != nil {
		return Bootstrap{}, err
	}
	groupSets, err := s.listGroupSets()
	if err != nil {
		return Bootstrap{}, err
	}
	config, err := s.globalConfig()
	if err != nil {
		return Bootstrap{}, err
	}
	return Bootstrap{
		Profiles:               profiles,
		Subscriptions:          subscriptions,
		NodeSets:               nodeSets,
		RoutingRuleSets:        routingRuleSets,
		RuleSourceRepositories: repositories,
		SingBoxRuleSets:        singBoxRuleSets,
		MihomoRuleProviders:    mihomoRuleProviders,
		GroupSets:              groupSets,
		Config:                 config,
	}, nil
}

func (s *Store) ListProfiles() ([]ProfileResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listProfiles()
}

func (s *Store) GetProfile(id string) (ProfileResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readProfile(id)
}

func (s *Store) CreateProfile(input ProfileResource) (ProfileResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item := normalizeProfile(input, ProfileResource{})
	if err := s.writeProfile(item); err != nil {
		return ProfileResource{}, err
	}
	return item, nil
}

func (s *Store) UpdateProfile(id string, input ProfileResource) (ProfileResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.readProfile(id)
	if err != nil {
		return ProfileResource{}, err
	}
	item := normalizeProfile(input, current)
	item.ID = current.ID
	item.CreatedAt = current.CreatedAt
	item.Kind = KindProfile
	if err := s.writeProfile(item); err != nil {
		return ProfileResource{}, err
	}
	return item, nil
}

func (s *Store) GlobalInbounds() ([]ManagedInbound, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	config, err := s.globalConfig()
	if err != nil {
		return nil, err
	}
	return config.Inbounds, nil
}

func (s *Store) GlobalConfig() (GlobalConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.globalConfig()
}

func (s *Store) UpdateGlobalConfig(input GlobalConfig) (GlobalConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	config := normalizeGlobalConfig(input)
	config.UpdatedAt = time.Now().UTC()
	if err := s.writeGlobalConfig(config); err != nil {
		return GlobalConfig{}, err
	}
	return config, nil
}

func (s *Store) UpdateGlobalInbounds(input []ManagedInbound) (GlobalConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	config, err := s.globalConfig()
	if err != nil {
		return GlobalConfig{}, err
	}
	config.Inbounds = normalizeManagedInbounds(input)
	config.UpdatedAt = time.Now().UTC()
	if err := s.writeGlobalConfig(config); err != nil {
		return GlobalConfig{}, err
	}
	return config, nil
}

func (s *Store) DeleteProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.readProfile(id); err != nil {
		return convertNotFound(err)
	}
	if _, err := s.db.Exec(`DELETE FROM profiles WHERE profile_id = ?`, id); err != nil {
		return err
	}
	return nil
}

func (s *Store) ListSubscriptions() ([]SubscriptionResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listSubscriptions()
}

func (s *Store) GetSubscription(id string) (SubscriptionResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.readSubscription(id)
	return item, convertNotFound(err)
}

func (s *Store) CreateSubscription(input SubscriptionResource) (SubscriptionResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item := normalizeSubscription(input, SubscriptionResource{})
	if err := s.writeSubscription(item, ""); err != nil {
		return SubscriptionResource{}, err
	}
	return item, nil
}

func (s *Store) UpdateSubscription(id string, input SubscriptionResource) (SubscriptionResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.readSubscription(id)
	if err != nil {
		return SubscriptionResource{}, convertNotFound(err)
	}
	item := normalizeSubscription(input, current)
	item.CreatedAt = current.CreatedAt
	if err := s.writeSubscription(item, current.ID); err != nil {
		return SubscriptionResource{}, err
	}
	if current.ID != item.ID {
		if err := s.renameManagedNodeSetLocked(current.ID, item.ID, item.Name); err != nil && !errors.Is(err, ErrNotFound) {
			return SubscriptionResource{}, err
		}
		if err := s.renameSubscriptionReferences(current.ID, item.ID); err != nil {
			return SubscriptionResource{}, err
		}
	}
	return item, nil
}

func (s *Store) DeleteSubscription(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, err := s.readSubscription(id)
	if err != nil {
		return convertNotFound(err)
	}
	if err := s.deleteResource(KindSubscription, item.ID); err != nil {
		return err
	}
	if err := s.deleteManagedNodeSetLocked(SubscriptionNodeSetName(item.Name)); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	_ = s.deleteResource(KindRoutingRuleSet, SubscriptionRuleSetName(item.Name))
	_ = s.deleteResource(KindGroupSet, SubscriptionGroupSetName(item.Name))

	profiles, err := s.listProfiles()
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if !slices.Contains(profile.SubscriptionIDs, id) {
			continue
		}
		profile.SubscriptionIDs = removeString(profile.SubscriptionIDs, id)
		profile.UpdatedAt = time.Now().UTC()
		if err := s.writeProfile(profile); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListNodeSets() ([]NodeSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listNodeSets()
}

func (s *Store) ListNodeSetFiles() ([]NodeSetFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listNodeSetFiles()
}

func (s *Store) GetNodeSet(id string) (NodeSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.readNodeSet(id)
	return item, convertNotFound(err)
}

func (s *Store) CreateNodeSet(input NodeSetResource) (NodeSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := normalizeNodeSet(input, NodeSetResource{})
	if err := s.writeNodeSet(item, ""); err != nil {
		return NodeSetResource{}, err
	}
	return item, nil
}

func (s *Store) UpdateNodeSet(id string, input NodeSetResource) (NodeSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.readNodeSet(id)
	if err != nil {
		return NodeSetResource{}, convertNotFound(err)
	}
	item := normalizeNodeSet(input, current)
	item.CreatedAt = current.CreatedAt
	if err := s.writeNodeSet(item, current.ID); err != nil {
		return NodeSetResource{}, err
	}
	return item, nil
}

func (s *Store) DeleteNodeSet(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.deleteManagedNodeSetLocked(id); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.deleteResource(KindNodeSet, id)
}

func (s *Store) UpsertManagedNodeSet(input NodeSetResource) (NodeSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.readManagedNodeSetLocked(input.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return NodeSetResource{}, err
	}
	item := normalizeNodeSet(input, current)
	if !current.CreatedAt.IsZero() {
		item.CreatedAt = current.CreatedAt
	}
	if err := s.writeManagedNodeSetLocked(item); err != nil {
		return NodeSetResource{}, err
	}
	return item, nil
}

func (s *Store) ListRuleSets() ([]RuleSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listRuleSets()
}

func (s *Store) GetRuleSet(id string) (RuleSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.readRuleSet(id)
	return item, convertNotFound(err)
}

func (s *Store) CreateRuleSet(input RuleSetResource) (RuleSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := normalizeRuleSet(input, RuleSetResource{})
	if err := s.writeRuleSet(item, ""); err != nil {
		return RuleSetResource{}, err
	}
	return item, nil
}

func (s *Store) UpdateRuleSet(id string, input RuleSetResource) (RuleSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.readRuleSet(id)
	if err != nil {
		return RuleSetResource{}, convertNotFound(err)
	}
	item := normalizeRuleSet(input, current)
	item.CreatedAt = current.CreatedAt
	if err := s.writeRuleSet(item, current.ID); err != nil {
		return RuleSetResource{}, err
	}
	return item, nil
}

func (s *Store) DeleteRuleSet(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteResource(KindRoutingRuleSet, id)
}

func (s *Store) ListRuleSourceRepositories() ([]RuleSourceRepository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listRuleSourceRepositories()
}

func (s *Store) QueryRuleSourceRepositories(offset int, limit int, query string) (RuleSourceRepositoryPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.listRuleSourceRepositories()
	if err != nil {
		return RuleSourceRepositoryPage{}, err
	}
	items = filterRuleSourceRepositories(items, query)
	pageItems, nextOffset, hasMore := paginateSlice(items, offset, limit)
	return RuleSourceRepositoryPage{
		Items:      pageItems,
		Offset:     normalizePageOffset(offset, len(items)),
		Limit:      normalizePageLimit(limit, len(pageItems), len(items)),
		Total:      len(items),
		NextOffset: nextOffset,
		HasMore:    hasMore,
	}, nil
}

func (s *Store) GetRuleSourceRepository(id string) (RuleSourceRepository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item, ok := builtInRuleSourceRepository(id); ok {
		return item, nil
	}
	item, err := s.readRuleSourceRepository(id)
	return item, convertNotFound(err)
}

func (s *Store) CreateRuleSourceRepository(input RuleSourceRepository) (RuleSourceRepository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := normalizeRuleSourceRepository(input, RuleSourceRepository{})
	if err != nil {
		return RuleSourceRepository{}, err
	}
	if _, ok := builtInRuleSourceRepository(item.ID); ok {
		return RuleSourceRepository{}, fmt.Errorf("%w: %s", ErrBuiltInRepositoryReadOnly, item.ID)
	}
	if err := s.writeRuleSourceRepository(item, ""); err != nil {
		return RuleSourceRepository{}, err
	}
	return item, nil
}

func (s *Store) UpdateRuleSourceRepository(id string, input RuleSourceRepository) (RuleSourceRepository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := builtInRuleSourceRepository(id); ok {
		return RuleSourceRepository{}, ErrBuiltInRepositoryReadOnly
	}
	current, err := s.readRuleSourceRepository(id)
	if err != nil {
		return RuleSourceRepository{}, convertNotFound(err)
	}
	item, err := normalizeRuleSourceRepository(input, current)
	if err != nil {
		return RuleSourceRepository{}, err
	}
	item.CreatedAt = current.CreatedAt
	if err := s.writeRuleSourceRepository(item, current.ID); err != nil {
		return RuleSourceRepository{}, err
	}
	return item, nil
}

func (s *Store) DeleteRuleSourceRepository(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := builtInRuleSourceRepository(id); ok {
		return ErrBuiltInRepositoryReadOnly
	}
	if err := s.deleteResource(KindRuleSourceRepo, id); err != nil {
		return err
	}
	return s.deleteRuleSourceIndexFromDB(id)
}

func (s *Store) GetRuleSourceIndex(repositoryID string, requestedPath ...string) (RuleSourceIndex, error) {
	return s.GetRuleSourceIndexPage(repositoryID, firstStringArg(requestedPath), 0, 0)
}

func (s *Store) GetRuleSourceIndexPage(repositoryID string, requestedPath string, offset int, limit int) (RuleSourceIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	indexPath := normalizeRepositoryRootPath(requestedPath)
	item, err := s.readRuleSourceIndexFromDB(repositoryID, indexPath, offset, limit)
	if errors.Is(err, sql.ErrNoRows) {
		if index, ok := builtInRuleSourceIndexSnapshotPage(repositoryID, indexPath, offset, limit); ok {
			return index, nil
		}
		return emptyBuiltInRuleSourceIndex(repositoryID, indexPath)
	}
	if err != nil {
		return RuleSourceIndex{}, err
	}
	return item, nil
}

func (s *Store) GetRuleSourceIndexFlatPage(repositoryID string, offset int, limit int) (RuleSourceIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.readRuleSourceIndexMetadataFromDB(repositoryID, "")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if index, ok := builtInRuleSourceIndexSnapshotFlatPage(repositoryID, offset, limit); ok {
				return index, nil
			}
			return emptyBuiltInRuleSourceIndex(repositoryID, "")
		}
		return RuleSourceIndex{}, err
	}
	total, err := s.countAllRuleSourceIndexEntries(repositoryID)
	if err != nil {
		return RuleSourceIndex{}, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = total
	}
	entries, err := s.listAllRuleSourceIndexEntriesPage(repositoryID, offset, limit)
	if err != nil {
		return RuleSourceIndex{}, err
	}
	nextOffset := offset + len(entries)
	item.Path = ""
	item.Directories = nil
	item.Entries = entries
	item.Offset = offset
	item.Limit = limit
	item.Total = total
	item.NextOffset = nextOffset
	item.HasMore = nextOffset < total
	return item, nil
}

func (s *Store) FindRuleSourceIndexEntry(repositoryID string, logicalPath string) (RuleSourceIndexEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	logicalPath = normalizeRepositoryRootPath(logicalPath)
	entry, err := s.readRuleSourceIndexEntryFromDB(repositoryID, logicalPath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if entry, ok := builtInRuleSourceIndexSnapshotEntry(repositoryID, logicalPath); ok {
				return entry, nil
			}
		}
		return RuleSourceIndexEntry{}, convertNotFound(err)
	}
	return entry, nil
}

func emptyBuiltInRuleSourceIndex(repositoryID string, indexPath string) (RuleSourceIndex, error) {
	repo, ok := builtInRuleSourceRepository(repositoryID)
	if !ok {
		return RuleSourceIndex{}, ErrNotFound
	}
	return emptyRuleSourceIndex(repo, indexPath), nil
}

func firstStringArg(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (s *Store) UpsertRuleSourceIndex(input RuleSourceIndex) (RuleSourceIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(input.RepositoryID) == "" {
		return RuleSourceIndex{}, fmt.Errorf("%w: repository id is required", ErrInvalidRuleSourceRepository)
	}
	if input.RefreshedAt.IsZero() {
		input.RefreshedAt = time.Now().UTC()
	}
	sort.Slice(input.Entries, func(i, j int) bool {
		return input.Entries[i].LogicalPath < input.Entries[j].LogicalPath
	})
	if err := s.writeRuleSourceIndexToDB(input); err != nil {
		return RuleSourceIndex{}, err
	}
	s.removeStaleRuleSourceIndexCaches(input.RepositoryID)
	return s.readRuleSourceIndexFromDB(input.RepositoryID, "", 0, 0)
}

func (s *Store) UpsertRuleSourceSelectableFiles(input RuleSourceSelectableFiles) (RuleSourceSelectableFiles, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	repo, err := s.getRuleSourceRepositoryLocked(input.RepositoryID)
	if err != nil {
		return RuleSourceSelectableFiles{}, convertNotFound(err)
	}
	mapping, err := findRuleSourceCoreMapping(repo, input.Core)
	if err != nil {
		return RuleSourceSelectableFiles{}, err
	}
	existing, err := s.readRuleSourceIndexMetadataFromDB(input.RepositoryID, "")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return RuleSourceSelectableFiles{}, err
	}
	entriesByPath := map[string]RuleSourceIndexEntry{}
	if err == nil {
		existingEntries, err := s.listAllRuleSourceIndexEntries(input.RepositoryID)
		if err != nil {
			return RuleSourceSelectableFiles{}, err
		}
		for _, entry := range existingEntries {
			delete(entry.Files, input.Core)
			if len(entry.Files) > 0 {
				entriesByPath[entry.LogicalPath] = entry
			}
		}
	} else {
		existing.Refs = map[Core]string{}
	}
	if existing.Refs == nil {
		existing.Refs = map[Core]string{}
	}
	existing.Refs[input.Core] = mapping.Ref

	for _, file := range input.Files {
		logicalPath := normalizeRepositoryRootPath(file.Path)
		if logicalPath == "" {
			continue
		}
		rawURL, err := BuildRepositoryRawURL(repo, input.Core, file.Path, mapping.Ref)
		if err != nil {
			return RuleSourceSelectableFiles{}, err
		}
		entry := entriesByPath[logicalPath]
		if entry.Files == nil {
			entry.Files = map[Core]RuleSourceIndexFile{}
		}
		entry.LogicalPath = logicalPath
		entry.Name = file.Name
		entry.Files[input.Core] = RuleSourceIndexFile{
			Core:        input.Core,
			Path:        logicalPath,
			LogicalPath: logicalPath,
			Name:        file.Name,
			Kind:        file.Kind,
			Format:      file.Format,
			Behavior:    file.Behavior,
			RawURL:      rawURL,
		}
		entriesByPath[logicalPath] = entry
	}

	entries := make([]RuleSourceIndexEntry, 0, len(entriesByPath))
	for _, entry := range entriesByPath {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LogicalPath < entries[j].LogicalPath
	})
	index := RuleSourceIndex{
		RepositoryID: input.RepositoryID,
		Owner:        repo.Owner,
		Repository:   repo.Repository,
		Refs:         existing.Refs,
		RefreshedAt:  time.Now().UTC(),
		Entries:      entries,
	}
	if err := s.writeRuleSourceIndexToDB(index); err != nil {
		return RuleSourceSelectableFiles{}, err
	}
	s.removeStaleRuleSourceIndexCaches(input.RepositoryID)
	return s.listRuleSourceSelectableFilesFromDB(input.RepositoryID, input.Core)
}

func (s *Store) SearchRuleSourceIndex(repositoryID string, query string, limit int, filters ...RuleSourceIndexSearchFilters) (RuleSourceIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	query = strings.ToLower(strings.TrimSpace(query))
	limit = queryLimit(limit, 100, 500)
	filter := RuleSourceIndexSearchFilters{}
	if len(filters) > 0 {
		filter = filters[0]
	}
	filter.Offset = queryOffset(filter.Offset)
	root, err := s.readRuleSourceIndexMetadataFromDB(repositoryID, "")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if index, ok := builtInRuleSourceIndexSnapshotSearch(repositoryID, query, limit, filter); ok {
				return index, nil
			}
		}
		return RuleSourceIndex{}, convertNotFound(err)
	}
	if query == "" && isEmptyRuleSourceIndexSearchFilter(filter) {
		return s.readRuleSourceIndexFromDB(repositoryID, "", filter.Offset, limit)
	}
	result, err := s.searchRuleSourceIndexFromDB(repositoryID, root, query, limit, filter)
	if err != nil {
		return RuleSourceIndex{}, err
	}
	return result, nil
}

func splitRuleSourceIndexByDirectory(input RuleSourceIndex) map[string]RuleSourceIndex {
	base := func(indexPath string) RuleSourceIndex {
		return RuleSourceIndex{
			RepositoryID: input.RepositoryID,
			Owner:        input.Owner,
			Repository:   input.Repository,
			Path:         indexPath,
			Refs:         input.Refs,
			RefreshedAt:  input.RefreshedAt,
			Directories:  []RuleSourceIndexDirectory{},
			Entries:      []RuleSourceIndexEntry{},
		}
	}
	indexes := map[string]RuleSourceIndex{"": base("")}
	directoriesByParent := map[string]map[string]RuleSourceIndexDirectory{}
	ensureIndex := func(indexPath string) RuleSourceIndex {
		indexPath = normalizeRepositoryRootPath(indexPath)
		index, ok := indexes[indexPath]
		if !ok {
			index = base(indexPath)
			indexes[indexPath] = index
		}
		return index
	}
	addDirectory := func(parentPath string, dir RuleSourceIndexDirectory) {
		parentPath = normalizeRepositoryRootPath(parentPath)
		if directoriesByParent[parentPath] == nil {
			directoriesByParent[parentPath] = map[string]RuleSourceIndexDirectory{}
		}
		directoriesByParent[parentPath][dir.Path] = dir
		ensureIndex(dir.Path)
	}

	for _, entry := range input.Entries {
		entry.LogicalPath = normalizeRepositoryRootPath(entry.LogicalPath)
		folderPath := path.Dir(entry.LogicalPath)
		if folderPath == "." {
			folderPath = ""
		}
		index := ensureIndex(folderPath)
		index.Entries = append(index.Entries, entry)
		indexes[folderPath] = index

		parts := strings.Split(entry.LogicalPath, "/")
		for i := 0; i < len(parts)-1; i++ {
			dirPath := strings.Join(parts[:i+1], "/")
			parentPath := ""
			if i > 0 {
				parentPath = strings.Join(parts[:i], "/")
			}
			addDirectory(parentPath, RuleSourceIndexDirectory{
				Name: parts[i],
				Path: dirPath,
			})
		}
	}

	for indexPath, byPath := range directoriesByParent {
		index := ensureIndex(indexPath)
		for _, dir := range byPath {
			index.Directories = append(index.Directories, dir)
		}
		sort.Slice(index.Directories, func(i, j int) bool {
			return index.Directories[i].Path < index.Directories[j].Path
		})
		indexes[indexPath] = index
	}
	for indexPath, index := range indexes {
		sort.Slice(index.Entries, func(i, j int) bool {
			return index.Entries[i].LogicalPath < index.Entries[j].LogicalPath
		})
		indexes[indexPath] = index
	}
	return indexes
}

func (s *Store) ListSingBoxRuleSets() ([]SingBoxRuleSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listSingBoxRuleSets()
}

func (s *Store) QuerySingBoxRuleSets(offset int, limit int, query string) (SingBoxRuleSetPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.listSingBoxRuleSets()
	if err != nil {
		return SingBoxRuleSetPage{}, err
	}
	items = filterSingBoxRuleSets(items, query)
	pageItems, nextOffset, hasMore := paginateSlice(items, offset, limit)
	return SingBoxRuleSetPage{
		Items:      pageItems,
		Offset:     normalizePageOffset(offset, len(items)),
		Limit:      normalizePageLimit(limit, len(pageItems), len(items)),
		Total:      len(items),
		NextOffset: nextOffset,
		HasMore:    hasMore,
	}, nil
}

func (s *Store) GetSingBoxRuleSet(id string) (SingBoxRuleSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.readSingBoxRuleSet(id)
	return item, convertNotFound(err)
}

func (s *Store) CreateSingBoxRuleSet(input SingBoxRuleSetResource) (SingBoxRuleSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := normalizeSingBoxRuleSet(input, SingBoxRuleSetResource{})
	if err != nil {
		return SingBoxRuleSetResource{}, err
	}
	if err := s.validateSingBoxRuleSetRepositoriesLocked(item); err != nil {
		return SingBoxRuleSetResource{}, err
	}
	if err := s.writeSingBoxRuleSet(item, ""); err != nil {
		return SingBoxRuleSetResource{}, err
	}
	return item, nil
}

func (s *Store) UpdateSingBoxRuleSet(id string, input SingBoxRuleSetResource) (SingBoxRuleSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.readSingBoxRuleSet(id)
	if err != nil {
		return SingBoxRuleSetResource{}, convertNotFound(err)
	}
	item, err := normalizeSingBoxRuleSet(input, current)
	if err != nil {
		return SingBoxRuleSetResource{}, err
	}
	item.CreatedAt = current.CreatedAt
	if err := s.validateSingBoxRuleSetRepositoriesLocked(item); err != nil {
		return SingBoxRuleSetResource{}, err
	}
	if err := s.writeSingBoxRuleSet(item, current.ID); err != nil {
		return SingBoxRuleSetResource{}, err
	}
	return item, nil
}

func (s *Store) DeleteSingBoxRuleSet(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteResource(KindSingBoxRuleSet, id)
}

func (s *Store) ListMihomoRuleProviders() ([]MihomoRuleProviderResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listMihomoRuleProviders()
}

func (s *Store) QueryMihomoRuleProviders(offset int, limit int, query string) (MihomoRuleProviderPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.listMihomoRuleProviders()
	if err != nil {
		return MihomoRuleProviderPage{}, err
	}
	items = filterMihomoRuleProviders(items, query)
	pageItems, nextOffset, hasMore := paginateSlice(items, offset, limit)
	return MihomoRuleProviderPage{
		Items:      pageItems,
		Offset:     normalizePageOffset(offset, len(items)),
		Limit:      normalizePageLimit(limit, len(pageItems), len(items)),
		Total:      len(items),
		NextOffset: nextOffset,
		HasMore:    hasMore,
	}, nil
}

func (s *Store) GetMihomoRuleProvider(id string) (MihomoRuleProviderResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.readMihomoRuleProvider(id)
	return item, convertNotFound(err)
}

func (s *Store) CreateMihomoRuleProvider(input MihomoRuleProviderResource) (MihomoRuleProviderResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := normalizeMihomoRuleProvider(input, MihomoRuleProviderResource{})
	if err != nil {
		return MihomoRuleProviderResource{}, err
	}
	if err := s.validateMihomoRuleProviderRepositoriesLocked(item); err != nil {
		return MihomoRuleProviderResource{}, err
	}
	if err := s.writeMihomoRuleProvider(item, ""); err != nil {
		return MihomoRuleProviderResource{}, err
	}
	return item, nil
}

func (s *Store) UpdateMihomoRuleProvider(id string, input MihomoRuleProviderResource) (MihomoRuleProviderResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.readMihomoRuleProvider(id)
	if err != nil {
		return MihomoRuleProviderResource{}, convertNotFound(err)
	}
	item, err := normalizeMihomoRuleProvider(input, current)
	if err != nil {
		return MihomoRuleProviderResource{}, err
	}
	item.CreatedAt = current.CreatedAt
	if err := s.validateMihomoRuleProviderRepositoriesLocked(item); err != nil {
		return MihomoRuleProviderResource{}, err
	}
	if err := s.writeMihomoRuleProvider(item, current.ID); err != nil {
		return MihomoRuleProviderResource{}, err
	}
	return item, nil
}

func (s *Store) DeleteMihomoRuleProvider(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteResource(KindMihomoRuleProvider, id)
}

func (s *Store) ListGroupSets() ([]GroupSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listGroupSets()
}

func (s *Store) GetGroupSet(id string) (GroupSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.readGroupSet(id)
	return item, convertNotFound(err)
}

func (s *Store) CreateGroupSet(input GroupSetResource) (GroupSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := normalizeGroupSet(input, GroupSetResource{})
	if err := s.writeGroupSet(item, ""); err != nil {
		return GroupSetResource{}, err
	}
	return item, nil
}

func (s *Store) UpdateGroupSet(id string, input GroupSetResource) (GroupSetResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.readGroupSet(id)
	if err != nil {
		return GroupSetResource{}, convertNotFound(err)
	}
	item := normalizeGroupSet(input, current)
	item.CreatedAt = current.CreatedAt
	if err := s.writeGroupSet(item, current.ID); err != nil {
		return GroupSetResource{}, err
	}
	return item, nil
}

func (s *Store) DeleteGroupSet(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteResource(KindGroupSet, id)
}

func (s *Store) listProfiles() ([]ProfileResource, error) {
	rows, err := s.db.Query(`
		SELECT profile_json
		FROM profiles
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ProfileResource{}
	for rows.Next() {
		var profileJSON string
		if err := rows.Scan(&profileJSON); err != nil {
			return nil, err
		}
		var item ProfileResource
		if err := json.Unmarshal([]byte(profileJSON), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) listSubscriptions() ([]SubscriptionResource, error) {
	return listResources[SubscriptionResource](s, KindSubscription)
}

func (s *Store) listNodeSets() ([]NodeSetResource, error) {
	return s.listNodeSetResources()
}

func (s *Store) listNodeSetFiles() ([]NodeSetFile, error) {
	nodeSets, err := s.listNodeSetResources()
	if err != nil {
		return nil, err
	}
	items := make([]NodeSetFile, 0, len(nodeSets))
	for _, item := range nodeSets {
		items = append(items, NodeSetFile{
			FileName: item.ID,
			NodeSet:  item,
		})
	}
	return items, nil
}

func (s *Store) listRuleSets() ([]RuleSetResource, error) {
	return s.listRuleSetResources()
}

func (s *Store) listRuleSourceRepositories() ([]RuleSourceRepository, error) {
	items, err := listResources[RuleSourceRepository](s, KindRuleSourceRepo)
	if err != nil {
		return nil, err
	}
	items = append(items, BuiltInRuleSourceRepositories()...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].BuiltIn != items[j].BuiltIn {
			return items[i].BuiltIn
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items, nil
}

func (s *Store) listRuleSourceIndexes() ([]RuleSourceIndex, error) {
	rows, err := s.db.Query(`
		SELECT repository_id, owner, repository, refs_json, refreshed_at
		FROM rule_source_index_repositories
		ORDER BY refreshed_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RuleSourceIndex{}
	for rows.Next() {
		item, err := scanRuleSourceIndexMetadata(rows, "")
		if err != nil {
			return nil, err
		}
		dirs, err := s.listRuleSourceIndexDirectories(item.RepositoryID, "")
		if err != nil {
			return nil, err
		}
		item.Directories = dirs
		item.Entries = []RuleSourceIndexEntry{}
		item.Offset = 0
		item.Limit = 0
		item.Total = 0
		item.NextOffset = 0
		item.HasMore = false
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) listSingBoxRuleSets() ([]SingBoxRuleSetResource, error) {
	return listResources[SingBoxRuleSetResource](s, KindSingBoxRuleSet)
}

func (s *Store) listMihomoRuleProviders() ([]MihomoRuleProviderResource, error) {
	return listResources[MihomoRuleProviderResource](s, KindMihomoRuleProvider)
}

func (s *Store) listGroupSets() ([]GroupSetResource, error) {
	return s.listGroupSetResources()
}

func (s *Store) readProfile(id string) (ProfileResource, error) {
	var profileJSON string
	err := s.db.QueryRow(
		`SELECT profile_json FROM profiles WHERE profile_id = ?`,
		id,
	).Scan(&profileJSON)
	if err != nil {
		return ProfileResource{}, convertNotFound(err)
	}
	var item ProfileResource
	if err := json.Unmarshal([]byte(profileJSON), &item); err != nil {
		return ProfileResource{}, err
	}
	return item, nil
}

func (s *Store) writeProfile(item ProfileResource) error {
	profileJSON, err := json.Marshal(item)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO profiles (profile_id, name, selected_core, profile_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(profile_id) DO UPDATE SET
			name = excluded.name,
			selected_core = excluded.selected_core,
			profile_json = excluded.profile_json,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`, item.ID, item.Name, string(item.SelectedCore), string(profileJSON), item.CreatedAt.UTC().Format(time.RFC3339Nano), item.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) globalConfig() (GlobalConfig, error) {
	var config GlobalConfig
	if err := s.readRepositoryResource(configResourceKind, globalConfigResourceID, &config); errors.Is(err, sql.ErrNoRows) {
		config = defaultGlobalConfig(time.Now().UTC())
		if writeErr := s.writeGlobalConfig(config); writeErr != nil {
			return GlobalConfig{}, writeErr
		}
		return config, nil
	} else if err != nil {
		return GlobalConfig{}, err
	}
	if config, changed := backfillGlobalConfigDefaults(config, time.Now().UTC()); changed {
		if writeErr := s.writeGlobalConfig(config); writeErr != nil {
			return GlobalConfig{}, writeErr
		}
		return config, nil
	}
	return normalizeGlobalConfig(config), nil
}

func defaultGlobalConfig(updatedAt time.Time) GlobalConfig {
	return normalizeGlobalConfig(GlobalConfig{
		Fields:     defaultGlobalConfigFields(),
		DNSServers: defaultGlobalDNSServers(),
		Inbounds:   defaultGlobalInbounds(),
		UpdatedAt:  updatedAt,
	})
}

func backfillGlobalConfigDefaults(config GlobalConfig, updatedAt time.Time) (GlobalConfig, bool) {
	config = normalizeGlobalConfig(config)
	changed := false
	if config.Fields == nil {
		config.Fields = map[string]any{}
	}
	for key, value := range defaultGlobalConfigFields() {
		if _, exists := config.Fields[key]; !exists {
			config.Fields[key] = value
			changed = true
		}
	}
	if len(config.DNSServers) == 0 {
		config.DNSServers = defaultGlobalDNSServers()
		changed = true
	} else if hasLegacyDefaultProxyDNSServers(config.DNSServers) {
		config.DNSServers = replaceLegacyDefaultProxyDNSServers(config.DNSServers)
		changed = true
	}
	if len(config.Inbounds) == 0 {
		config.Inbounds = defaultGlobalInbounds()
		changed = true
	}
	if changed {
		config.UpdatedAt = updatedAt
	}
	return config, changed
}

func defaultGlobalConfigFields() map[string]any {
	return map[string]any{
		"allowLan":                       true,
		"authentication":                 "Clash:SEs3GYwN",
		"bindAddress":                    "*",
		"dnsEnabled":                     true,
		"dnsIpv6":                        false,
		"dnsListen":                      "0.0.0.0:7874",
		"dnsUseHosts":                    false,
		"dnsFakeIpRange":                 "198.18.0.1/15",
		"dnsFakeIpFilters":               strings.Join(defaultDNSFakeIPFilter, "\n"),
		"dnsMode":                        "fake-ip",
		"dnsMihomoRespectRules":          true,
		"dnsFakeIpFilterMode":            "blacklist",
		"dnsFakeIpEnabled":               true,
		"dnsDefaultStrategy":             "prefer_ipv4",
		"dnsCacheEnabled":                true,
		"dnsCacheAlgorithm":              "lru",
		"dnsCacheCapacity":               "0",
		"dnsOptimisticEnabled":           false,
		"dnsOptimisticTimeout":           "3d",
		"dnsTimeout":                     "10s",
		"dnsMihomoPreferH3":              false,
		"dnsMihomoFallbackGeoip":         true,
		"dnsMihomoFallbackGeoipCode":     "CN",
		"dnsSingBoxReverseMapping":       false,
		"dnsSingBoxClientSubnet":         "",
		"externalController":             "0.0.0.0:9090",
		"externalUi":                     "",
		"externalUiName":                 "",
		"externalUiUrl":                  "",
		"ipv6":                           false,
		"keepAliveIdle":                  "600",
		"keepAliveInterval":              "15",
		"logLevel":                       "info",
		"mode":                           "rule",
		"ntpEnabled":                     true,
		"ntpInterval":                    "30",
		"ntpServer":                      "time.apple.com",
		"ntpServerPort":                  "123",
		"ntpWriteToSystem":               true,
		"profileStoreSelected":           true,
		"routeAutoDetectInterface":       true,
		"routeBlockQuic":                 true,
		"secret":                         "Yi9ImtJh",
		"selectedCore":                   string(CoreMihomo),
		"routingRuleSetIds":              []string{},
		"snifferEnabled":                 true,
		"snifferForceDomain":             "+.netflix.com\n+.nflxvideo.net\n+.amazonaws.com\n+.media.dssott.com",
		"snifferHttpOverrideDestination": true,
		"snifferHttpPorts":               "80\n8080-8880",
		"snifferOverrideDestination":     true,
		"snifferParsePureIp":             true,
		"snifferQuicPorts":               "443",
		"snifferSkipDomain":              "Mijia Cloud\ndlg.io.mi.com\n+.oray.com\n+.sunlogin.net\n+.push.apple.com",
		"snifferTlsPorts":                "443\n8443",
		"unifiedDelay":                   true,
	}
}

func defaultGlobalInbounds() []ManagedInbound {
	return normalizeManagedInbounds([]ManagedInbound{
		{
			ID:      "http-in",
			Enabled: true,
			Tag:     "http-in",
			Kind:    "http",
			Listen:  InboundListen{Address: "0.0.0.0", Port: 7890},
		},
		{
			ID:      "socks-in",
			Enabled: true,
			Tag:     "socks-in",
			Kind:    "socks",
			Listen:  InboundListen{Address: "0.0.0.0", Port: 7891},
		},
		{
			ID:      "redirect-in",
			Enabled: true,
			Tag:     "redirect-in",
			Kind:    "redirect",
			Listen:  InboundListen{Address: "0.0.0.0", Port: 7892},
			Network: "tcp",
		},
		{
			ID:      "mixed-in",
			Enabled: true,
			Tag:     "mixed-in",
			Kind:    "mixed",
			Listen:  InboundListen{Address: "0.0.0.0", Port: 7893},
		},
		{
			ID:      "tproxy-in",
			Enabled: true,
			Tag:     "tproxy-in",
			Kind:    "tproxy",
			Listen:  InboundListen{Address: "0.0.0.0", Port: 7895},
			Network: "tcp",
		},
		{
			ID:      "tun-in",
			Enabled: true,
			Tag:     "tun-in",
			Kind:    "tun",
			Tun: InboundTun{
				Address:             []string{"172.19.0.1/30"},
				InterfaceName:       "utun101",
				Device:              "utun101",
				Stack:               "system",
				AutoRoute:           true,
				AutoRedirect:        true,
				AutoDetectInterface: true,
				StrictRoute:         false,
				DNSHijack:           []string{"any:53"},
			},
		},
	})
}

func defaultGlobalDNSServers() []GlobalDNSServer {
	servers := make([]GlobalDNSServer, 0, len(defaultDNSNameservers)+len(defaultProxyDNSNameservers))
	for index, endpoint := range defaultDNSNameservers {
		protocol, address, port, path := parseDefaultDNSServerEndpoint(endpoint)
		servers = append(servers, GlobalDNSServer{
			ID:       fmt.Sprintf("dns-default-%d", index+1),
			Name:     fmt.Sprintf("default-%d", index+1),
			Role:     "default",
			Protocol: protocol,
			Address:  address,
			Port:     port,
			Path:     path,
		})
	}
	servers = append(servers, proxyDefaultDNSServers()...)
	return servers
}

func parseDefaultDNSServerEndpoint(endpoint string) (string, string, string, string) {
	protocol := "udp"
	target := endpoint
	if scheme, rest, ok := strings.Cut(endpoint, "://"); ok {
		protocol = scheme
		target = rest
	}
	path := ""
	if protocol == "https" || protocol == "h3" {
		if host, value, ok := strings.Cut(target, "/"); ok {
			target = host
			path = "/" + strings.TrimLeft(value, "/")
		}
	}
	address := target
	port := ""
	if host, value, ok := strings.Cut(target, ":"); ok {
		address = host
		port = value
	} else if protocol == "udp" {
		port = "53"
	}
	return protocol, address, port, path
}

func hasLegacyDefaultProxyDNSServers(servers []GlobalDNSServer) bool {
	proxyServers := make([]GlobalDNSServer, 0, len(servers))
	for _, server := range servers {
		if strings.TrimSpace(server.Role) == "proxy" {
			proxyServers = append(proxyServers, server)
		}
	}
	if len(proxyServers) != len(defaultDNSNameservers) {
		return false
	}
	for index, endpoint := range defaultDNSNameservers {
		if !dnsServerMatchesEndpoint(proxyServers[index], endpoint) {
			return false
		}
	}
	return true
}

func replaceLegacyDefaultProxyDNSServers(servers []GlobalDNSServer) []GlobalDNSServer {
	next := make([]GlobalDNSServer, 0, len(servers)-len(defaultDNSNameservers)+len(defaultProxyDNSNameservers))
	for _, server := range servers {
		if strings.TrimSpace(server.Role) != "proxy" {
			next = append(next, server)
		}
	}
	next = append(next, proxyDefaultDNSServers()...)
	return normalizeGlobalDNSServers(next)
}

func proxyDefaultDNSServers() []GlobalDNSServer {
	servers := make([]GlobalDNSServer, 0, len(defaultProxyDNSNameservers))
	for index, endpoint := range defaultProxyDNSNameservers {
		protocol, address, port, path := parseDefaultDNSServerEndpoint(endpoint)
		servers = append(servers, GlobalDNSServer{
			ID:       fmt.Sprintf("dns-proxy-%d", index+1),
			Name:     fmt.Sprintf("proxy-%d", index+1),
			Role:     "proxy",
			Protocol: protocol,
			Address:  address,
			Port:     port,
			Path:     path,
		})
	}
	return servers
}

func dnsServerMatchesEndpoint(server GlobalDNSServer, endpoint string) bool {
	protocol, address, port, path := parseDefaultDNSServerEndpoint(endpoint)
	return strings.TrimSpace(server.Protocol) == protocol &&
		strings.TrimSpace(server.Address) == address &&
		strings.TrimSpace(server.Port) == port &&
		strings.TrimSpace(server.Path) == path
}

func (s *Store) writeGlobalConfig(config GlobalConfig) error {
	if config.UpdatedAt.IsZero() {
		config.UpdatedAt = time.Now().UTC()
	}
	config = normalizeGlobalConfig(config)
	return s.writeRepositoryResource(
		configResourceKind,
		globalConfigResourceID,
		globalConfigResourceID,
		config.UpdatedAt,
		config.UpdatedAt,
		config,
	)
}

func (s *Store) deleteResource(kind ResourceKind, id string) error {
	removed, err := s.deleteRepositoryResource(string(kind), id)
	if err != nil {
		return err
	}
	if !removed {
		return ErrNotFound
	}
	return nil
}

func (s *Store) readRepositoryResource(kind string, id string, dst any) error {
	var payload string
	err := s.db.QueryRow(`
		SELECT resource_json
		FROM repository_resources
		WHERE resource_kind = ? AND resource_id = ?
	`, kind, id).Scan(&payload)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(payload), dst)
}

func readResource[T any](s *Store, kind ResourceKind, id string) (T, error) {
	var item T
	err := s.readRepositoryResource(string(kind), id, &item)
	return item, err
}

func listResources[T interface{ GetUpdatedAt() time.Time }](s *Store, kind ResourceKind) ([]T, error) {
	rows, err := s.db.Query(`
		SELECT resource_json
		FROM repository_resources
		WHERE resource_kind = ?
		ORDER BY updated_at DESC
	`, string(kind))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []T{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var item T
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func paginateSlice[T any](items []T, offset int, limit int) ([]T, int, bool) {
	total := len(items)
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
	hasMore := end < total
	return items[offset:end], nextOffset, hasMore
}

func matchesSearchQuery(query string, values ...string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(strings.TrimSpace(value)), query) {
			return true
		}
	}
	return false
}

func filterRuleSourceRepositories(items []RuleSourceRepository, query string) []RuleSourceRepository {
	if strings.TrimSpace(query) == "" {
		return items
	}
	filtered := make([]RuleSourceRepository, 0, len(items))
	for _, item := range items {
		if matchesSearchQuery(
			query,
			item.ID,
			item.Name,
			item.Description,
			item.Owner,
			item.Repository,
		) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterSingBoxRuleSets(items []SingBoxRuleSetResource, query string) []SingBoxRuleSetResource {
	if strings.TrimSpace(query) == "" {
		return items
	}
	filtered := make([]SingBoxRuleSetResource, 0, len(items))
	for _, item := range items {
		if matchesSearchQuery(
			query,
			item.ID,
			item.Name,
			item.Description,
			item.Tag,
			string(item.SourceMode),
			item.RepositoryID,
			item.Path,
			item.URL,
			item.LocalPath,
			item.Format,
			item.UpdateInterval,
		) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterMihomoRuleProviders(items []MihomoRuleProviderResource, query string) []MihomoRuleProviderResource {
	if strings.TrimSpace(query) == "" {
		return items
	}
	filtered := make([]MihomoRuleProviderResource, 0, len(items))
	for _, item := range items {
		if matchesSearchQuery(
			query,
			item.ID,
			item.Name,
			item.Description,
			item.Provider,
			string(item.SourceMode),
			item.RepositoryID,
			item.Path,
			item.URL,
			item.LocalPath,
			item.Behavior,
			item.Format,
			item.Interval,
		) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func normalizePageOffset(offset int, total int) int {
	if offset < 0 {
		return 0
	}
	if offset > total {
		return total
	}
	return offset
}

func normalizePageLimit(limit int, pageSize int, total int) int {
	if limit > 0 {
		return limit
	}
	if total == 0 {
		return 0
	}
	return pageSize
}

func (s *Store) writeRepositoryResource(kind string, id string, name string, createdAt time.Time, updatedAt time.Time, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO repository_resources (
			resource_kind, resource_id, name, resource_json, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(resource_kind, resource_id) DO UPDATE SET
			name = excluded.name,
			resource_json = excluded.resource_json,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`, kind, id, name, string(payload), createdAt.UTC().Format(time.RFC3339Nano), updatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) deleteRepositoryResource(kind string, id string) (bool, error) {
	result, err := s.db.Exec(`
		DELETE FROM repository_resources
		WHERE resource_kind = ? AND resource_id = ?
	`, kind, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (s *Store) readSubscription(id string) (SubscriptionResource, error) {
	return readResource[SubscriptionResource](s, KindSubscription, id)
}

func (s *Store) readNodeSet(id string) (NodeSetResource, error) {
	return readResource[NodeSetResource](s, KindNodeSet, id)
}

func (s *Store) writeSubscription(item SubscriptionResource, previousID string) error {
	if err := s.writeRepositoryResource(string(KindSubscription), item.ID, item.Name, item.CreatedAt, item.UpdatedAt, item); err != nil {
		return err
	}
	if previousID != "" && previousID != item.ID {
		_, err := s.deleteRepositoryResource(string(KindSubscription), previousID)
		return err
	}
	return nil
}

func (s *Store) writeNodeSet(item NodeSetResource, previousID string) error {
	if err := s.writeRepositoryResource(string(KindNodeSet), item.ID, item.Name, item.CreatedAt, item.UpdatedAt, item); err != nil {
		return err
	}
	if previousID != "" && previousID != item.ID {
		_, err := s.deleteRepositoryResource(string(KindNodeSet), previousID)
		return err
	}
	return nil
}

func (s *Store) listNodeSetResources() ([]NodeSetResource, error) {
	return listResources[NodeSetResource](s, KindNodeSet)
}

func (s *Store) readManagedNodeSetsLocked() ([]NodeSetResource, error) {
	return s.listNodeSetResources()
}

func (s *Store) readManagedNodeSetLocked(id string) (NodeSetResource, error) {
	items, err := s.readManagedNodeSetsLocked()
	if err != nil {
		return NodeSetResource{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return NodeSetResource{}, ErrNotFound
}

func (s *Store) writeManagedNodeSetLocked(item NodeSetResource) error {
	items, err := s.readManagedNodeSetsLocked()
	if err != nil {
		return err
	}
	if err := validateManagedNodeSetNames(items, item); err != nil {
		return err
	}
	replaced := false
	for index, existing := range items {
		if existing.ID != item.ID {
			continue
		}
		items[index] = item
		replaced = true
		break
	}
	if !replaced {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return s.writeNodeSet(item, "")
}

func (s *Store) deleteManagedNodeSetLocked(id string) error {
	removed, err := s.deleteRepositoryResource(string(KindNodeSet), id)
	if err != nil {
		return err
	}
	if !removed {
		return ErrNotFound
	}
	return nil
}

func (s *Store) renameManagedNodeSetLocked(oldID string, newID string, newName string) error {
	items, err := s.readManagedNodeSetsLocked()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(items, func(item NodeSetResource) bool {
		return item.ID == oldID
	})
	if index < 0 {
		return ErrNotFound
	}
	item := items[index]
	item.ID = newID
	item.Name = newName
	item.UpdatedAt = time.Now().UTC()
	for nodeIndex, node := range item.Nodes {
		node.Source = newName
		item.Nodes[nodeIndex] = node
	}
	others := append([]NodeSetResource{}, items[:index]...)
	others = append(others, items[index+1:]...)
	if err := validateManagedNodeSetNames(others, item); err != nil {
		return err
	}
	return s.writeNodeSet(item, oldID)
}

func (s *Store) readRuleSet(id string) (RuleSetResource, error) {
	return readResource[RuleSetResource](s, KindRoutingRuleSet, id)
}

func (s *Store) writeRuleSet(item RuleSetResource, previousID string) error {
	if err := s.writeRepositoryResource(string(KindRoutingRuleSet), item.ID, item.Name, item.CreatedAt, item.UpdatedAt, item); err != nil {
		return err
	}
	if previousID != "" && previousID != item.ID {
		_, err := s.deleteRepositoryResource(string(KindRoutingRuleSet), previousID)
		return err
	}
	return nil
}

func (s *Store) listRuleSetResources() ([]RuleSetResource, error) {
	return listResources[RuleSetResource](s, KindRoutingRuleSet)
}

func (s *Store) readRuleSourceRepository(id string) (RuleSourceRepository, error) {
	return readResource[RuleSourceRepository](s, KindRuleSourceRepo, id)
}

func (s *Store) writeRuleSourceRepository(item RuleSourceRepository, previousID string) error {
	if err := s.writeRepositoryResource(string(KindRuleSourceRepo), item.ID, item.Name, item.CreatedAt, item.UpdatedAt, item); err != nil {
		return err
	}
	if previousID != "" && previousID != item.ID {
		_, err := s.deleteRepositoryResource(string(KindRuleSourceRepo), previousID)
		return err
	}
	return nil
}

func (s *Store) ruleSourceIndexRepositoryDir(repositoryID string) string {
	return filepath.Join(s.dataDir, sanitizeResourceName(repositoryID))
}

func (s *Store) writeRuleSourceIndexToDB(item RuleSourceIndex) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM rule_source_index_entries WHERE repository_id = ?`, item.RepositoryID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM rule_source_index_directories WHERE repository_id = ?`, item.RepositoryID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM rule_source_index_repositories WHERE repository_id = ?`, item.RepositoryID); err != nil {
		return err
	}

	refsJSON, err := json.Marshal(item.Refs)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO rule_source_index_repositories (repository_id, owner, repository, refs_json, refreshed_at)
		 VALUES (?, ?, ?, ?, ?)`,
		item.RepositoryID,
		item.Owner,
		item.Repository,
		string(refsJSON),
		item.RefreshedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return err
	}

	indexes := splitRuleSourceIndexByDirectory(item)
	insertDirectory, err := tx.Prepare(`
		INSERT INTO rule_source_index_directories (repository_id, path, name, parent_path)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(repository_id, path) DO UPDATE SET
			name = excluded.name,
			parent_path = excluded.parent_path
	`)
	if err != nil {
		return err
	}
	defer insertDirectory.Close()
	for parentPath, index := range indexes {
		for _, directory := range index.Directories {
			if _, err := insertDirectory.Exec(
				item.RepositoryID,
				directory.Path,
				directory.Name,
				normalizeRepositoryRootPath(parentPath),
			); err != nil {
				return err
			}
		}
	}

	insertEntry, err := tx.Prepare(`
		INSERT INTO rule_source_index_entries (repository_id, logical_path, name, parent_path, files_json, search_text)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer insertEntry.Close()
	for _, entry := range item.Entries {
		entry.LogicalPath = normalizeRepositoryRootPath(entry.LogicalPath)
		if entry.LogicalPath == "" {
			continue
		}
		parentPath := path.Dir(entry.LogicalPath)
		if parentPath == "." {
			parentPath = ""
		}
		filesJSON, err := json.Marshal(entry.Files)
		if err != nil {
			return err
		}
		if _, err := insertEntry.Exec(
			item.RepositoryID,
			entry.LogicalPath,
			entry.Name,
			parentPath,
			string(filesJSON),
			ruleSourceIndexEntrySearchText(entry),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) deleteRuleSourceIndexFromDB(repositoryID string) error {
	if _, err := s.db.Exec(`DELETE FROM rule_source_index_entries WHERE repository_id = ?`, repositoryID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM rule_source_index_directories WHERE repository_id = ?`, repositoryID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM rule_source_index_repositories WHERE repository_id = ?`, repositoryID); err != nil {
		return err
	}
	s.removeStaleRuleSourceIndexCaches(repositoryID)
	return nil
}

func (s *Store) removeStaleRuleSourceIndexCaches(repositoryID string) {
	name := sanitizeResourceName(repositoryID)
	_ = os.RemoveAll(s.ruleSourceIndexRepositoryDir(repositoryID))
	for _, filename := range []string{
		name + ".json",
		name + ".jsonl",
		name + ".search.jsonl",
		name + "-search.jsonl",
	} {
		_ = os.Remove(filepath.Join(s.dataDir, filename))
	}
}

func (s *Store) readRuleSourceIndexFromDB(repositoryID string, indexPath string, offset int, limit int) (RuleSourceIndex, error) {
	indexPath = normalizeRepositoryRootPath(indexPath)
	item, err := s.readRuleSourceIndexMetadataFromDB(repositoryID, indexPath)
	if err != nil {
		return RuleSourceIndex{}, err
	}
	directories, err := s.listRuleSourceIndexDirectories(repositoryID, indexPath)
	if err != nil {
		return RuleSourceIndex{}, err
	}
	total, err := s.countRuleSourceIndexEntries(repositoryID, indexPath)
	if err != nil {
		return RuleSourceIndex{}, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = total
	}
	entries, err := s.listRuleSourceIndexEntries(repositoryID, indexPath, offset, limit)
	if err != nil {
		return RuleSourceIndex{}, err
	}
	nextOffset := offset + len(entries)
	item.Directories = directories
	item.Entries = entries
	item.Offset = offset
	item.Limit = limit
	item.Total = total
	item.NextOffset = nextOffset
	item.HasMore = nextOffset < total
	return item, nil
}

func (s *Store) readRuleSourceIndexMetadataFromDB(repositoryID string, indexPath string) (RuleSourceIndex, error) {
	return scanRuleSourceIndexMetadata(
		s.db.QueryRow(`
			SELECT repository_id, owner, repository, refs_json, refreshed_at
			FROM rule_source_index_repositories
			WHERE repository_id = ?
		`, repositoryID),
		normalizeRepositoryRootPath(indexPath),
	)
}

func (s *Store) listRuleSourceIndexDirectories(repositoryID string, parentPath string) ([]RuleSourceIndexDirectory, error) {
	parentPath = normalizeRepositoryRootPath(parentPath)
	rows, err := s.db.Query(`
		SELECT name, path
		FROM rule_source_index_directories
		WHERE repository_id = ? AND parent_path = ?
		ORDER BY path
	`, repositoryID, parentPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RuleSourceIndexDirectory{}
	for rows.Next() {
		var item RuleSourceIndexDirectory
		if err := rows.Scan(&item.Name, &item.Path); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) countRuleSourceIndexEntries(repositoryID string, parentPath string) (int, error) {
	parentPath = normalizeRepositoryRootPath(parentPath)
	var total int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM rule_source_index_entries
		WHERE repository_id = ? AND parent_path = ?
	`, repositoryID, parentPath).Scan(&total)
	return total, err
}

func (s *Store) listRuleSourceIndexEntries(repositoryID string, parentPath string, offset int, limit int) ([]RuleSourceIndexEntry, error) {
	parentPath = normalizeRepositoryRootPath(parentPath)
	rows, err := s.db.Query(`
		SELECT logical_path, name, files_json
		FROM rule_source_index_entries
		WHERE repository_id = ? AND parent_path = ?
		ORDER BY logical_path
		LIMIT ? OFFSET ?
	`, repositoryID, parentPath, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRuleSourceIndexEntries(rows)
}

func (s *Store) listAllRuleSourceIndexEntries(repositoryID string) ([]RuleSourceIndexEntry, error) {
	rows, err := s.db.Query(`
		SELECT logical_path, name, files_json
		FROM rule_source_index_entries
		WHERE repository_id = ?
		ORDER BY logical_path
	`, repositoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRuleSourceIndexEntries(rows)
}

func (s *Store) countAllRuleSourceIndexEntries(repositoryID string) (int, error) {
	var total int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM rule_source_index_entries
		WHERE repository_id = ?
	`, repositoryID).Scan(&total)
	return total, err
}

func (s *Store) listAllRuleSourceIndexEntriesPage(repositoryID string, offset int, limit int) ([]RuleSourceIndexEntry, error) {
	rows, err := s.db.Query(`
		SELECT logical_path, name, files_json
		FROM rule_source_index_entries
		WHERE repository_id = ?
		ORDER BY logical_path
		LIMIT ? OFFSET ?
	`, repositoryID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRuleSourceIndexEntries(rows)
}

func (s *Store) listRuleSourceSelectableFilesFromDB(repositoryID string, core Core) (RuleSourceSelectableFiles, error) {
	root, err := s.readRuleSourceIndexMetadataFromDB(repositoryID, "")
	if err != nil {
		return RuleSourceSelectableFiles{}, err
	}
	entries, err := s.listAllRuleSourceIndexEntries(repositoryID)
	if err != nil {
		return RuleSourceSelectableFiles{}, err
	}
	files := []RuleSourceSelectableFile{}
	for _, entry := range entries {
		file, ok := entry.Files[core]
		if !ok {
			continue
		}
		files = append(files, RuleSourceSelectableFile{
			Name:     file.Name,
			Path:     file.Path,
			Kind:     file.Kind,
			Format:   file.Format,
			Behavior: file.Behavior,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return RuleSourceSelectableFiles{
		RepositoryID: repositoryID,
		Core:         core,
		Ref:          root.Refs[core],
		RefreshedAt:  root.RefreshedAt,
		Files:        files,
	}, nil
}

func (s *Store) readRuleSourceIndexEntryFromDB(repositoryID string, logicalPath string) (RuleSourceIndexEntry, error) {
	rows, err := s.db.Query(`
		SELECT logical_path, name, files_json
		FROM rule_source_index_entries
		WHERE repository_id = ? AND logical_path = ?
	`, repositoryID, normalizeRepositoryRootPath(logicalPath))
	if err != nil {
		return RuleSourceIndexEntry{}, err
	}
	defer rows.Close()
	entries, err := scanRuleSourceIndexEntries(rows)
	if err != nil {
		return RuleSourceIndexEntry{}, err
	}
	if len(entries) == 0 {
		return RuleSourceIndexEntry{}, sql.ErrNoRows
	}
	return entries[0], nil
}

func (s *Store) searchRuleSourceIndexFromDB(repositoryID string, root RuleSourceIndex, query string, limit int, filters RuleSourceIndexSearchFilters) (RuleSourceIndex, error) {
	where, args := ruleSourceIndexSearchWhere(repositoryID, query, filters)
	var total int
	countSQL := `
		SELECT COUNT(*)
		FROM rule_source_index_entries
		WHERE ` + where
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return RuleSourceIndex{}, err
	}
	querySQL := `
		SELECT logical_path, name, files_json
		FROM rule_source_index_entries
		WHERE ` + where + `
		ORDER BY logical_path
		LIMIT ? OFFSET ?`
	rows, err := s.db.Query(querySQL, append(args, limit, filters.Offset)...)
	if err != nil {
		return RuleSourceIndex{}, err
	}
	defer rows.Close()
	entries, err := scanRuleSourceIndexEntries(rows)
	if err != nil {
		return RuleSourceIndex{}, err
	}
	root.Directories = []RuleSourceIndexDirectory{}
	root.Entries = entries
	nextOffset := filters.Offset + len(entries)
	root.Offset = filters.Offset
	root.Limit = limit
	root.Total = total
	root.NextOffset = nextOffset
	root.HasMore = nextOffset < total
	return root, nil
}

func ruleSourceIndexSearchWhere(repositoryID string, query string, filters RuleSourceIndexSearchFilters) (string, []any) {
	clauses := []string{"repository_id = ?"}
	args := []any{repositoryID}
	query = strings.ToLower(strings.TrimSpace(query))
	if query != "" {
		clauses = append(clauses, "search_text LIKE ?")
		args = append(args, "%"+query+"%")
	}
	if filters.Core != "" {
		clauses = append(clauses, "files_json LIKE ?")
		args = append(args, `%"core":"`+string(filters.Core)+`"%`)
	}
	if value := strings.TrimSpace(filters.Format); value != "" {
		clauses = append(clauses, "files_json LIKE ?")
		args = append(args, `%"format":"`+value+`"%`)
	}
	if value := strings.TrimSpace(filters.Behavior); value != "" {
		clauses = append(clauses, "files_json LIKE ?")
		args = append(args, `%"behavior":"`+value+`"%`)
	}
	if filters.Kind != "" {
		clauses = append(clauses, "files_json LIKE ?")
		args = append(args, `%"kind":"`+string(filters.Kind)+`"%`)
	}
	if value := normalizeRepositoryRootPath(filters.PathPrefix); value != "" {
		clauses = append(clauses, "(logical_path = ? OR logical_path LIKE ?)")
		args = append(args, value, value+"/%")
	}
	return strings.Join(clauses, " AND "), args
}

func isEmptyRuleSourceIndexSearchFilter(filters RuleSourceIndexSearchFilters) bool {
	return filters.Core == "" &&
		strings.TrimSpace(filters.Format) == "" &&
		strings.TrimSpace(filters.Behavior) == "" &&
		filters.Kind == "" &&
		strings.TrimSpace(filters.PathPrefix) == ""
}

type ruleSourceIndexMetadataScanner interface {
	Scan(dest ...any) error
}

func scanRuleSourceIndexMetadata(scanner ruleSourceIndexMetadataScanner, indexPath string) (RuleSourceIndex, error) {
	var item RuleSourceIndex
	var refsJSON string
	var refreshedAt string
	if err := scanner.Scan(&item.RepositoryID, &item.Owner, &item.Repository, &refsJSON, &refreshedAt); err != nil {
		return RuleSourceIndex{}, err
	}
	if strings.TrimSpace(refsJSON) != "" {
		if err := json.Unmarshal([]byte(refsJSON), &item.Refs); err != nil {
			return RuleSourceIndex{}, err
		}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, refreshedAt); err == nil {
		item.RefreshedAt = parsed
	}
	item.Path = normalizeRepositoryRootPath(indexPath)
	item.Directories = []RuleSourceIndexDirectory{}
	item.Entries = []RuleSourceIndexEntry{}
	return item, nil
}

func scanRuleSourceIndexEntries(rows *sql.Rows) ([]RuleSourceIndexEntry, error) {
	items := []RuleSourceIndexEntry{}
	for rows.Next() {
		var item RuleSourceIndexEntry
		var filesJSON string
		if err := rows.Scan(&item.LogicalPath, &item.Name, &filesJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(filesJSON), &item.Files); err != nil {
			return nil, err
		}
		if item.Files == nil {
			item.Files = map[Core]RuleSourceIndexFile{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func ruleSourceIndexEntrySearchText(entry RuleSourceIndexEntry) string {
	values := []string{entry.LogicalPath, entry.Name}
	for core, file := range entry.Files {
		values = append(values,
			string(core),
			file.Path,
			file.LogicalPath,
			file.Name,
			string(file.Kind),
			file.Format,
			file.Behavior,
			file.RawURL,
		)
	}
	return strings.ToLower(strings.Join(values, " "))
}

func (s *Store) readSingBoxRuleSet(id string) (SingBoxRuleSetResource, error) {
	return readResource[SingBoxRuleSetResource](s, KindSingBoxRuleSet, id)
}

func (s *Store) writeSingBoxRuleSet(item SingBoxRuleSetResource, previousID string) error {
	if err := s.writeRepositoryResource(string(KindSingBoxRuleSet), item.ID, item.Name, item.CreatedAt, item.UpdatedAt, item); err != nil {
		return err
	}
	if previousID != "" && previousID != item.ID {
		_, err := s.deleteRepositoryResource(string(KindSingBoxRuleSet), previousID)
		return err
	}
	return nil
}

func (s *Store) readMihomoRuleProvider(id string) (MihomoRuleProviderResource, error) {
	return readResource[MihomoRuleProviderResource](s, KindMihomoRuleProvider, id)
}

func (s *Store) writeMihomoRuleProvider(item MihomoRuleProviderResource, previousID string) error {
	if err := s.writeRepositoryResource(string(KindMihomoRuleProvider), item.ID, item.Name, item.CreatedAt, item.UpdatedAt, item); err != nil {
		return err
	}
	if previousID != "" && previousID != item.ID {
		_, err := s.deleteRepositoryResource(string(KindMihomoRuleProvider), previousID)
		return err
	}
	return nil
}

func (s *Store) validateSingBoxRuleSetRepositoriesLocked(item SingBoxRuleSetResource) error {
	if item.SourceMode != RuleAssetSourceModeRepositoryFile {
		return nil
	}
	repo, err := s.getRuleSourceRepositoryLocked(item.RepositoryID)
	if err != nil {
		return err
	}
	_, err = findRuleSourceCoreMapping(repo, CoreSingBox)
	return err
}

func (s *Store) validateMihomoRuleProviderRepositoriesLocked(item MihomoRuleProviderResource) error {
	if item.SourceMode != RuleAssetSourceModeRepositoryFile {
		return nil
	}
	repo, err := s.getRuleSourceRepositoryLocked(item.RepositoryID)
	if err != nil {
		return err
	}
	_, err = findRuleSourceCoreMapping(repo, CoreMihomo)
	return err
}

func (s *Store) getRuleSourceRepositoryLocked(id string) (RuleSourceRepository, error) {
	if item, ok := builtInRuleSourceRepository(id); ok {
		return item, nil
	}
	item, err := s.readRuleSourceRepository(id)
	return item, convertNotFound(err)
}

func (s *Store) readGroupSet(id string) (GroupSetResource, error) {
	return readResource[GroupSetResource](s, KindGroupSet, id)
}

func (s *Store) writeGroupSet(item GroupSetResource, previousID string) error {
	if err := s.writeRepositoryResource(string(KindGroupSet), item.ID, item.Name, item.CreatedAt, item.UpdatedAt, item); err != nil {
		return err
	}
	if previousID != "" && previousID != item.ID {
		_, err := s.deleteRepositoryResource(string(KindGroupSet), previousID)
		return err
	}
	return nil
}

func (s *Store) listGroupSetResources() ([]GroupSetResource, error) {
	return listResources[GroupSetResource](s, KindGroupSet)
}

func convertNotFound(err error) error {
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func normalizeProfile(input ProfileResource, current ProfileResource) ProfileResource {
	now := time.Now().UTC()
	item := input
	if item.ID == "" {
		item.ID = NewID("profile")
	}
	item.Kind = KindProfile
	item.Name = chooseName(item.Name, current.Name, "Untitled profile")
	item.OriginType = chooseOrigin(item.OriginType, current.OriginType)
	item.Description = chooseString(item.Description, current.Description)
	item.SelectedCore = chooseCore(item.SelectedCore, current.SelectedCore)
	item.SubscriptionIDs = uniqueStrings(item.SubscriptionIDs)
	item.NodeSetIDs = uniqueStrings(item.NodeSetIDs)
	item.RuleSetIDs = uniqueStrings(item.RuleSetIDs)
	item.GroupSetIDs = uniqueStrings(item.GroupSetIDs)
	if current.CreatedAt.IsZero() {
		item.CreatedAt = now
	} else {
		item.CreatedAt = current.CreatedAt
	}
	item.UpdatedAt = now
	return item
}

func normalizeSubscription(input SubscriptionResource, current SubscriptionResource) SubscriptionResource {
	now := time.Now().UTC()
	item := input
	item.Kind = KindSubscription
	item.Name = chooseName(item.Name, current.Name, "Untitled subscription")
	item.ID = item.Name
	item.OriginType = chooseOrigin(item.OriginType, current.OriginType)
	item.Description = chooseString(item.Description, current.Description)
	item.SourceURL = chooseString(item.SourceURL, current.SourceURL)
	item.Revision = chooseString(item.Revision, current.Revision)
	item.Fetch = normalizeSubscriptionFetch(item.Fetch, current.Fetch, item.SourceURL)
	item.AutoUpdate = normalizeSubscriptionAutoUpdate(item.AutoUpdate, current.AutoUpdate)
	item.Sync = normalizeSubscriptionSync(item.Sync, current.Sync)
	if current.CreatedAt.IsZero() {
		item.CreatedAt = now
	} else {
		item.CreatedAt = current.CreatedAt
	}
	item.UpdatedAt = now
	return item
}

func normalizeSubscriptionFetch(input SubscriptionFetchOptions, current SubscriptionFetchOptions, sourceURL string) SubscriptionFetchOptions {
	return SubscriptionFetchOptions{
		SourceInput: chooseString(input.SourceInput, current.SourceInput, sourceURL),
		UserAgent:   chooseString(input.UserAgent, current.UserAgent, "Clash"),
	}
}

func normalizeSubscriptionAutoUpdate(input SubscriptionAutoUpdate, current SubscriptionAutoUpdate) SubscriptionAutoUpdate {
	if input.IntervalMinutes <= 0 {
		if current.IntervalMinutes > 0 {
			input.IntervalMinutes = current.IntervalMinutes
		} else {
			input.IntervalMinutes = 60
		}
	}
	return input
}

func normalizeSubscriptionSync(input SubscriptionSyncStatus, current SubscriptionSyncStatus) SubscriptionSyncStatus {
	if input.LastSyncedAt.IsZero() {
		input.LastSyncedAt = current.LastSyncedAt
	}
	if strings.TrimSpace(input.LastSyncError) == "" && input.LastSyncedAt.Equal(current.LastSyncedAt) {
		input.LastSyncError = current.LastSyncError
	}
	return input
}

func normalizeNodeSet(input NodeSetResource, current NodeSetResource) NodeSetResource {
	now := time.Now().UTC()
	item := input
	item.Kind = KindNodeSet
	item.Name = chooseName(item.Name, current.Name, "Untitled node set")
	item.ID = item.Name
	item.OriginType = chooseOrigin(item.OriginType, current.OriginType)
	if current.CreatedAt.IsZero() {
		item.CreatedAt = now
	} else {
		item.CreatedAt = current.CreatedAt
	}
	item.UpdatedAt = now
	return item
}

func normalizeRuleSet(input RuleSetResource, current RuleSetResource) RuleSetResource {
	now := time.Now().UTC()
	item := input
	item.Kind = KindRoutingRuleSet
	item.Name = chooseName(item.Name, current.Name, "Untitled rule set")
	item.ID = item.Name
	item.OriginType = chooseOrigin(item.OriginType, current.OriginType)
	item.SupportedCores = normalizeRuleSetSupportedCores(item.SupportedCores, item.Rules)
	if current.CreatedAt.IsZero() {
		item.CreatedAt = now
	} else {
		item.CreatedAt = current.CreatedAt
	}
	item.UpdatedAt = now
	return item
}

func normalizeRuleSetSupportedCores(input []Core, rules []NormalizedRule) []Core {
	derived := supportedCoresFromRules(rules)
	if len(derived) > 0 {
		return derived
	}
	return uniqueCores(input)
}

func supportedCoresFromRules(rules []NormalizedRule) []Core {
	if len(rules) == 0 {
		return nil
	}
	supported := map[Core]bool{}
	var walk func([]NormalizedRule, map[Core]bool)
	walk = func(items []NormalizedRule, inheritedUnsupported map[Core]bool) {
		for _, rule := range items {
			unsupported := map[Core]bool{}
			for core, value := range inheritedUnsupported {
				unsupported[core] = value
			}
			for _, core := range rule.UnsupportedCores {
				unsupported[core] = true
			}
			supported[CoreMihomo] = true
			if !unsupported[CoreSingBox] {
				supported[CoreSingBox] = true
			}
			if len(rule.Rules) > 0 {
				walk(rule.Rules, unsupported)
			}
		}
	}
	walk(rules, nil)
	result := []Core{}
	for _, core := range []Core{CoreMihomo, CoreSingBox} {
		if supported[core] {
			result = append(result, core)
		}
	}
	return result
}

func ruleUnsupportedForCore(rule NormalizedRule, core Core) bool {
	for _, unsupported := range rule.UnsupportedCores {
		if unsupported == core {
			return true
		}
	}
	return false
}

func normalizeGroupSet(input GroupSetResource, current GroupSetResource) GroupSetResource {
	now := time.Now().UTC()
	item := input
	item.Kind = KindGroupSet
	item.Name = chooseName(item.Name, current.Name, "Untitled group set")
	item.ID = item.Name
	item.OriginType = chooseOrigin(item.OriginType, current.OriginType)
	if current.CreatedAt.IsZero() {
		item.CreatedAt = now
	} else {
		item.CreatedAt = current.CreatedAt
	}
	item.UpdatedAt = now
	return item
}

func normalizeGlobalConfig(input GlobalConfig) GlobalConfig {
	input.Fields = normalizeGlobalConfigFields(input.Fields)
	input.DNSServers = normalizeGlobalDNSServers(input.DNSServers)
	input.DNSRules = normalizeGlobalDNSRules(input.DNSRules)
	input.Inbounds = normalizeManagedInbounds(input.Inbounds)
	return input
}

func normalizeGlobalConfigFields(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	fields := make(map[string]any, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		switch typed := value.(type) {
		case string:
			fields[key] = typed
		case bool:
			fields[key] = typed
		case float64:
			fields[key] = typed
		case int:
			fields[key] = typed
		case nil:
			fields[key] = ""
		default:
			fields[key] = typed
		}
	}
	return fields
}

func normalizeGlobalDNSServers(input []GlobalDNSServer) []GlobalDNSServer {
	if len(input) == 0 {
		return []GlobalDNSServer{}
	}
	servers := make([]GlobalDNSServer, 0, len(input))
	for index, server := range input {
		server.ID = strings.TrimSpace(server.ID)
		server.Name = strings.TrimSpace(server.Name)
		server.Role = strings.TrimSpace(server.Role)
		server.Protocol = strings.TrimSpace(server.Protocol)
		server.Address = strings.TrimSpace(server.Address)
		server.Port = strings.TrimSpace(server.Port)
		server.Path = strings.TrimSpace(server.Path)
		server.Detour = strings.TrimSpace(server.Detour)
		server.ClientSubnet = strings.TrimSpace(server.ClientSubnet)
		if server.ID == "" {
			server.ID = fmt.Sprintf("dns-server-%d", index+1)
		}
		if server.Role == "" {
			server.Role = "default"
		}
		if server.Protocol == "" {
			server.Protocol = "udp"
		}
		servers = append(servers, server)
	}
	return servers
}

func normalizeGlobalDNSRules(input []GlobalDNSRule) []GlobalDNSRule {
	if len(input) == 0 {
		return []GlobalDNSRule{}
	}
	rules := make([]GlobalDNSRule, 0, len(input))
	for index, rule := range input {
		rule.ID = strings.TrimSpace(rule.ID)
		rule.Matcher = strings.TrimSpace(rule.Matcher)
		rule.Value = strings.TrimSpace(rule.Value)
		rule.ServerName = strings.TrimSpace(rule.ServerName)
		rule.Strategy = strings.TrimSpace(rule.Strategy)
		rule.ClientSubnet = strings.TrimSpace(rule.ClientSubnet)
		if rule.ID == "" {
			rule.ID = fmt.Sprintf("dns-rule-%d", index+1)
		}
		if rule.Matcher == "" {
			rule.Matcher = "domain_suffix"
		}
		rules = append(rules, rule)
	}
	return rules
}

func normalizeManagedInbounds(input []ManagedInbound) []ManagedInbound {
	if len(input) == 0 {
		return []ManagedInbound{}
	}
	items := make([]ManagedInbound, 0, len(input))
	seenIDs := map[string]int{}
	for index, inbound := range input {
		inbound.ID = strings.TrimSpace(inbound.ID)
		inbound.Tag = strings.TrimSpace(inbound.Tag)
		inbound.Kind = strings.TrimSpace(inbound.Kind)
		inbound.Listen.Address = strings.TrimSpace(inbound.Listen.Address)
		inbound.Network = strings.TrimSpace(inbound.Network)
		inbound.Tun.InterfaceName = strings.TrimSpace(inbound.Tun.InterfaceName)
		inbound.Tun.Device = strings.TrimSpace(inbound.Tun.Device)
		inbound.Tun.Stack = strings.TrimSpace(inbound.Tun.Stack)
		inbound.Tun.Address = trimNonEmptyStrings(inbound.Tun.Address)
		inbound.Tun.DNSHijack = normalizeDNSHijackTargets(inbound.Tun.DNSHijack)
		inbound.Tun.RouteAddress = trimNonEmptyStrings(inbound.Tun.RouteAddress)
		inbound.Tun.RouteExcludeAddress = trimNonEmptyStrings(inbound.Tun.RouteExcludeAddress)
		inbound.Tun.RouteAddressSet = trimNonEmptyStrings(inbound.Tun.RouteAddressSet)
		inbound.Tun.RouteExcludeSet = trimNonEmptyStrings(inbound.Tun.RouteExcludeSet)
		inbound.Tun.IncludeInterface = trimNonEmptyStrings(inbound.Tun.IncludeInterface)
		inbound.Tun.ExcludeInterface = trimNonEmptyStrings(inbound.Tun.ExcludeInterface)
		if inbound.ID == "" {
			inbound.ID = fmt.Sprintf("inbound-%d", index+1)
		}
		if count := seenIDs[inbound.ID]; count > 0 {
			inbound.ID = fmt.Sprintf("%s-%d", inbound.ID, count+1)
		}
		seenIDs[inbound.ID]++
		if inbound.Kind == "" {
			inbound.Kind = "mixed"
		}
		if inbound.Tag == "" {
			inbound.Tag = fmt.Sprintf("%s-in", inbound.Kind)
		}
		items = append(items, inbound)
	}
	return items
}

func trimNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func normalizeDNSHijackTargets(values []string) []string {
	targets := trimNonEmptyStrings(values)
	for index, target := range targets {
		if target == "127.0.0.1:53" || target == "localhost:53" {
			targets[index] = "any:53"
		}
	}
	return targets
}

func chooseName(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func chooseString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func chooseOrigin(input OriginType, current OriginType) OriginType {
	if input != "" {
		return input
	}
	if current != "" {
		return current
	}
	return OriginManual
}

func chooseCore(input Core, current Core) Core {
	if input != "" {
		return input
	}
	if current != "" {
		return current
	}
	return CoreMihomo
}

func uniqueCores(values []Core) []Core {
	if len(values) == 0 {
		return nil
	}
	unique := make([]Core, 0, len(values))
	for _, value := range values {
		if value != CoreMihomo && value != CoreSingBox {
			continue
		}
		if slices.Contains(unique, value) {
			continue
		}
		unique = append(unique, value)
	}
	return unique
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || slices.Contains(unique, value) {
			continue
		}
		unique = append(unique, value)
	}
	return unique
}

func removeString(values []string, target string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func (s *Store) renameSubscriptionReferences(oldID string, newID string) error {
	profiles, err := s.listProfiles()
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		updated := false
		for index, value := range profile.SubscriptionIDs {
			if value == oldID {
				profile.SubscriptionIDs[index] = newID
				updated = true
			}
		}
		if !updated {
			continue
		}
		profile.SubscriptionIDs = uniqueStrings(profile.SubscriptionIDs)
		profile.UpdatedAt = time.Now().UTC()
		if err := s.writeProfile(profile); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeResourceName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unnamed"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	value = replacer.Replace(value)
	value = strings.TrimSpace(value)
	if value == "" {
		return "unnamed"
	}
	return value
}

func validateManagedNodeSetNames(existing []NodeSetResource, next NodeSetResource) error {
	seen := map[string]string{}
	for _, item := range existing {
		if item.ID == next.ID {
			continue
		}
		for _, node := range item.Nodes {
			name := strings.TrimSpace(node.Tag)
			if name == "" {
				continue
			}
			seen[name] = item.Name
		}
	}
	localSeen := map[string]struct{}{}
	for _, node := range next.Nodes {
		name := strings.TrimSpace(node.Tag)
		if name == "" {
			continue
		}
		if _, ok := localSeen[name]; ok {
			return fmt.Errorf("%w: %s", ErrDuplicateNodeName, name)
		}
		if owner, ok := seen[name]; ok {
			return fmt.Errorf("%w: %s (already used by %s)", ErrDuplicateNodeName, name, owner)
		}
		localSeen[name] = struct{}{}
	}
	return nil
}

func (m Metadata) GetUpdatedAt() time.Time               { return m.UpdatedAt }
func (r SubscriptionResource) GetUpdatedAt() time.Time   { return r.UpdatedAt }
func (r NodeSetResource) GetUpdatedAt() time.Time        { return r.UpdatedAt }
func (r RuleSetResource) GetUpdatedAt() time.Time        { return r.UpdatedAt }
func (r RuleSourceRepository) GetUpdatedAt() time.Time   { return r.UpdatedAt }
func (r SingBoxRuleSetResource) GetUpdatedAt() time.Time { return r.UpdatedAt }
func (r MihomoRuleProviderResource) GetUpdatedAt() time.Time {
	return r.UpdatedAt
}
func (r GroupSetResource) GetUpdatedAt() time.Time { return r.UpdatedAt }
func (r ProfileResource) GetUpdatedAt() time.Time  { return r.UpdatedAt }
