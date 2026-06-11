package api

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

func TestMihomoRuntimeConfigMatchesStandardDefaultShape(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	config, err := store.GlobalConfig()
	if err != nil {
		t.Fatalf("GlobalConfig() error = %v", err)
	}

	runtimeConfig := mihomoRuntimeConfig(config, nil, nil, nil, "0.0.0.0:9090")
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

	data, err := marshalMihomoRuntimeConfig(mihomoRuntimeConfig(config, nil, nil, nil, "0.0.0.0:9090"))
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
	})

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

func TestSingBoxOutboundsDoNotLeakClashGroupRawFields(t *testing.T) {
	outbounds := singBoxOutbounds(nil, []repository.NormalizedGroup{
		{
			Tag:       "auto",
			Type:      "url-test",
			Outbounds: []string{"proxy-a", "proxy-b"},
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
		"outbounds": []string{"proxy-a", "proxy-b"},
		"url":       "https://example.com/generate_204",
		"interval":  300,
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

func TestSingBoxRouteAddsBuiltInGeoIPRuleSets(t *testing.T) {
	route := singBoxRoute(repository.GlobalConfig{}, []repository.NormalizedRule{
		{Fields: map[string]any{"geoip": []string{"CN"}}, Action: "route", Outbound: "DIRECT"},
	}, nil, nil)

	expected := []map[string]any{
		{
			"type":   "remote",
			"tag":    "geoip-cn",
			"format": "binary",
			"url":    "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geoip/cn.srs",
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
