package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreInitializesNodeCacheSchema(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	for _, table := range []string{
		"node_cache_nodes",
		"node_cache_sources",
		"node_cache_tags",
		"health_check_samples",
		"profiles",
		"repository_resources",
	} {
		var name string
		err := store.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`,
			table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("query table %s error = %v", table, err)
		}
		if name != table {
			t.Fatalf("table name = %q, want %q", name, table)
		}
	}
	var indexName string
	err = store.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_node_cache_nodes_source'`,
	).Scan(&indexName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("query node source index error = %v", err)
	}
	if indexName != "idx_node_cache_nodes_source" {
		t.Fatalf("node source index = %q, want idx_node_cache_nodes_source", indexName)
	}
}

func TestRuleSetSupportedCoresDerivedFromRuleCompatibility(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	mixed, err := store.CreateRuleSet(RuleSetResource{
		Metadata:       Metadata{Name: "Mixed"},
		SupportedCores: []Core{CoreMihomo},
		Rules: []NormalizedRule{
			{
				ID:               "unsupported",
				Raw:              []string{"RULE-SET,NoMatch,Proxy"},
				Outbound:         "Proxy",
				UnsupportedCores: []Core{CoreSingBox},
			},
			{
				ID:       "youtube",
				Fields:   map[string]any{"rule_set": []string{"geosite-youtube"}},
				Outbound: "Proxy",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateRuleSet(mixed) error = %v", err)
	}
	if fmt.Sprint(mixed.SupportedCores) != "[mihomo sing-box]" {
		t.Fatalf("mixed.SupportedCores = %#v, want mihomo and sing-box", mixed.SupportedCores)
	}

	mihomoOnly, err := store.CreateRuleSet(RuleSetResource{
		Metadata: Metadata{Name: "Mihomo Only"},
		Rules: []NormalizedRule{
			{
				ID:               "unsupported",
				Raw:              []string{"RULE-SET,NoMatch,Proxy"},
				Outbound:         "Proxy",
				UnsupportedCores: []Core{CoreSingBox},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateRuleSet(mihomoOnly) error = %v", err)
	}
	if fmt.Sprint(mihomoOnly.SupportedCores) != "[mihomo]" {
		t.Fatalf("mihomoOnly.SupportedCores = %#v, want mihomo only", mihomoOnly.SupportedCores)
	}
}

func TestRuleSetPreservesRuleCardLeafSourceRule(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	created, err := store.CreateRuleSet(RuleSetResource{
		Metadata: Metadata{Name: "Remote INI"},
		RuleCards: []RoutingRuleCardUI{{
			Enabled: true,
			ID:      "card-youtube",
			Name:    "Proxy",
			Rules: []RoutingRuleLeafUI{{
				Condition: "RULE-SET",
				ID:        "rule-youtube",
				Target:    "Proxy",
				Value:     "YouTube",
				SourceRule: &NormalizedRule{
					ID:       "rule-youtube",
					Fields:   map[string]any{"rule_set": []string{"geo/geosite/youtube"}},
					Raw:      []string{"RULE-SET,YouTube,Proxy"},
					Outbound: "Proxy",
				},
			}},
		}},
		Rules: []NormalizedRule{{
			ID:       "rule-youtube",
			Fields:   map[string]any{"rule_set": []string{"geo/geosite/youtube"}},
			Raw:      []string{"RULE-SET,YouTube,Proxy"},
			Outbound: "Proxy",
		}},
	})
	if err != nil {
		t.Fatalf("CreateRuleSet() error = %v", err)
	}

	read, err := store.GetRuleSet(created.ID)
	if err != nil {
		t.Fatalf("GetRuleSet() error = %v", err)
	}
	sourceRule := read.RuleCards[0].Rules[0].SourceRule
	if sourceRule == nil {
		t.Fatalf("SourceRule was not preserved: %#v", read.RuleCards[0].Rules[0])
	}
	if fmt.Sprint(sourceRule.Fields["rule_set"]) != "[geo/geosite/youtube]" {
		t.Fatalf("source rule_set = %#v, want geo/geosite/youtube", sourceRule.Fields["rule_set"])
	}
}

func TestGlobalConfigInitializesAndPersistsDefaultDNS(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	config, err := store.GlobalConfig()
	if err != nil {
		t.Fatalf("GlobalConfig() error = %v", err)
	}
	if config.Fields["dnsListen"] != "0.0.0.0:7874" {
		t.Fatalf("dnsListen = %#v, want default listen address", config.Fields["dnsListen"])
	}
	if config.Fields["dnsMode"] != "fake-ip" || config.Fields["dnsMihomoRespectRules"] != true {
		t.Fatalf("fields = %#v, want default DNS mode and respect-rules", config.Fields)
	}
	if config.Fields["unifiedDelay"] != true || config.Fields["logLevel"] != "info" {
		t.Fatalf("fields = %#v, want default network basics", config.Fields)
	}
	if config.Fields["routeAutoDetectInterface"] != true {
		t.Fatalf("routeAutoDetectInterface = %#v, want enabled by default", config.Fields["routeAutoDetectInterface"])
	}
	if config.Fields["externalController"] != "0.0.0.0:9090" || config.Fields["secret"] != "Yi9ImtJh" {
		t.Fatalf("fields = %#v, want default controller config", config.Fields)
	}
	if config.Fields["externalUi"] != "" || config.Fields["externalUiName"] != "" || config.Fields["externalUiUrl"] != "" {
		t.Fatalf("external UI fields = %#v, want empty default UI config", config.Fields)
	}
	if config.Fields["snifferEnabled"] != true || config.Fields["snifferTlsPorts"] != "443\n8443" {
		t.Fatalf("fields = %#v, want default sniffer config", config.Fields)
	}
	if config.Fields["ntpEnabled"] != true || config.Fields["ntpInterval"] != "30" || config.Fields["ntpWriteToSystem"] != true {
		t.Fatalf("fields = %#v, want default NTP config", config.Fields)
	}
	if len(config.Inbounds) != 6 {
		t.Fatalf("len(Inbounds) = %d, want default HTTP/SOCKS/Redirect/Mixed/TProxy/Tun inbounds", len(config.Inbounds))
	}
	if config.Inbounds[0].Kind != "http" || config.Inbounds[0].Listen.Port != 7890 {
		t.Fatalf("Inbounds[0] = %#v, want default HTTP inbound", config.Inbounds[0])
	}
	if config.Inbounds[3].Kind != "mixed" || config.Inbounds[3].Listen.Port != 7893 {
		t.Fatalf("Inbounds[3] = %#v, want default mixed inbound", config.Inbounds[3])
	}
	if config.Inbounds[4].Kind != "tproxy" || config.Inbounds[4].Listen.Port != 7895 {
		t.Fatalf("Inbounds[4] = %#v, want default tproxy inbound", config.Inbounds[4])
	}
	tun := config.Inbounds[5].Tun
	if config.Inbounds[5].Kind != "tun" || tun.InterfaceName != "utun101" || tun.Stack != "system" {
		t.Fatalf("Inbounds[5] = %#v, want default tun inbound", config.Inbounds[5])
	}
	if len(tun.Address) != 1 || tun.Address[0] != "172.19.0.1/30" {
		t.Fatalf("tun.Address = %#v, want default interface address", tun.Address)
	}
	if !tun.AutoRoute || !tun.AutoDetectInterface || !tun.AutoRedirect || tun.StrictRoute {
		t.Fatalf("tun = %#v, want route flags enabled except strict route", tun)
	}
	if len(tun.DNSHijack) != 1 || tun.DNSHijack[0] != "any:53" {
		t.Fatalf("tun.DNSHijack = %#v, want DNS hijack target", tun.DNSHijack)
	}
	if config.Fields["routeBlockQuic"] != true {
		t.Fatalf("routeBlockQuic = %#v, want default QUIC block enabled", config.Fields["routeBlockQuic"])
	}
	filters, ok := config.Fields["dnsFakeIpFilters"].(string)
	if !ok || !strings.Contains(filters, "a.w.bilicdn1.com") || strings.Contains(filters, "*.argotunnel.com") {
		t.Fatalf("dnsFakeIpFilters = %#v, want default fake-ip filters", config.Fields["dnsFakeIpFilters"])
	}
	if len(config.DNSServers) != 9 {
		t.Fatalf("len(DNSServers) = %d, want default and proxy nameserver defaults", len(config.DNSServers))
	}
	if config.DNSServers[0].Role != "default" || config.DNSServers[0].Address != "223.5.5.5" {
		t.Fatalf("DNSServers[0] = %#v, want first default nameserver", config.DNSServers[0])
	}
	if config.DNSServers[2].Protocol != "tls" || config.DNSServers[2].Port != "853" {
		t.Fatalf("DNSServers[2] = %#v, want parsed TLS nameserver", config.DNSServers[2])
	}
	if config.DNSServers[6].Role != "proxy" || config.DNSServers[6].Protocol != "https" || config.DNSServers[6].Address != "8.8.8.8" || config.DNSServers[6].Path != "/dns-query" {
		t.Fatalf("DNSServers[6] = %#v, want proxy DoH nameserver defaults", config.DNSServers[6])
	}

	var persisted GlobalConfig
	if err := store.readRepositoryResource(configResourceKind, globalConfigResourceID, &persisted); err != nil {
		t.Fatalf("read persisted global config error = %v", err)
	}
	if persisted.Fields["dnsListen"] != "0.0.0.0:7874" || len(persisted.DNSServers) != 9 || len(persisted.Inbounds) != 6 {
		t.Fatalf("persisted config = %#v, want saved default DNS config", persisted)
	}
}

func TestGlobalConfigBackfillsPersistedEmptyConfigWithDefaultDNS(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := store.writeGlobalConfig(GlobalConfig{UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("write empty global config error = %v", err)
	}

	config, err := store.GlobalConfig()
	if err != nil {
		t.Fatalf("GlobalConfig() error = %v", err)
	}
	if config.Fields["dnsListen"] != "0.0.0.0:7874" || len(config.DNSServers) != 9 || len(config.Inbounds) != 6 {
		t.Fatalf("config = %#v, want backfilled default DNS config", config)
	}

	var persisted GlobalConfig
	if err := store.readRepositoryResource(configResourceKind, globalConfigResourceID, &persisted); err != nil {
		t.Fatalf("read persisted global config error = %v", err)
	}
	if persisted.Fields["dnsListen"] != "0.0.0.0:7874" || len(persisted.DNSServers) != 9 || len(persisted.Inbounds) != 6 {
		t.Fatalf("persisted config = %#v, want saved backfilled default DNS config", persisted)
	}
}

func TestGlobalConfigBackfillsMissingDefaultsWithoutOverwritingSavedValues(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := store.writeGlobalConfig(GlobalConfig{
		Fields: map[string]any{
			"logLevel":           "debug",
			"externalController": "127.0.0.1:9090",
		},
		DNSServers: []GlobalDNSServer{{
			ID:       "saved",
			Name:     "saved",
			Role:     "default",
			Protocol: "udp",
			Address:  "1.1.1.1",
			Port:     "53",
		}},
		Inbounds: []ManagedInbound{{
			ID:      "saved-in",
			Enabled: true,
			Tag:     "saved-in",
			Kind:    "mixed",
			Listen:  InboundListen{Address: "127.0.0.1", Port: 7788},
		}},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write partial global config error = %v", err)
	}

	config, err := store.GlobalConfig()
	if err != nil {
		t.Fatalf("GlobalConfig() error = %v", err)
	}
	if config.Fields["logLevel"] != "debug" || config.Fields["externalController"] != "127.0.0.1:9090" {
		t.Fatalf("fields = %#v, want saved values preserved", config.Fields)
	}
	if config.Fields["unifiedDelay"] != true || config.Fields["snifferQuicPorts"] != "443" || config.Fields["routeAutoDetectInterface"] != true {
		t.Fatalf("fields = %#v, want missing defaults backfilled", config.Fields)
	}
	if len(config.DNSServers) != 1 || config.DNSServers[0].Address != "1.1.1.1" {
		t.Fatalf("DNSServers = %#v, want saved DNS servers preserved", config.DNSServers)
	}
	if len(config.Inbounds) != 1 || config.Inbounds[0].Listen.Port != 7788 {
		t.Fatalf("Inbounds = %#v, want saved inbounds preserved", config.Inbounds)
	}
}

func TestGlobalConfigMigratesLegacyProxyDNSDefaults(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	legacyServers := make([]GlobalDNSServer, 0, len(defaultDNSNameservers)*2)
	for _, role := range []string{"default", "proxy"} {
		for index, endpoint := range defaultDNSNameservers {
			protocol, address, port, path := parseDefaultDNSServerEndpoint(endpoint)
			legacyServers = append(legacyServers, GlobalDNSServer{
				ID:       fmt.Sprintf("dns-%s-%d", role, index+1),
				Name:     fmt.Sprintf("%s-%d", role, index+1),
				Role:     role,
				Protocol: protocol,
				Address:  address,
				Port:     port,
				Path:     path,
			})
		}
	}
	if err := store.writeGlobalConfig(GlobalConfig{
		Fields:     defaultGlobalConfigFields(),
		DNSServers: legacyServers,
		Inbounds:   defaultGlobalInbounds(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write legacy global config error = %v", err)
	}

	config, err := store.GlobalConfig()
	if err != nil {
		t.Fatalf("GlobalConfig() error = %v", err)
	}
	if len(config.DNSServers) != 9 {
		t.Fatalf("len(DNSServers) = %d, want migrated default DNS servers", len(config.DNSServers))
	}
	if config.DNSServers[6].Role != "proxy" || config.DNSServers[6].Protocol != "https" || config.DNSServers[6].Address != "8.8.8.8" {
		t.Fatalf("DNSServers[6] = %#v, want migrated proxy DoH server", config.DNSServers[6])
	}

	var persisted GlobalConfig
	if err := store.readRepositoryResource(configResourceKind, globalConfigResourceID, &persisted); err != nil {
		t.Fatalf("read persisted global config error = %v", err)
	}
	if len(persisted.DNSServers) != 9 || persisted.DNSServers[6].Address != "8.8.8.8" {
		t.Fatalf("persisted DNSServers = %#v, want migrated proxy DNS persisted", persisted.DNSServers)
	}
}

func TestStoreUpsertsNodeCacheWithSourcesAndDeduplication(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	first := NormalizedNode{
		ID:         "source-node-1",
		Tag:        "Tokyo",
		Type:       "vmess",
		Server:     "tokyo.example.com",
		ServerPort: 443,
		Source:     "Airport A",
	}
	if err := store.UpsertNodeCache(NodeCacheUpsert{
		SourceType:     NodeCacheSourceSubscription,
		SourceID:       "sub-1",
		SubscriptionID: "sub-1",
		NodeSetID:      "Airport A",
		Nodes:          []NormalizedNode{first},
	}); err != nil {
		t.Fatalf("UpsertNodeCache(first) error = %v", err)
	}
	second := first
	second.ID = "source-node-2"
	second.Source = "Airport B"
	if err := store.UpsertNodeCache(NodeCacheUpsert{
		SourceType:     NodeCacheSourceSubscription,
		SourceID:       "sub-1",
		SubscriptionID: "sub-1",
		NodeSetID:      "Airport B",
		Nodes:          []NormalizedNode{second},
	}); err != nil {
		t.Fatalf("UpsertNodeCache(second) error = %v", err)
	}

	var nodeCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM node_cache_nodes`).Scan(&nodeCount); err != nil {
		t.Fatalf("count nodes error = %v", err)
	}
	if nodeCount != 1 {
		t.Fatalf("node count = %d, want deduplicated single row", nodeCount)
	}
	var source, nodeSetID string
	if err := store.db.QueryRow(`
		SELECT n.source, s.node_set_id
		FROM node_cache_nodes n
		JOIN node_cache_sources s ON s.node_id = n.node_id
		WHERE s.source_type = 'subscription' AND s.source_id = 'sub-1'
	`).Scan(&source, &nodeSetID); err != nil {
		t.Fatalf("query node source error = %v", err)
	}
	if source != "Airport B" || nodeSetID != "Airport B" {
		t.Fatalf("source = %q, nodeSetID = %q, want Airport B", source, nodeSetID)
	}
	var tagCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM node_cache_tags WHERE tag = 'Tokyo'`).Scan(&tagCount); err != nil {
		t.Fatalf("count tags error = %v", err)
	}
	if tagCount != 1 {
		t.Fatalf("tag count = %d, want 1", tagCount)
	}
	page, err := store.QueryNodeCache(NodeCacheQuery{
		Limit:          1,
		SubscriptionID: "sub-1",
		Protocol:       "vmess",
		Query:          "tokyo",
	})
	if err != nil {
		t.Fatalf("QueryNodeCache() error = %v", err)
	}
	if len(page.Nodes) != 1 || page.Total != 1 || page.Nodes[0].Tag != "Tokyo" {
		t.Fatalf("page = %#v, want Tokyo node", page)
	}
	empty, err := store.QueryNodeCache(NodeCacheQuery{
		Tag: "Osaka",
	})
	if err != nil {
		t.Fatalf("QueryNodeCache(empty) error = %v", err)
	}
	if len(empty.Nodes) != 0 || empty.Total != 0 {
		t.Fatalf("empty page = %#v, want no nodes", empty)
	}
}

func TestDatabaseResetClearsNodeCacheAndRepositoryResources(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	nodeSet, err := store.CreateNodeSet(NodeSetResource{
		Metadata: Metadata{
			Name:       "Manual nodes",
			OriginType: OriginPlainNode,
		},
		Nodes: []NormalizedNode{{
			Tag:        "Seoul",
			Type:       "trojan",
			Server:     "seoul.example.com",
			ServerPort: 443,
		}},
	})
	if err != nil {
		t.Fatalf("CreateNodeSet() error = %v", err)
	}
	if err := store.UpsertNodeCache(NodeCacheUpsert{
		SourceType: NodeCacheSourceImport,
		SourceID:   nodeSet.ID,
		NodeSetID:  nodeSet.ID,
		Nodes:      nodeSet.Nodes,
	}); err != nil {
		t.Fatalf("UpsertNodeCache() error = %v", err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := resetSQLiteFiles(filepath.Join(root, "repository", "rule-source-indexes", "rule-source-indexes.sqlite")); err != nil {
		t.Fatalf("resetSQLiteFiles() error = %v", err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore(reopened) error = %v", err)
	}
	empty, err := reopened.QueryNodeCache(NodeCacheQuery{Limit: 10})
	if err != nil {
		t.Fatalf("QueryNodeCache(empty) error = %v", err)
	}
	if empty.Total != 0 {
		t.Fatalf("empty.Total = %d, want reset cache", empty.Total)
	}
	if _, err := reopened.GetNodeSet(nodeSet.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetNodeSet() after sqlite reset error = %v, want ErrNotFound", err)
	}
}

func TestNodeCacheQueryIsBounded(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	nodes := []NormalizedNode{
		{Tag: "A", Type: "vmess", Server: "a.example.com", ServerPort: 443},
		{Tag: "B", Type: "vmess", Server: "b.example.com", ServerPort: 443},
		{Tag: "C", Type: "vmess", Server: "c.example.com", ServerPort: 443},
	}
	if err := store.UpsertNodeCache(NodeCacheUpsert{
		SourceType: NodeCacheSourceImport,
		SourceID:   "import-1",
		NodeSetID:  "import-1",
		Nodes:      nodes,
	}); err != nil {
		t.Fatalf("UpsertNodeCache() error = %v", err)
	}
	page, err := store.QueryNodeCache(NodeCacheQuery{
		Protocol: "vmess",
		Limit:    2,
	})
	if err != nil {
		t.Fatalf("QueryNodeCache() error = %v", err)
	}
	if len(page.Nodes) != 2 || page.Total != 3 || page.NextOffset != 2 || !page.HasMore {
		t.Fatalf("page = %#v, want bounded first page", page)
	}
}

func TestStoreRecordsHealthCheckSamples(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	success, err := store.RecordHealthCheckSample(HealthCheckSample{
		NodeID:    "node-1",
		CheckType: "tcp",
		LatencyMS: 23,
		Success:   true,
	})
	if err != nil {
		t.Fatalf("RecordHealthCheckSample(success) error = %v", err)
	}
	failure, err := store.RecordHealthCheckSample(HealthCheckSample{
		NodeID:       "node-1",
		CheckType:    "tcp",
		Success:      false,
		ErrorSummary: "connection refused",
	})
	if err != nil {
		t.Fatalf("RecordHealthCheckSample(failure) error = %v", err)
	}
	if success.ID == 0 || failure.ID == 0 || !success.Success || failure.Success {
		t.Fatalf("samples = %#v %#v, want persisted success and failure", success, failure)
	}
	var total int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM health_check_samples WHERE node_id = 'node-1'`).Scan(&total); err != nil {
		t.Fatalf("count health samples error = %v", err)
	}
	if total != 2 {
		t.Fatalf("health sample count = %d, want 2", total)
	}
}

func TestHealthHistoryQueriesLatestStateAndTrend(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	base := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	samples := []HealthCheckSample{
		{NodeID: "node-1", CheckType: "tcp", LatencyMS: 30, Success: true, CheckedAt: base},
		{NodeID: "node-1", CheckType: "tcp", Success: false, ErrorSummary: "timeout", CheckedAt: base.Add(time.Minute)},
		{NodeID: "node-2", CheckType: "tcp", LatencyMS: 12, Success: true, CheckedAt: base.Add(2 * time.Minute)},
	}
	for _, sample := range samples {
		if _, err := store.RecordHealthCheckSample(sample); err != nil {
			t.Fatalf("RecordHealthCheckSample() error = %v", err)
		}
	}
	latest, err := store.LatestHealthCheckSamples(HealthCheckQuery{CheckType: "tcp"})
	if err != nil {
		t.Fatalf("LatestHealthCheckSamples() error = %v", err)
	}
	if len(latest) != 2 || latest[0].NodeID != "node-2" || latest[1].NodeID != "node-1" || latest[1].Success {
		t.Fatalf("latest = %#v, want node-2 success then node-1 failure", latest)
	}
	trend, err := store.HealthCheckTrend(HealthCheckQuery{NodeID: "node-1", CheckType: "tcp", Limit: 10})
	if err != nil {
		t.Fatalf("HealthCheckTrend() error = %v", err)
	}
	if trend.Total != 2 || trend.SuccessCount != 1 || trend.FailureCount != 1 || trend.AverageLatencyMS != 30 {
		t.Fatalf("trend = %#v, want one success, one failure, average latency 30", trend)
	}
	if len(trend.Samples) != 2 || trend.Samples[0].ErrorSummary != "timeout" {
		t.Fatalf("trend samples = %#v, want newest failure summary", trend.Samples)
	}
}

func TestHealthHistoryRetentionCleanup(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	base := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 4; index++ {
		if _, err := store.RecordHealthCheckSample(HealthCheckSample{
			NodeID:    "node-1",
			CheckType: "tcp",
			LatencyMS: 10 + index,
			Success:   true,
			CheckedAt: base.Add(time.Duration(index) * time.Minute),
		}); err != nil {
			t.Fatalf("RecordHealthCheckSample(%d) error = %v", index, err)
		}
	}
	deleted, err := store.CleanupHealthHistory(HealthHistoryRetention{
		Before: base.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("CleanupHealthHistory(before) error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted before = %d, want 1", deleted)
	}
	deleted, err = store.CleanupHealthHistory(HealthHistoryRetention{
		MaxPerNode: 2,
	})
	if err != nil {
		t.Fatalf("CleanupHealthHistory(max) error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted max = %d, want 1", deleted)
	}
	trend, err := store.HealthCheckTrend(HealthCheckQuery{NodeID: "node-1", Limit: 10})
	if err != nil {
		t.Fatalf("HealthCheckTrend() error = %v", err)
	}
	if trend.Total != 2 || trend.Samples[0].LatencyMS != 13 || trend.Samples[1].LatencyMS != 12 {
		t.Fatalf("remaining trend = %#v, want latest two samples", trend)
	}
}

func TestOperationEventsAreQueryableAndContextIsSafe(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	created, err := store.RecordOperationEvent(OperationEvent{
		Severity:     "error",
		EventType:    "repository.refresh.failed",
		ResourceType: string(KindRuleSourceRepo),
		ResourceID:   "repo-1",
		Core:         CoreSingBox,
		Message:      "Refresh failed",
		ErrorCode:    "refresh_failed",
		Context: map[string]any{
			"token": "secret-token",
			"note":  strings.Repeat("x", maxOperationEventContextBytes*2),
		},
	})
	if err != nil {
		t.Fatalf("RecordOperationEvent() error = %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("created.ID = 0, want persisted event")
	}
	page, err := store.QueryOperationEvents(OperationEventQuery{
		Severity:     "error",
		EventType:    "repository.refresh.failed",
		ResourceType: string(KindRuleSourceRepo),
		ResourceID:   "repo-1",
		Core:         CoreSingBox,
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("QueryOperationEvents() error = %v", err)
	}
	if page.Total != 1 || len(page.Events) != 1 {
		t.Fatalf("page = %#v, want one event", page)
	}
	event := page.Events[0]
	if event.Context["truncated"] != true {
		t.Fatalf("context = %#v, want truncated context", event.Context)
	}
	if data, err := json.Marshal(event.Context); err != nil || strings.Contains(string(data), "secret-token") {
		t.Fatalf("context data = %s, err = %v, want secret redacted", data, err)
	}
	empty, err := store.QueryOperationEvents(OperationEventQuery{Severity: "info", Limit: 10})
	if err != nil {
		t.Fatalf("QueryOperationEvents(empty) error = %v", err)
	}
	if empty.Total != 0 || len(empty.Events) != 0 {
		t.Fatalf("empty = %#v, want no info events", empty)
	}
}

func TestStorePersistsResourcesAndGlobalConfig(t *testing.T) {
	root := t.TempDir()

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	subscription, err := store.CreateSubscription(SubscriptionResource{
		Metadata: Metadata{
			Name:       "Main subscription",
			OriginType: OriginClashSubscription,
		},
		SourceURL: "https://example.com/subscription.yaml",
	})
	if err != nil {
		t.Fatalf("CreateSubscription() error = %v", err)
	}

	nodeSet, err := store.CreateNodeSet(NodeSetResource{
		Metadata: Metadata{
			Name:       "Manual nodes",
			OriginType: OriginPlainNode,
		},
		Nodes: []NormalizedNode{{
			ID:         "node-1",
			Tag:        "Tokyo",
			Type:       "vmess",
			Server:     "tokyo.example.com",
			ServerPort: 443,
		}},
	})
	if err != nil {
		t.Fatalf("CreateNodeSet() error = %v", err)
	}

	profile, err := store.CreateProfile(ProfileResource{
		Metadata: Metadata{
			Name: "Daily",
		},
		SelectedCore:    CoreSingBox,
		SubscriptionIDs: []string{subscription.ID},
		NodeSetIDs:      []string{nodeSet.ID},
	})
	if err != nil {
		t.Fatalf("CreateProfile() error = %v", err)
	}

	config, err := store.GlobalConfig()
	if err != nil {
		t.Fatalf("GlobalConfig() error = %v", err)
	}
	config.Fields["selectedCore"] = string(CoreSingBox)
	config.Fields["routingRuleSetIds"] = []string{"selected-rules"}
	if _, err := store.UpdateGlobalConfig(config); err != nil {
		t.Fatalf("UpdateGlobalConfig() error = %v", err)
	}

	reloaded, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore(reload) error = %v", err)
	}

	bootstrap, err := reloaded.Bootstrap()
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if len(bootstrap.Subscriptions) != 1 {
		t.Fatalf("len(Subscriptions) = %d, want 1", len(bootstrap.Subscriptions))
	}
	if subscription.ID != "Main subscription" {
		t.Fatalf("subscription.ID = %q, want %q", subscription.ID, "Main subscription")
	}
	if len(bootstrap.NodeSets) != 1 {
		t.Fatalf("len(NodeSets) = %d, want 1", len(bootstrap.NodeSets))
	}
	if nodeSet.ID != "Manual nodes" {
		t.Fatalf("nodeSet.ID = %q, want %q", nodeSet.ID, "Manual nodes")
	}
	if len(bootstrap.Profiles) != 1 {
		t.Fatalf("len(Profiles) = %d, want 1", len(bootstrap.Profiles))
	}
	if bootstrap.Profiles[0].ID != profile.ID || bootstrap.Profiles[0].Name != "Daily" {
		t.Fatalf("bootstrap.Profiles[0] = %#v, want reloaded sqlite profile", bootstrap.Profiles[0])
	}
	if bootstrap.Config.Fields["dnsListen"] != "0.0.0.0:7874" || len(bootstrap.Config.DNSServers) != 9 {
		t.Fatalf("bootstrap.Config = %#v, want default DNS global config", bootstrap.Config)
	}
	if bootstrap.Config.Fields["selectedCore"] != string(CoreSingBox) {
		t.Fatalf("bootstrap.Config.Fields[selectedCore] = %#v, want %q", bootstrap.Config.Fields["selectedCore"], CoreSingBox)
	}
	ruleSetIDs, ok := bootstrap.Config.Fields["routingRuleSetIds"].([]any)
	if !ok || len(ruleSetIDs) != 1 || ruleSetIDs[0] != "selected-rules" {
		t.Fatalf("bootstrap.Config.Fields[routingRuleSetIds] = %#v, want selected-rules", bootstrap.Config.Fields["routingRuleSetIds"])
	}
	if _, err := os.Stat(filepath.Join(root, "repository", "profiles")); !os.IsNotExist(err) {
		t.Fatalf("profile JSON directory should not be created, stat error = %v", err)
	}
	for _, dir := range []string{
		"subscriptions",
		"node-sets",
		"routing-rule-sets",
		"rule-source-repositories",
		"sing-box-rule-sets",
		"mihomo-rule-providers",
		"group-sets",
	} {
		if _, err := os.Stat(filepath.Join(root, "repository", dir)); !os.IsNotExist(err) {
			t.Fatalf("repository file directory %q should not be created, stat error = %v", dir, err)
		}
	}
	var profileCount int
	if err := reloaded.db.QueryRow(`SELECT COUNT(*) FROM profiles`).Scan(&profileCount); err != nil {
		t.Fatalf("query sqlite profiles count error = %v", err)
	}
	if profileCount != 1 {
		t.Fatalf("sqlite profile count = %d, want 1", profileCount)
	}
	for kind, want := range map[string]int{
		string(KindSubscription): 1,
		string(KindNodeSet):      1,
		configResourceKind:       1,
	} {
		var count int
		if err := reloaded.db.QueryRow(`
			SELECT COUNT(*)
			FROM repository_resources
			WHERE resource_kind = ?
		`, kind).Scan(&count); err != nil {
			t.Fatalf("query repository resource count for %s error = %v", kind, err)
		}
		if count != want {
			t.Fatalf("repository resource count for %s = %d, want %d", kind, count, want)
		}
	}
}

func TestUpdateSubscriptionClearsPreviousSyncErrorAfterSuccessfulSync(t *testing.T) {
	root := t.TempDir()

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	subscription, err := store.CreateSubscription(SubscriptionResource{
		Metadata: Metadata{
			Name:       "Main subscription",
			OriginType: OriginClashSubscription,
		},
		Sync: SubscriptionSyncStatus{
			LastSyncedAt:  time.Date(2026, 6, 6, 1, 0, 0, 0, time.UTC),
			LastSyncError: "network timeout",
		},
	})
	if err != nil {
		t.Fatalf("CreateSubscription() error = %v", err)
	}

	updated, err := store.UpdateSubscription(subscription.ID, SubscriptionResource{
		Metadata: Metadata{
			Name:       subscription.Name,
			OriginType: subscription.OriginType,
		},
		Sync: SubscriptionSyncStatus{
			LastSyncedAt: time.Date(2026, 6, 6, 2, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("UpdateSubscription() error = %v", err)
	}

	if updated.Sync.LastSyncError != "" {
		t.Fatalf("updated.Sync.LastSyncError = %q, want empty", updated.Sync.LastSyncError)
	}
}

func TestListNodeSetFilesReadsAllNodeSetResources(t *testing.T) {
	root := t.TempDir()

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	first, err := store.CreateNodeSet(NodeSetResource{
		Metadata: Metadata{
			Name:       "Alpha",
			OriginType: OriginPlainNode,
		},
		Nodes: []NormalizedNode{{
			ID:         "node-1",
			Tag:        "Alpha Node",
			Type:       "vmess",
			Server:     "alpha.example.com",
			ServerPort: 443,
		}},
	})
	if err != nil {
		t.Fatalf("CreateNodeSet(first) error = %v", err)
	}

	second, err := store.CreateNodeSet(NodeSetResource{
		Metadata: Metadata{
			Name:       "Beta",
			OriginType: OriginPlainNode,
		},
		Nodes: []NormalizedNode{{
			ID:         "node-2",
			Tag:        "Beta Node",
			Type:       "vless",
			Server:     "beta.example.com",
			ServerPort: 8443,
		}},
	})
	if err != nil {
		t.Fatalf("CreateNodeSet(second) error = %v", err)
	}

	files, err := store.ListNodeSetFiles()
	if err != nil {
		t.Fatalf("ListNodeSetFiles() error = %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}

	got := map[string]NodeSetResource{}
	for _, item := range files {
		got[item.FileName] = item.NodeSet
	}

	if got["Alpha"].ID != first.ID {
		t.Fatalf("Alpha ID = %q, want %q", got["Alpha"].ID, first.ID)
	}
	if got["Beta"].ID != second.ID {
		t.Fatalf("Beta ID = %q, want %q", got["Beta"].ID, second.ID)
	}
	if got["Alpha"].Nodes[0].Tag != "Alpha Node" {
		t.Fatalf("Alpha first node tag = %q, want Alpha Node", got["Alpha"].Nodes[0].Tag)
	}
	if got["Beta"].Nodes[0].Tag != "Beta Node" {
		t.Fatalf("Beta first node tag = %q, want Beta Node", got["Beta"].Nodes[0].Tag)
	}
}

func TestManagedNodeSetsPersistAsRepositoryResources(t *testing.T) {
	root := t.TempDir()

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	subscriptionNodes, err := store.UpsertManagedNodeSet(NodeSetResource{
		Metadata: Metadata{
			Name:       "Airport A",
			OriginType: OriginClashSubscription,
		},
		Nodes: []NormalizedNode{{
			ID:         "node-1",
			Tag:        "Tokyo-A",
			Type:       "vmess",
			Server:     "tokyo-a.example.com",
			ServerPort: 443,
			Source:     "Airport A",
		}},
	})
	if err != nil {
		t.Fatalf("UpsertManagedNodeSet(subscription) error = %v", err)
	}

	manualNode, err := store.UpsertManagedNodeSet(NodeSetResource{
		Metadata: Metadata{
			Name:       "Manual Seoul",
			OriginType: OriginManual,
		},
		Nodes: []NormalizedNode{{
			ID:         "node-2",
			Tag:        "Seoul-1",
			Type:       "trojan",
			Server:     "seoul.example.com",
			ServerPort: 443,
			Source:     "Manual Seoul",
		}},
	})
	if err != nil {
		t.Fatalf("UpsertManagedNodeSet(manual) error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "repository", "node-sets")); !os.IsNotExist(err) {
		t.Fatalf("node set file directory should not be created, got err=%v", err)
	}

	bootstrap, err := store.Bootstrap()
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if len(bootstrap.NodeSets) != 2 {
		t.Fatalf("len(bootstrap.NodeSets) = %d, want 2", len(bootstrap.NodeSets))
	}

	gotSubscription, err := store.GetNodeSet("Airport A")
	if err != nil {
		t.Fatalf("GetNodeSet(subscription) error = %v", err)
	}
	if gotSubscription.Nodes[0].Tag != subscriptionNodes.Nodes[0].Tag {
		t.Fatalf("subscription node tag = %q, want %q", gotSubscription.Nodes[0].Tag, subscriptionNodes.Nodes[0].Tag)
	}

	gotManual, err := store.GetNodeSet("Manual Seoul")
	if err != nil {
		t.Fatalf("GetNodeSet(manual) error = %v", err)
	}
	if gotManual.Nodes[0].Tag != manualNode.Nodes[0].Tag {
		t.Fatalf("manual node tag = %q, want %q", gotManual.Nodes[0].Tag, manualNode.Nodes[0].Tag)
	}

	files, err := store.ListNodeSetFiles()
	if err != nil {
		t.Fatalf("ListNodeSetFiles() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	got := map[string]NodeSetResource{}
	for _, item := range files {
		got[item.FileName] = item.NodeSet
	}
	if got["Airport A"].Nodes[0].Tag != subscriptionNodes.Nodes[0].Tag {
		t.Fatalf("Airport A node tag = %q, want %q", got["Airport A"].Nodes[0].Tag, subscriptionNodes.Nodes[0].Tag)
	}
	if got["Manual Seoul"].Nodes[0].Tag != manualNode.Nodes[0].Tag {
		t.Fatalf("Manual Seoul node tag = %q, want %q", got["Manual Seoul"].Nodes[0].Tag, manualNode.Nodes[0].Tag)
	}
}

func TestManagedNodeSetsRejectDuplicateNodeNames(t *testing.T) {
	root := t.TempDir()

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if _, err := store.UpsertManagedNodeSet(NodeSetResource{
		Metadata: Metadata{
			Name:       "Subscription A",
			OriginType: OriginClashSubscription,
		},
		Nodes: []NormalizedNode{{
			ID:   "node-1",
			Tag:  "Shared Node",
			Type: "vmess",
		}},
	}); err != nil {
		t.Fatalf("seed UpsertManagedNodeSet() error = %v", err)
	}

	_, err = store.UpsertManagedNodeSet(NodeSetResource{
		Metadata: Metadata{
			Name:       "Manual Node",
			OriginType: OriginManual,
		},
		Nodes: []NormalizedNode{{
			ID:   "node-2",
			Tag:  "Shared Node",
			Type: "trojan",
		}},
	})
	if !errors.Is(err, ErrDuplicateNodeName) {
		t.Fatalf("duplicate UpsertManagedNodeSet() error = %v, want ErrDuplicateNodeName", err)
	}
}
