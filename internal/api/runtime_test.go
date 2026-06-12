package api

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

func TestCompileRuntimeUsesAllNodeAndGroupSets(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if _, err := store.CreateNodeSet(repository.NodeSetResource{
		Metadata: repository.Metadata{Name: "selected"},
		Nodes: []repository.NormalizedNode{
			{Tag: "Selected Node", Type: "http", Server: "selected.example", ServerPort: 8080},
		},
	}); err != nil {
		t.Fatalf("CreateNodeSet(selected) error = %v", err)
	}
	if _, err := store.CreateNodeSet(repository.NodeSetResource{
		Metadata: repository.Metadata{Name: "手动添加"},
		Nodes: []repository.NormalizedNode{
			{Tag: "Manual Node", Type: "http", Server: "manual.example", ServerPort: 8081},
		},
	}); err != nil {
		t.Fatalf("CreateNodeSet(manual) error = %v", err)
	}
	if _, err := store.CreateGroupSet(repository.GroupSetResource{
		Metadata: repository.Metadata{Name: "manual groups"},
		Groups: []repository.NormalizedGroup{
			{Tag: "Manual Group", Type: "select", Outbounds: []string{"Manual Node"}},
		},
	}); err != nil {
		t.Fatalf("CreateGroupSet() error = %v", err)
	}
	config, err := store.GlobalConfig()
	if err != nil {
		t.Fatalf("GlobalConfig() error = %v", err)
	}
	config.Fields["selectedCore"] = string(repository.CoreMihomo)
	config.Fields["routingRuleSetIds"] = []string{"selected-rules"}
	if _, err := store.UpdateGlobalConfig(config); err != nil {
		t.Fatalf("UpdateGlobalConfig() error = %v", err)
	}

	compiled, err := (&Server{store: store}).compileRuntime(context.Background(), runtimeSelection{
		SelectedCore: repository.CoreMihomo,
		RuleSetIDs:   []string{"selected-rules"},
	}, "")
	if err != nil {
		t.Fatalf("compileRuntime() error = %v", err)
	}
	output := string(compiled.Data)
	for _, expected := range []string{
		"name: Selected Node",
		"name: Manual Node",
		"name: Manual Group",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("compiled runtime missing %q:\n%s", expected, output)
		}
	}
}

func TestCompileRuntimeUsesResourcesMatchingSelectedRuleSet(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	selectedRuleSet, err := store.CreateRuleSet(repository.RuleSetResource{
		Metadata: repository.Metadata{Name: "Daily"},
		Rules: []repository.NormalizedRule{
			{Raw: []string{"MATCH,🚀 节点选择"}, Outbound: "🚀 节点选择"},
		},
	})
	if err != nil {
		t.Fatalf("CreateRuleSet(selected) error = %v", err)
	}
	if _, err := store.CreateRuleSet(repository.RuleSetResource{
		Metadata: repository.Metadata{Name: "Other"},
		Rules: []repository.NormalizedRule{
			{Raw: []string{"MATCH,Other"}, Outbound: "Other"},
		},
	}); err != nil {
		t.Fatalf("CreateRuleSet(other) error = %v", err)
	}
	if _, err := store.CreateNodeSet(repository.NodeSetResource{
		Metadata: repository.Metadata{Name: "Daily"},
		Nodes: []repository.NormalizedNode{
			{Tag: "Daily Node", Type: "http", Server: "daily.example", ServerPort: 8080},
		},
	}); err != nil {
		t.Fatalf("CreateNodeSet(selected) error = %v", err)
	}
	if _, err := store.CreateNodeSet(repository.NodeSetResource{
		Metadata: repository.Metadata{Name: "Other"},
		Nodes: []repository.NormalizedNode{
			{Tag: "Other Node", Type: "http", Server: "other.example", ServerPort: 8080},
		},
	}); err != nil {
		t.Fatalf("CreateNodeSet(other) error = %v", err)
	}
	if _, err := store.CreateGroupSet(repository.GroupSetResource{
		Metadata: repository.Metadata{Name: "Daily"},
		Groups: []repository.NormalizedGroup{
			{Tag: "🚀 节点选择", Type: "select", Outbounds: []string{"Daily Node"}},
		},
	}); err != nil {
		t.Fatalf("CreateGroupSet(selected) error = %v", err)
	}
	if _, err := store.CreateGroupSet(repository.GroupSetResource{
		Metadata: repository.Metadata{Name: "Other"},
		Groups: []repository.NormalizedGroup{
			{Tag: "🚀 节点选择", Type: "select", Outbounds: []string{"Other Node"}},
		},
	}); err != nil {
		t.Fatalf("CreateGroupSet(other) error = %v", err)
	}

	compiled, err := (&Server{store: store}).compileRuntime(context.Background(), runtimeSelection{
		SelectedCore: repository.CoreMihomo,
		RuleSetIDs:   []string{selectedRuleSet.ID},
	}, "")
	if err != nil {
		t.Fatalf("compileRuntime() error = %v", err)
	}
	output := string(compiled.Data)
	if count := strings.Count(output, "type: select"); count != 1 {
		t.Fatalf("compiled runtime group count = %d, want 1:\n%s", count, output)
	}
	if !strings.Contains(output, "name: Daily Node") {
		t.Fatalf("compiled runtime missing selected node:\n%s", output)
	}
	if strings.Contains(output, "name: Other Node") {
		t.Fatalf("compiled runtime included unrelated node:\n%s", output)
	}
}

func TestCompileRuntimeIncludesManualNodesReferencedBySelectedGroups(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	selectedRuleSet, err := store.CreateRuleSet(repository.RuleSetResource{
		Metadata: repository.Metadata{Name: "Daily"},
		Rules: []repository.NormalizedRule{
			{Raw: []string{"MATCH,OpenAI"}, Outbound: "OpenAI"},
		},
	})
	if err != nil {
		t.Fatalf("CreateRuleSet(selected) error = %v", err)
	}
	if _, err := store.CreateNodeSet(repository.NodeSetResource{
		Metadata: repository.Metadata{Name: "Daily"},
		Nodes: []repository.NormalizedNode{
			{Tag: "Daily Node", Type: "http", Server: "daily.example", ServerPort: 8080},
		},
	}); err != nil {
		t.Fatalf("CreateNodeSet(selected) error = %v", err)
	}
	if _, err := store.CreateNodeSet(repository.NodeSetResource{
		Metadata: repository.Metadata{Name: "手动添加"},
		Nodes: []repository.NormalizedNode{
			{Tag: "CN2GIA-ycmfs7laka", Type: "http", Server: "manual.example", ServerPort: 8080},
		},
	}); err != nil {
		t.Fatalf("CreateNodeSet(manual) error = %v", err)
	}
	if _, err := store.CreateGroupSet(repository.GroupSetResource{
		Metadata: repository.Metadata{Name: "Daily"},
		Groups: []repository.NormalizedGroup{
			{Tag: "OpenAI", Type: "select", Outbounds: []string{"CN2GIA-ycmfs7laka"}},
		},
	}); err != nil {
		t.Fatalf("CreateGroupSet(selected) error = %v", err)
	}

	compiled, err := (&Server{store: store}).compileRuntime(context.Background(), runtimeSelection{
		SelectedCore: repository.CoreMihomo,
		RuleSetIDs:   []string{selectedRuleSet.ID},
	}, "")
	if err != nil {
		t.Fatalf("compileRuntime() error = %v", err)
	}
	output := string(compiled.Data)
	if !strings.Contains(output, "name: CN2GIA-ycmfs7laka") {
		t.Fatalf("compiled runtime missing referenced manual node:\n%s", output)
	}
}

func TestMihomoRuntimeConfigMatchesStandardDefaultShape(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	config, err := store.GlobalConfig()
	if err != nil {
		t.Fatalf("GlobalConfig() error = %v", err)
	}

	runtimeConfig := mihomoRuntimeConfig(config, nil, nil, nil, nil, nil, "0.0.0.0:9090")
	data, err := marshalMihomoRuntimeConfig(runtimeConfig)
	if err != nil {
		t.Fatalf("marshalMihomoRuntimeConfig() error = %v", err)
	}
	output := string(data)

	for _, expected := range []string{
		"clash-for-android:",
		"    append-system-dns: false",
		"experimental:",
		"    sniff-tls-sni: true",
		"geox-url: {}",
		"    write-to-system: true",
		"    store-selected: true",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("mihomo runtime output missing %q:\n%s", expected, output)
		}
	}

	for _, unexpected := range []string{
		"tcp-concurrent:",
		"find-process-mode:",
		"etag-support:",
		"disable-keep-alive:",
		"geodata-mode:",
		"geo-auto-update:",
		"external-controller-cors:",
		"external-ui: /usr/share/openclash/ui",
		"external-ui-name:",
		"external-ui-url:",
		"cache-algorithm:",
		"use-system-hosts:",
		"prefer-h3:",
		"fallback-filter:",
		"store-fake-ip:",
	} {
		if strings.Contains(output, unexpected) {
			t.Fatalf("mihomo runtime output contains %q:\n%s", unexpected, output)
		}
	}
}

func TestMihomoRuntimeConfigUsesNTPPortField(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	config, err := store.GlobalConfig()
	if err != nil {
		t.Fatalf("GlobalConfig() error = %v", err)
	}

	data, err := marshalMihomoRuntimeConfig(mihomoRuntimeConfig(config, nil, nil, nil, nil, nil, "0.0.0.0:9090"))
	if err != nil {
		t.Fatalf("marshalMihomoRuntimeConfig() error = %v", err)
	}
	output := string(data)

	if !strings.Contains(output, "    port: 123") {
		t.Fatalf("mihomo runtime output missing NTP port field:\n%s", output)
	}
	if strings.Contains(output, "server-port:") {
		t.Fatalf("mihomo runtime output contains legacy NTP server-port field:\n%s", output)
	}
}

func TestMihomoRuntimeConfigAddsReferencedBuiltInRuleProviders(t *testing.T) {
	config := mihomoRuntimeConfig(repository.GlobalConfig{}, nil, nil, []repository.NormalizedRule{
		{
			Fields:   map[string]any{"rule_set": []string{"geo-lite/geosite/google"}},
			Raw:      []string{"RULE-SET,geo-lite/geosite/google,Google"},
			Outbound: "Google",
		},
	}, nil, nil, "0.0.0.0:9090")

	providers, ok := config["rule-providers"].(map[string]any)
	if !ok {
		t.Fatalf("rule-providers = %#v, want map", config["rule-providers"])
	}
	provider, ok := providers["geo-lite/geosite/google"].(map[string]any)
	if !ok {
		t.Fatalf("provider = %#v, want map", providers["geo-lite/geosite/google"])
	}

	expected := map[string]any{
		"type":     "http",
		"behavior": "domain",
		"format":   "mrs",
		"url":      "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo-lite/geosite/google.mrs",
		"path":     "./rule-providers/geo-lite_geosite_google.mrs",
		"interval": 86400,
	}
	if !reflect.DeepEqual(provider, expected) {
		t.Fatalf("provider = %#v, want %#v", provider, expected)
	}
}

func TestMihomoDNSUsesReachableNameserversForProxyServerLookup(t *testing.T) {
	dns := mihomoDNS(repository.GlobalConfig{
		Fields: map[string]any{
			"dnsMode": "fake-ip",
		},
		DNSServers: []repository.GlobalDNSServer{
			{Name: "default-1", Role: "default", Protocol: "udp", Address: "223.5.5.5", Port: "53"},
			{Name: "proxy-1", Role: "proxy", Protocol: "https", Address: "8.8.8.8", Path: "/dns-query"},
		},
	})

	expected := []string{"223.5.5.5"}
	if !reflect.DeepEqual(dns["proxy-server-nameserver"], expected) {
		t.Fatalf("proxy-server-nameserver = %#v, want default DNS", dns["proxy-server-nameserver"])
	}
}

func TestMihomoDNSPrefersBootstrapForProxyServerLookup(t *testing.T) {
	dns := mihomoDNS(repository.GlobalConfig{
		Fields: map[string]any{
			"dnsMode": "fake-ip",
		},
		DNSServers: []repository.GlobalDNSServer{
			{Name: "bootstrap-1", Role: "bootstrap", Protocol: "udp", Address: "119.29.29.29", Port: "53"},
			{Name: "default-1", Role: "default", Protocol: "udp", Address: "223.5.5.5", Port: "53"},
		},
	})

	expected := []string{"119.29.29.29"}
	if !reflect.DeepEqual(dns["proxy-server-nameserver"], expected) {
		t.Fatalf("proxy-server-nameserver = %#v, want bootstrap DNS", dns["proxy-server-nameserver"])
	}
}

func TestMihomoTunAutoDetectsInterfaceWhenAutoRouteEnabled(t *testing.T) {
	tun := mihomoTun(repository.InboundTun{AutoRoute: true})

	if tun["auto-detect-interface"] != true {
		t.Fatalf("auto-detect-interface = %#v, want true when auto-route is enabled", tun["auto-detect-interface"])
	}
}

func TestSingBoxDNSServersUseNewServerFormat(t *testing.T) {
	servers := singBoxDNSServers([]repository.GlobalDNSServer{
		{Name: "system", Protocol: "system", Address: "system"},
		{Name: "plain", Protocol: "udp", Address: "223.5.5.5", Port: "5353"},
		{Name: "secure", Protocol: "https", Address: "dns.example.com", Port: "8443", Path: "/query", Detour: "proxy", SkipCertVerify: true},
	})

	expected := []map[string]any{
		{"type": "local", "tag": "system", "server": "system"},
		{"type": "udp", "tag": "plain", "server": "223.5.5.5", "server_port": 5353},
		{"type": "https", "tag": "secure", "server": "dns.example.com", "server_port": 8443, "path": "/query", "detour": "proxy", "tls": map[string]any{"insecure": true}},
	}
	if !reflect.DeepEqual(servers, expected) {
		t.Fatalf("singBoxDNSServers() = %#v, want %#v", servers, expected)
	}
	for _, server := range servers {
		if _, ok := server["address"]; ok {
			t.Fatalf("singBoxDNSServers() emitted legacy address field: %#v", servers)
		}
	}
}

func TestSingBoxDNSServersFallbackUsesNewServerFormat(t *testing.T) {
	servers := singBoxDNSServers(nil)

	if len(servers) != 0 {
		t.Fatalf("singBoxDNSServers(nil) = %#v, want empty preview-compatible server list", servers)
	}
}

func TestSingBoxDNSMatchesManagedPreviewShape(t *testing.T) {
	config := repository.GlobalConfig{
		Fields: map[string]any{
			"dnsMode":                  "fake-ip",
			"dnsFakeIpEnabled":         true,
			"dnsFakeIpRange":           "198.18.0.1/15",
			"dnsFakeIpRange6":          "fc00::/18",
			"dnsFakeIpFilterMode":      "blacklist",
			"dnsFakeIpFilters":         "*.lan\ngeosite:private",
			"dnsDefaultStrategy":       "prefer_ipv4",
			"dnsCacheEnabled":          false,
			"dnsCacheCapacity":         "2048",
			"dnsOptimisticEnabled":     true,
			"dnsOptimisticTimeout":     "2d",
			"dnsTimeout":               "8s",
			"dnsSingBoxReverseMapping": true,
			"dnsSingBoxClientSubnet":   "1.1.1.1/24",
		},
		DNSServers: []repository.GlobalDNSServer{
			{Name: "default-1", Role: "default", Protocol: "udp", Address: "223.5.5.5"},
			{Role: "proxy", Protocol: "https", Address: "dns.example.com", Port: "443", Path: "/query", Detour: "proxy", ClientSubnet: "2.2.2.2/24", SkipCertVerify: true},
		},
		DNSRules: []repository.GlobalDNSRule{
			{Matcher: "domain_suffix", Value: "example.com", Strategy: "ipv4_only", ClientSubnet: "3.3.3.3/24"},
		},
	}

	dns := singBoxDNS(config, runtimeCompileOptions{SingBoxDNS14: true})
	expected := map[string]any{
		"servers": []map[string]any{
			{"type": "udp", "tag": "default-1", "server": "223.5.5.5"},
			{"type": "https", "tag": "proxy-2", "server": "dns.example.com", "server_port": 443, "path": "/query", "detour": "proxy", "client_subnet": "2.2.2.2/24", "tls": map[string]any{"insecure": true}},
			{"type": "fakeip", "tag": "fakeip", "inet4_range": "198.18.0.1/15", "inet6_range": "fc00::/18"},
		},
		"rules": []map[string]any{
			{"domain_suffix": []string{"lan"}, "geosite": []string{"private"}, "server": "default-1"},
			{"domain_suffix": []string{"example.com"}, "server": "default-1", "strategy": "ipv4_only", "client_subnet": "3.3.3.3/24"},
			{"query_type": []string{"A", "AAAA"}, "server": "fakeip"},
		},
		"final":           "default-1",
		"strategy":        "prefer_ipv4",
		"disable_cache":   true,
		"cache_capacity":  2048,
		"optimistic":      map[string]any{"enabled": true, "timeout": "2d"},
		"timeout":         "8s",
		"reverse_mapping": true,
		"client_subnet":   "1.1.1.1/24",
	}
	if !reflect.DeepEqual(dns, expected) {
		t.Fatalf("singBoxDNS() = %#v, want %#v", dns, expected)
	}
}

func TestSingBoxDNSOmitsFakeIPWhenDisabled(t *testing.T) {
	config := repository.GlobalConfig{
		Fields: map[string]any{
			"dnsMode":            "fake-ip",
			"dnsFakeIpEnabled":   false,
			"dnsFakeIpFilters":   "*.lan",
			"dnsDefaultStrategy": "prefer_ipv4",
		},
		DNSServers: []repository.GlobalDNSServer{
			{Name: "default-1", Role: "default", Protocol: "udp", Address: "223.5.5.5"},
		},
	}

	dns := singBoxDNS(config, runtimeCompileOptions{SingBoxDNS14: true})
	expected := map[string]any{
		"servers": []map[string]any{
			{"type": "udp", "tag": "default-1", "server": "223.5.5.5"},
		},
		"final":           "default-1",
		"strategy":        "prefer_ipv4",
		"disable_cache":   false,
		"timeout":         "10s",
		"reverse_mapping": false,
	}
	if !reflect.DeepEqual(dns, expected) {
		t.Fatalf("singBoxDNS() = %#v, want %#v", dns, expected)
	}
}

func TestSingBoxDNSAddsRuntimeDetourToProxyServers(t *testing.T) {
	servers := singBoxDNSServers([]repository.GlobalDNSServer{
		{Name: "default-1", Role: "default", Protocol: "udp", Address: "223.5.5.5"},
		{Name: "proxy-1", Role: "proxy", Protocol: "https", Address: "1.1.1.1", Path: "/dns-query"},
		{Name: "proxy-custom", Role: "proxy", Protocol: "https", Address: "8.8.8.8", Path: "/dns-query", Detour: "manual"},
	}, "Proxy")

	expected := []map[string]any{
		{"type": "udp", "tag": "default-1", "server": "223.5.5.5"},
		{"type": "https", "tag": "proxy-1", "server": "1.1.1.1", "path": "/dns-query", "detour": "Proxy"},
		{"type": "https", "tag": "proxy-custom", "server": "8.8.8.8", "path": "/dns-query", "detour": "manual"},
	}
	if !reflect.DeepEqual(servers, expected) {
		t.Fatalf("singBoxDNSServers() = %#v, want %#v", servers, expected)
	}
}

func TestSingBoxDNSFakeIPWhitelistRulesUseFakeIPFirst(t *testing.T) {
	config := repository.GlobalConfig{
		Fields: map[string]any{
			"dnsMode":             "fake-ip",
			"dnsFakeIpEnabled":    true,
			"dnsFakeIpFilterMode": "whitelist",
			"dnsFakeIpFilters":    "rule-set:proxy-domain\n+.example.com",
		},
		DNSServers: []repository.GlobalDNSServer{
			{Name: "default-1", Role: "default", Protocol: "udp", Address: "223.5.5.5"},
		},
	}

	dns := singBoxDNS(config, runtimeCompileOptions{})
	expectedRules := []map[string]any{
		{"rule_set": []string{"proxy-domain"}, "domain_suffix": []string{"example.com"}, "server": "fakeip"},
	}
	if !reflect.DeepEqual(dns["rules"], expectedRules) {
		t.Fatalf("dns rules = %#v, want %#v", dns["rules"], expectedRules)
	}
	if dns["final"] != "default-1" {
		t.Fatalf("dns final = %#v, want default-1", dns["final"])
	}
}

func TestSingBoxDNSFakeIPBlacklistCatchAllStaysAfterUserRules(t *testing.T) {
	config := repository.GlobalConfig{
		Fields: map[string]any{
			"dnsMode":             "fake-ip",
			"dnsFakeIpEnabled":    true,
			"dnsFakeIpFilterMode": "blacklist",
		},
		DNSServers: []repository.GlobalDNSServer{
			{Name: "default-1", Role: "default", Protocol: "udp", Address: "223.5.5.5"},
			{Name: "policy-1", Role: "policy", Protocol: "udp", Address: "1.1.1.1"},
		},
		DNSRules: []repository.GlobalDNSRule{
			{Matcher: "domain", Value: "special.example", ServerName: "policy-1"},
		},
	}

	dns := singBoxDNS(config, runtimeCompileOptions{})
	expectedRules := []map[string]any{
		{"domain": []string{"special.example"}, "server": "policy-1"},
		{"query_type": []string{"A", "AAAA"}, "server": "fakeip"},
	}
	if !reflect.DeepEqual(dns["rules"], expectedRules) {
		t.Fatalf("dns rules = %#v, want %#v", dns["rules"], expectedRules)
	}
	if dns["final"] != "default-1" {
		t.Fatalf("dns final = %#v, want default-1", dns["final"])
	}
}

func TestMergeAdjacentSingBoxDNSRulesKeepsBoundaries(t *testing.T) {
	rules := mergeAdjacentSingBoxDNSRules([]map[string]any{
		{"domain": []string{"a.example"}, "server": "default"},
		{"domain_suffix": []string{"example.com"}, "server": "default"},
		{"server": "default"},
		{"geosite": []string{"gfw"}, "server": "fakeip"},
		{"rule_set": []string{"proxy"}, "server": "fakeip"},
	})

	expected := []map[string]any{
		{"domain": []string{"a.example"}, "domain_suffix": []string{"example.com"}, "server": "default"},
		{"server": "default"},
		{"geosite": []string{"gfw"}, "rule_set": []string{"proxy"}, "server": "fakeip"},
	}
	if !reflect.DeepEqual(rules, expected) {
		t.Fatalf("mergeAdjacentSingBoxDNSRules() = %#v, want %#v", rules, expected)
	}
}

func TestSingBoxDNSFakeIPRuleModeKeepsFilterOrder(t *testing.T) {
	config := repository.GlobalConfig{
		Fields: map[string]any{
			"dnsMode":             "fake-ip",
			"dnsFakeIpEnabled":    true,
			"dnsFakeIpFilterMode": "rule",
			"dnsFakeIpFilters":    "DOMAIN-SUFFIX,example.com,real-ip\nGEOSITE,gfw,fake-ip\nMATCH,real-ip",
		},
		DNSServers: []repository.GlobalDNSServer{
			{Name: "default-1", Role: "default", Protocol: "udp", Address: "223.5.5.5"},
		},
	}

	dns := singBoxDNS(config, runtimeCompileOptions{})
	expectedRules := []map[string]any{
		{"domain_suffix": []string{"example.com"}, "server": "default-1"},
		{"geosite": []string{"gfw"}, "server": "fakeip"},
		{"server": "default-1"},
	}
	if !reflect.DeepEqual(dns["rules"], expectedRules) {
		t.Fatalf("dns rules = %#v, want %#v", dns["rules"], expectedRules)
	}
}

func TestSingBoxDNSOmitsDNS14FieldsForOlderCore(t *testing.T) {
	config := repository.GlobalConfig{
		Fields: map[string]any{
			"dnsOptimisticEnabled": true,
			"dnsOptimisticTimeout": "2d",
			"dnsTimeout":           "8s",
		},
	}

	dns := singBoxDNS(config, runtimeCompileOptions{SingBoxDNS14: false})
	if _, ok := dns["timeout"]; ok {
		t.Fatalf("singBoxDNS() emitted dns.timeout for pre-1.14 core: %#v", dns)
	}
	if _, ok := dns["optimistic"]; ok {
		t.Fatalf("singBoxDNS() emitted dns.optimistic for pre-1.14 core: %#v", dns)
	}
}

func TestSemanticVersionAtLeast(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{version: "1.13.13", want: false},
		{version: "1.14.0", want: true},
		{version: "v1.15.1", want: true},
	}
	for _, tt := range tests {
		if got := semanticVersionAtLeast(tt.version, 1, 14); got != tt.want {
			t.Fatalf("semanticVersionAtLeast(%q, 1, 14) = %v, want %v", tt.version, got, tt.want)
		}
	}
}

func TestSingBoxInboundsOnlyEmitNetworkForSupportedKinds(t *testing.T) {
	inbounds := singBoxInbounds([]repository.ManagedInbound{
		{
			Enabled: true,
			Tag:     "redirect-in",
			Kind:    "redirect",
			Listen:  repository.InboundListen{Address: "0.0.0.0", Port: 7892},
			Network: "tcp",
			Raw:     map[string]any{"network": "udp"},
		},
		{
			Enabled: true,
			Tag:     "tproxy-in",
			Kind:    "tproxy",
			Listen:  repository.InboundListen{Address: "0.0.0.0", Port: 7895},
			Network: "tcp",
		},
		{
			Enabled: true,
			Tag:     "tun-in",
			Kind:    "tun",
			Tun: repository.InboundTun{
				AutoDetectInterface: true,
				Address:             []string{"172.20.0.1/30"},
				RouteAddress:        []string{"0.0.0.0/1"},
				RouteExcludeAddress: []string{"192.168.0.0/16"},
			},
		},
	}, runtimeCompileOptions{SingBoxDNS14: true})

	if _, ok := inbounds[0]["network"]; ok {
		t.Fatalf("redirect inbound emitted unsupported network field: %#v", inbounds[0])
	}
	tunIndex := 1
	if runtime.GOOS == "linux" {
		if inbounds[1]["network"] != "tcp" {
			t.Fatalf("tproxy inbound network = %#v, want tcp", inbounds[1]["network"])
		}
		tunIndex = 2
	}
	if _, ok := inbounds[tunIndex]["auto_detect_interface"]; ok {
		t.Fatalf("tun inbound emitted unsupported auto_detect_interface field: %#v", inbounds[tunIndex])
	}
	if _, ok := inbounds[tunIndex]["inet4_route_address"]; ok {
		t.Fatalf("tun inbound emitted legacy inet4_route_address field: %#v", inbounds[tunIndex])
	}
	if !reflect.DeepEqual(inbounds[tunIndex]["address"], []string{"172.20.0.1/30"}) {
		t.Fatalf("tun inbound address = %#v, want interface address", inbounds[tunIndex]["address"])
	}
	if !reflect.DeepEqual(inbounds[tunIndex]["route_address"], []string{"0.0.0.0/1"}) {
		t.Fatalf("tun inbound route_address = %#v, want merged route_address", inbounds[tunIndex]["route_address"])
	}
	if !reflect.DeepEqual(inbounds[tunIndex]["route_exclude_address"], []string{"192.168.0.0/16"}) {
		t.Fatalf("tun inbound route_exclude_address = %#v, want merged route_exclude_address", inbounds[tunIndex]["route_exclude_address"])
	}
	if inbounds[tunIndex]["dns_mode"] != "hijack" {
		t.Fatalf("tun inbound dns_mode = %#v, want hijack", inbounds[tunIndex]["dns_mode"])
	}
}

func TestSingBoxInboundTunDefaultsMissingAddress(t *testing.T) {
	tun := singBoxInboundTun(repository.InboundTun{})

	if !reflect.DeepEqual(tun["address"], []string{"172.19.0.1/30"}) {
		t.Fatalf("tun address = %#v, want default interface address", tun["address"])
	}
}

func TestSingBoxTProxyInboundIsLinuxOnly(t *testing.T) {
	supported := singBoxInboundSupportedOnCurrentOS("tproxy")
	if runtime.GOOS == "linux" && !supported {
		t.Fatalf("tproxy should be supported on linux")
	}
	if runtime.GOOS != "linux" && supported {
		t.Fatalf("tproxy should be skipped on %s", runtime.GOOS)
	}
}

func TestSingBoxRouteUsesGlobalRouteFields(t *testing.T) {
	route := singBoxRoute(repository.GlobalConfig{
		Fields: map[string]any{
			"routeAutoDetectInterface": true,
			"routeOverrideAndroidVpn":  true,
			"networkInterface":         "en0",
			"networkRoutingMark":       "2024",
			"dnsDefaultStrategy":       "prefer_ipv4",
			"dnsSingBoxClientSubnet":   "1.1.1.1/24",
		},
		DNSServers: []repository.GlobalDNSServer{
			{Name: "default-1", Role: "default"},
		},
	}, nil, nil, nil)

	expected := map[string]any{
		"rules": singBoxDefaultRouteRules(),
		"default_domain_resolver": map[string]any{
			"server":        "default-1",
			"strategy":      "prefer_ipv4",
			"client_subnet": "1.1.1.1/24",
		},
		"auto_detect_interface": true,
		"override_android_vpn":  true,
		"default_interface":     "en0",
		"default_mark":          2024,
	}
	if !reflect.DeepEqual(route, expected) {
		t.Fatalf("singBoxRoute() = %#v, want %#v", route, expected)
	}
}

func TestSingBoxRouteFallsBackToFirstDNSResolver(t *testing.T) {
	route := singBoxRoute(repository.GlobalConfig{
		Fields: map[string]any{},
		DNSServers: []repository.GlobalDNSServer{
			{Name: "proxy-1", Role: "proxy"},
		},
	}, nil, nil, nil)

	expected := map[string]any{
		"rules": singBoxDefaultRouteRules(),
		"default_domain_resolver": map[string]any{
			"server":   "proxy-1",
			"strategy": "prefer_ipv4",
		},
	}
	if !reflect.DeepEqual(route, expected) {
		t.Fatalf("singBoxRoute() = %#v, want %#v", route, expected)
	}
}

func TestSingBoxRoutePrependsDefaultRules(t *testing.T) {
	route := singBoxRoute(repository.GlobalConfig{}, []repository.NormalizedRule{
		{Fields: map[string]any{"domain": []string{"example.com"}}, Action: "route", Outbound: "DIRECT"},
	}, nil, nil)

	expectedRules := append(singBoxDefaultRouteRules(), map[string]any{
		"domain":   []string{"example.com"},
		"action":   "route",
		"outbound": "DIRECT",
	})
	if !reflect.DeepEqual(route["rules"], expectedRules) {
		t.Fatalf("route rules = %#v, want %#v", route["rules"], expectedRules)
	}
}

func TestSingBoxRouteCanDisableDefaultQuicBlock(t *testing.T) {
	route := singBoxRoute(repository.GlobalConfig{
		Fields: map[string]any{"routeBlockQuic": false},
	}, nil, nil, nil)

	expectedRules := []map[string]any{
		{"action": "sniff"},
		{
			"type": "logical",
			"mode": "or",
			"rules": []map[string]any{
				{"protocol": []string{"dns"}},
				{"port": 53},
			},
			"action": "hijack-dns",
		},
		{"ip_is_private": true, "outbound": "DIRECT"},
	}
	if !reflect.DeepEqual(route["rules"], expectedRules) {
		t.Fatalf("route rules = %#v, want %#v", route["rules"], expectedRules)
	}
}

func TestSingBoxRulesNormalizeBuiltInOutboundTags(t *testing.T) {
	rules := singBoxRules([]repository.NormalizedRule{
		{Fields: map[string]any{"domain": []string{"direct.example.com"}}, Action: "route", Outbound: "direct"},
		{Fields: map[string]any{"domain": []string{"blocked.example.com"}}, Action: "route", Outbound: "block"},
		{Fields: map[string]any{"domain": []string{"reject.example.com"}}, Action: "route", Outbound: "reject-drop"},
	})

	expected := []map[string]any{
		{"domain": []string{"direct.example.com"}, "action": "route", "outbound": "DIRECT"},
		{"domain": []string{"blocked.example.com"}, "action": "route", "outbound": "REJECT"},
		{"domain": []string{"reject.example.com"}, "action": "route", "outbound": "REJECT"},
	}
	if !reflect.DeepEqual(rules, expected) {
		t.Fatalf("singBoxRules() = %#v, want %#v", rules, expected)
	}
}

func TestSingBoxOutboundsDoNotLeakClashRawFields(t *testing.T) {
	outbounds := singBoxOutbounds([]repository.NormalizedNode{
		{
			Tag:        "proxy-a",
			Type:       "shadowsocks",
			Server:     "example.com",
			ServerPort: 8388,
			Transport:  map[string]any{"method": "2022-blake3-aes-128-gcm", "password": "secret"},
			Raw:        map[string]any{"name": "raw-name", "port": 1234, "skip-cert-verify": true},
		},
	}, nil)

	proxy := outbounds[2]
	expected := map[string]any{
		"type":        "shadowsocks",
		"tag":         "proxy-a",
		"server":      "example.com",
		"server_port": 8388,
		"method":      "2022-blake3-aes-128-gcm",
		"password":    "secret",
	}
	if !reflect.DeepEqual(proxy, expected) {
		t.Fatalf("singBoxOutbounds()[2] = %#v, want %#v", proxy, expected)
	}
	for _, key := range []string{"name", "port", "skip-cert-verify"} {
		if _, ok := proxy[key]; ok {
			t.Fatalf("singBoxOutbounds()[2] leaked raw field %q: %#v", key, proxy)
		}
	}
}

func TestSingBoxOutboundsKeepDialFieldsAndOmitMihomoOnlyCommonFields(t *testing.T) {
	outbounds := singBoxOutbounds([]repository.NormalizedNode{
		{
			Tag:        "proxy-a",
			Type:       "shadowsocks",
			Server:     "example.com",
			ServerPort: 8388,
			Transport: map[string]any{
				"method":            "2022-blake3-aes-128-gcm",
				"password":          "secret",
				"mihomo_ip_version": "ipv4-prefer",
				"domain_strategy":   "prefer_ipv4",
				"udp":               true,
				"detour":            "relay",
				"bind_interface":    "en0",
				"routing_mark":      1234,
				"connect_timeout":   "5s",
				"tcp_fast_open":     true,
				"tcp_multi_path":    true,
				"udp_fragment":      true,
				"network_strategy":  "hybrid",
				"network_type":      []any{"wifi", "ethernet"},
				"fallback_delay":    "250ms",
				"multiplex": map[string]any{
					"enabled":         true,
					"protocol":        "smux",
					"max_connections": 4,
				},
			},
		},
	}, nil)

	proxy := outbounds[2]
	expected := map[string]any{
		"type":             "shadowsocks",
		"tag":              "proxy-a",
		"server":           "example.com",
		"server_port":      8388,
		"method":           "2022-blake3-aes-128-gcm",
		"password":         "secret",
		"domain_strategy":  "prefer_ipv4",
		"detour":           "relay",
		"bind_interface":   "en0",
		"routing_mark":     1234,
		"connect_timeout":  "5s",
		"tcp_fast_open":    true,
		"tcp_multi_path":   true,
		"udp_fragment":     true,
		"network_strategy": "hybrid",
		"network_type":     []any{"wifi", "ethernet"},
		"fallback_delay":   "250ms",
		"multiplex":        map[string]any{"enabled": true, "protocol": "smux", "max_connections": 4},
	}
	if !reflect.DeepEqual(proxy, expected) {
		t.Fatalf("singBoxOutbounds()[2] = %#v, want %#v", proxy, expected)
	}
	for _, key := range []string{"udp", "mihomo_ip_version"} {
		if _, ok := proxy[key]; ok {
			t.Fatalf("singBoxOutbounds()[2] leaked mihomo-only field %q: %#v", key, proxy)
		}
	}
}

func TestMihomoProxiesMapNormalizedFieldsToMihomoShape(t *testing.T) {
	proxies := mihomoProxies([]repository.NormalizedNode{
		{
			Tag:        "ss-node",
			Type:       "shadowsocks",
			Server:     "ss.example.com",
			ServerPort: 8388,
			Transport: map[string]any{
				"method":            "2022-blake3-aes-128-gcm",
				"password":          "secret",
				"mihomo_ip_version": "ipv4-prefer",
				"udp":               true,
				"bind_interface":    "en0",
				"routing_mark":      1234,
				"tcp_fast_open":     true,
				"tcp_multi_path":    true,
				"detour":            "relay",
				"multiplex": map[string]any{
					"enabled":         true,
					"protocol":        "smux",
					"max_connections": 4,
					"min_streams":     2,
					"max_streams":     16,
					"padding":         true,
					"brutal": map[string]any{
						"enabled":   true,
						"up_mbps":   50,
						"down_mbps": 100,
					},
				},
			},
		},
		{
			Tag:        "vless-reality",
			Type:       "vless",
			Server:     "reality.example.com",
			ServerPort: 443,
			Transport: map[string]any{
				"uuid":       "7a179cb3-fe6f-4ce2-806d-35b9d71fe1bd",
				"flow":       "xtls-rprx-vision",
				"encryption": "none",
				"network":    "tcp",
				"tls": map[string]any{
					"enabled":     true,
					"server_name": "www.oracle.com",
					"utls": map[string]any{
						"enabled":     true,
						"fingerprint": "chrome",
					},
					"reality": map[string]any{
						"enabled":    true,
						"public_key": "-4UWQwn6n6ZFcXHQ8IuBe4wLV3ZD2FwcM40YeBLPWGc",
						"short_id":   "e39e425cbb5b",
					},
				},
			},
		},
	})

	expectedSS := map[string]any{
		"name":           "ss-node",
		"type":           "ss",
		"server":         "ss.example.com",
		"port":           8388,
		"cipher":         "2022-blake3-aes-128-gcm",
		"password":       "secret",
		"ip-version":     "ipv4-prefer",
		"udp":            true,
		"interface-name": "en0",
		"routing-mark":   1234,
		"tfo":            true,
		"mptcp":          true,
		"dialer-proxy":   "relay",
		"smux": map[string]any{
			"enabled":         true,
			"protocol":        "smux",
			"max-connections": 4,
			"min-streams":     2,
			"max-streams":     16,
			"padding":         true,
			"brutal-opts": map[string]any{
				"enabled": true,
				"up":      50,
				"down":    100,
			},
		},
	}
	if !reflect.DeepEqual(proxies[0], expectedSS) {
		t.Fatalf("mihomoProxies()[0] = %#v, want %#v", proxies[0], expectedSS)
	}

	expectedVLESS := map[string]any{
		"name":               "vless-reality",
		"type":               "vless",
		"server":             "reality.example.com",
		"port":               443,
		"uuid":               "7a179cb3-fe6f-4ce2-806d-35b9d71fe1bd",
		"flow":               "xtls-rprx-vision",
		"encryption":         "none",
		"network":            "tcp",
		"tls":                true,
		"servername":         "www.oracle.com",
		"sni":                "www.oracle.com",
		"client-fingerprint": "chrome",
		"reality-opts": map[string]any{
			"public-key": "-4UWQwn6n6ZFcXHQ8IuBe4wLV3ZD2FwcM40YeBLPWGc",
			"short-id":   "e39e425cbb5b",
		},
	}
	if !reflect.DeepEqual(proxies[1], expectedVLESS) {
		t.Fatalf("mihomoProxies()[1] = %#v, want %#v", proxies[1], expectedVLESS)
	}
}

func TestSingBoxOutboundsDoNotLeakClashGroupRawFields(t *testing.T) {
	outbounds := singBoxOutbounds(nil, []repository.NormalizedGroup{
		{
			Tag:       "auto",
			Type:      "url-test",
			Outbounds: []string{"proxy-a", "direct", "block"},
			Raw: map[string]any{
				"name":      "raw-name",
				"proxies":   []any{"raw-a"},
				"url":       "https://example.com/generate_204",
				"interval":  300,
				"tolerance": 50,
			},
		},
	})

	group := outbounds[2]
	expected := map[string]any{
		"type":      "urltest",
		"tag":       "auto",
		"outbounds": []string{"proxy-a", "DIRECT", "REJECT"},
		"url":       "https://example.com/generate_204",
		"interval":  "300ms",
		"tolerance": 50,
	}
	if !reflect.DeepEqual(group, expected) {
		t.Fatalf("singBoxOutbounds()[2] = %#v, want %#v", group, expected)
	}
	for _, key := range []string{"name", "proxies"} {
		if _, ok := group[key]; ok {
			t.Fatalf("singBoxOutbounds()[2] leaked raw group field %q: %#v", key, group)
		}
	}
}

func TestSingBoxOutboundsConvertsUnsupportedClashGroupTypes(t *testing.T) {
	outbounds := singBoxOutbounds(nil, []repository.NormalizedGroup{
		{
			Tag:       "balanced",
			Type:      "load-balance",
			Outbounds: []string{"proxy-a", "proxy-b"},
			Raw: map[string]any{
				"url":       "https://example.com/generate_204",
				"interval":  300,
				"tolerance": 50,
			},
		},
	})

	group := outbounds[2]
	expected := map[string]any{
		"type":      "selector",
		"tag":       "balanced",
		"outbounds": []string{"proxy-a", "proxy-b"},
	}
	if !reflect.DeepEqual(group, expected) {
		t.Fatalf("singBoxOutbounds()[2] = %#v, want %#v", group, expected)
	}
}

func TestSingBoxRulesConvertGeoIPToBuiltInRuleSet(t *testing.T) {
	rules := singBoxRules([]repository.NormalizedRule{
		{
			Fields:   map[string]any{"geoip": []string{"cn"}},
			Action:   "route",
			Outbound: "DIRECT",
		},
		{
			Action:   "route",
			Outbound: "MATCH",
		},
	})

	expected := []map[string]any{
		{"rule_set": []string{"geoip-cn"}, "action": "route", "outbound": "DIRECT"},
		{"action": "route", "outbound": "MATCH"},
	}
	if !reflect.DeepEqual(rules, expected) {
		t.Fatalf("singBoxRules() = %#v, want %#v", rules, expected)
	}
}

func TestSingBoxRulesConvertGeositeToBuiltInRuleSet(t *testing.T) {
	rules := singBoxRules([]repository.NormalizedRule{
		{
			Fields:   map[string]any{"geosite": []string{"geolocation-!cn"}},
			Action:   "route",
			Outbound: "PROXY",
		},
	})

	expected := []map[string]any{
		{"rule_set": []string{"geosite-geolocation-!cn"}, "action": "route", "outbound": "PROXY"},
	}
	if !reflect.DeepEqual(rules, expected) {
		t.Fatalf("singBoxRules() = %#v, want %#v", rules, expected)
	}
}

func TestSingBoxRouteAddsBuiltInGeoRuleSets(t *testing.T) {
	route := singBoxRoute(repository.GlobalConfig{}, []repository.NormalizedRule{
		{Fields: map[string]any{"geoip": []string{"CN"}}, Action: "route", Outbound: "DIRECT"},
		{Fields: map[string]any{"geosite": []string{"geolocation-!cn"}}, Action: "route", Outbound: "PROXY"},
	}, nil, nil, "PROXY")

	expected := []map[string]any{
		{
			"type":            "remote",
			"tag":             "geoip-cn",
			"format":          "binary",
			"url":             "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geoip/cn.srs",
			"download_detour": "PROXY",
		},
		{
			"type":            "remote",
			"tag":             "geosite-geolocation-!cn",
			"format":          "binary",
			"url":             "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geosite/geolocation-%21cn.srs",
			"download_detour": "PROXY",
		},
	}
	if !reflect.DeepEqual(route["rule_set"], expected) {
		t.Fatalf("route rule_set = %#v, want %#v", route["rule_set"], expected)
	}
}

func TestSingBoxConfiguredRuleSetsUseDownloadDetourForRemoteSources(t *testing.T) {
	route := singBoxRoute(repository.GlobalConfig{}, nil, []repository.SingBoxRuleSetResource{
		{
			Tag:            "remote-proxy",
			Format:         "binary",
			SourceMode:     repository.RuleAssetSourceModeRemote,
			URL:            "https://example.com/proxy.srs",
			UpdateInterval: "1d",
		},
		{
			Tag:        "local-direct",
			Format:     "binary",
			SourceMode: repository.RuleAssetSourceModeLocal,
			LocalPath:  "/tmp/direct.srs",
		},
	}, nil, "PROXY")

	expected := []map[string]any{
		{
			"tag":             "remote-proxy",
			"format":          "binary",
			"type":            "remote",
			"url":             "https://example.com/proxy.srs",
			"download_detour": "PROXY",
			"update_interval": "1d",
		},
		{
			"tag":    "local-direct",
			"format": "binary",
			"type":   "local",
			"path":   "/tmp/direct.srs",
		},
	}
	if !reflect.DeepEqual(route["rule_set"], expected) {
		t.Fatalf("route rule_set = %#v, want %#v", route["rule_set"], expected)
	}
}

func TestSingBoxRuleSetDownloadDetourPrefersProxyRuleOutbound(t *testing.T) {
	detour := singBoxRuleSetDownloadDetour(
		[]repository.NormalizedNode{{Tag: "node-a"}},
		[]repository.NormalizedGroup{{Tag: "Proxy"}, {Tag: "Auto"}},
		[]repository.NormalizedRule{
			{Fields: map[string]any{"geoip": []string{"cn"}}, Outbound: "DIRECT"},
			{Fields: map[string]any{"geosite": []string{"geolocation-!cn"}}, Outbound: "Proxy"},
		},
	)

	if detour != "Proxy" {
		t.Fatalf("detour = %q, want proxy rule outbound", detour)
	}
}

func TestSingBoxRouteAddsReferencedBuiltInRuleSets(t *testing.T) {
	route := singBoxRoute(repository.GlobalConfig{}, []repository.NormalizedRule{
		{Fields: map[string]any{"rule_set": []string{"geo/geosite/cn"}}, Action: "route", Outbound: "DIRECT"},
	}, nil, nil)

	expected := []map[string]any{
		{
			"type":   "remote",
			"tag":    "geo/geosite/cn",
			"format": "binary",
			"url":    "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geosite/cn.srs",
		},
	}
	if !reflect.DeepEqual(route["rule_set"], expected) {
		t.Fatalf("route rule_set = %#v, want %#v", route["rule_set"], expected)
	}
}

func TestSingBoxRulesSkipLogicalRuleWhenSourceGeoIPChildrenAreRemoved(t *testing.T) {
	rules := singBoxRules([]repository.NormalizedRule{
		{
			Type:   "logical",
			Mode:   "or",
			Action: "route",
			Rules: []repository.NormalizedRule{
				{Fields: map[string]any{"source_geoip": []string{"private"}}, Action: "route", Outbound: "DIRECT"},
			},
			Outbound: "DIRECT",
		},
	})

	if len(rules) != 0 {
		t.Fatalf("singBoxRules() = %#v, want removed logical rule", rules)
	}
}
