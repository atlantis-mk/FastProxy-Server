package importer

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

func TestImportClashCreatesDerivedRepositoryResources(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	service := NewService(store)

	result, err := service.ImportClash(ClashImportInput{
		Name: "Office",
		Content: `
proxies:
  - name: hk-1
    type: vmess
    server: hk.example.com
    port: 443
proxy-groups:
  - name: auto
    type: select
    proxies: [hk-1]
rules:
  - DOMAIN,google.com,auto
  - DOMAIN-SUFFIX,google.com,auto
  - DOMAIN,openai.com,direct
  - DOMAIN-KEYWORD,chatgpt,auto
  - NETWORK,UDP,DIRECT
`,
	})
	if err != nil {
		t.Fatalf("ImportClash() error = %v", err)
	}
	if result.Subscription == nil || result.NodeSet == nil || result.GroupSet == nil || result.RuleSet == nil {
		t.Fatalf("ImportClash() should create subscription, node set, group set, and rule set")
	}
	if len(result.NodeSet.Nodes) != 1 {
		t.Fatalf("len(result.NodeSet.Nodes) = %d, want 1", len(result.NodeSet.Nodes))
	}
	node := result.NodeSet.Nodes[0]
	if node.Tag != "hk-1" {
		t.Fatalf("node.Tag = %q, want hk-1", node.Tag)
	}
	if node.Type != "vmess" {
		t.Fatalf("node.Type = %q, want vmess", node.Type)
	}
	if node.ServerPort != 443 {
		t.Fatalf("node.ServerPort = %d, want 443", node.ServerPort)
	}
	serialized, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("json.Marshal(node) error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(serialized, &payload); err != nil {
		t.Fatalf("json.Unmarshal(node) error = %v", err)
	}
	if _, ok := payload["transport"]; ok {
		t.Fatalf("serialized node should not expose transport wrapper: %s", string(serialized))
	}
	if len(result.RuleSet.Rules) != 4 {
		t.Fatalf("len(result.RuleSet.Rules) = %d, want 4 rules preserving contiguous blocks", len(result.RuleSet.Rules))
	}
	autoRule := result.RuleSet.Rules[0]
	if autoRule.Type != "" || autoRule.Outbound != "auto" {
		t.Fatalf("auto aggregated rule should be a flattened atomic rule: %+v", autoRule)
	}
	domains, _ := autoRule.Fields["domain"].([]string)
	if len(domains) != 1 || domains[0] != "google.com" {
		t.Fatalf("auto domain aggregation = %#v, want [google.com]", autoRule.Fields["domain"])
	}
	domainSuffixes, _ := autoRule.Fields["domain_suffix"].([]string)
	if len(domainSuffixes) != 1 || domainSuffixes[0] != "google.com" {
		t.Fatalf("auto domain_suffix aggregation = %#v, want [google.com]", autoRule.Fields["domain_suffix"])
	}
	rawLines := autoRule.Raw
	if len(rawLines) != 2 || rawLines[0] != "DOMAIN,google.com,auto" || rawLines[1] != "DOMAIN-SUFFIX,google.com,auto" {
		t.Fatalf("auto aggregated rule raw lines = %#v, want both merged source rules", rawLines)
	}
	serializedRule, err := json.Marshal(autoRule)
	if err != nil {
		t.Fatalf("json.Marshal(rule) error = %v", err)
	}
	var serializedRulePayload map[string]any
	if err := json.Unmarshal(serializedRule, &serializedRulePayload); err != nil {
		t.Fatalf("json.Unmarshal(rule) error = %v", err)
	}
	if _, ok := serializedRulePayload["fields"]; ok {
		t.Fatalf("serialized rule should not expose fields wrapper: %s", string(serializedRule))
	}
	if _, ok := serializedRulePayload["domain_suffix"]; !ok {
		t.Fatalf("serialized rule should flatten domain_suffix to top level: %s", string(serializedRule))
	}
	middleDirectRule := result.RuleSet.Rules[1]
	if middleDirectRule.Type != "" || middleDirectRule.Outbound != "DIRECT" {
		t.Fatalf("middle direct rule should stay standalone to preserve order: %+v", middleDirectRule)
	}
	secondAutoRule := result.RuleSet.Rules[2]
	if secondAutoRule.Type != "" || secondAutoRule.Outbound != "auto" {
		t.Fatalf("second auto rule should stay standalone after direct split: %+v", secondAutoRule)
	}
	directRule := result.RuleSet.Rules[3]
	if directRule.Outbound != "DIRECT" {
		t.Fatalf("final direct rule outbound = %q, want DIRECT", directRule.Outbound)
	}

	bootstrap, err := store.Bootstrap()
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	routingRuleSets, err := store.ListRuleSets()
	if err != nil {
		t.Fatalf("ListRuleSets() error = %v", err)
	}
	if len(bootstrap.Subscriptions) != 1 ||
		len(bootstrap.NodeSets) != 1 ||
		len(bootstrap.GroupSets) != 1 ||
		len(bootstrap.RuleSourceRepositories) != 1 ||
		len(bootstrap.SingBoxRuleSets) != 0 ||
		len(bootstrap.MihomoRuleProviders) != 0 ||
		len(routingRuleSets) != 1 {
		t.Fatalf("unexpected repository counts: %+v", bootstrap)
	}
}

func TestImportClashPreservesCommonProxyFields(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	service := NewService(store)

	result, err := service.ImportClash(ClashImportInput{
		Name: "Common",
		Content: `
proxies:
  - name: tuned
    type: ss
    server: tuned.example.com
    port: 443
    cipher: 2022-blake3-aes-128-gcm
    password: secret
    ip-version: ipv4-prefer
    udp: true
    interface-name: en0
    routing-mark: 1234
    tfo: true
    mptcp: true
    dialer-proxy: relay
    connect_timeout: 5s
    udp_fragment: true
    network_strategy: hybrid
    network_type: [wifi, ethernet]
    fallback_delay: 250ms
    smux:
      enabled: true
      protocol: smux
      max-connections: 4
      min-streams: 2
      max-streams: 16
      padding: true
      brutal-opts:
        enabled: true
        up: 50
        down: 100
`,
	})
	if err != nil {
		t.Fatalf("ImportClash() error = %v", err)
	}
	if result.NodeSet == nil || len(result.NodeSet.Nodes) != 1 {
		t.Fatalf("imported nodes = %#v, want one node", result.NodeSet)
	}

	transport := result.NodeSet.Nodes[0].Transport
	expected := map[string]any{
		"method":            "2022-blake3-aes-128-gcm",
		"password":          "secret",
		"mihomo_ip_version": "ipv4-prefer",
		"domain_strategy":   "prefer_ipv4",
		"udp":               true,
		"bind_interface":    "en0",
		"routing_mark":      1234,
		"tcp_fast_open":     true,
		"tcp_multi_path":    true,
		"detour":            "relay",
		"connect_timeout":   "5s",
		"udp_fragment":      true,
		"network_strategy":  "hybrid",
		"network_type":      []any{"wifi", "ethernet"},
		"fallback_delay":    "250ms",
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
	}
	if !reflect.DeepEqual(transport, expected) {
		t.Fatalf("transport = %#v, want %#v", transport, expected)
	}
}

func TestCreateManualNodeAppendsToManualNodeSet(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	service := NewService(store)

	first, err := service.CreateManualNode(ManualNodeImportInput{
		Name: "手动添加",
		Node: repository.NormalizedNode{
			Tag:        "Manual A",
			Type:       "trojan",
			Server:     "a.example.com",
			ServerPort: 443,
		},
	})
	if err != nil {
		t.Fatalf("CreateManualNode(first) error = %v", err)
	}
	if len(first.NodeSet.Nodes) != 1 {
		t.Fatalf("len(first.NodeSet.Nodes) = %d, want 1", len(first.NodeSet.Nodes))
	}

	second, err := service.CreateManualNode(ManualNodeImportInput{
		Name: "手动添加",
		Node: repository.NormalizedNode{
			Tag:        "Manual B",
			Type:       "vless",
			Server:     "b.example.com",
			ServerPort: 8443,
		},
	})
	if err != nil {
		t.Fatalf("CreateManualNode(second) error = %v", err)
	}
	if len(second.NodeSet.Nodes) != 2 {
		t.Fatalf("len(second.NodeSet.Nodes) = %d, want 2", len(second.NodeSet.Nodes))
	}
	if second.NodeSet.Nodes[0].Source != "手动添加" || second.NodeSet.Nodes[1].Source != "手动添加" {
		t.Fatalf("manual node sources = %q, %q; want 手动添加", second.NodeSet.Nodes[0].Source, second.NodeSet.Nodes[1].Source)
	}

	replacement, err := service.CreateManualNode(ManualNodeImportInput{
		Name: "手动添加",
		Node: repository.NormalizedNode{
			Tag:        "Manual A",
			Type:       "trojan",
			Server:     "new-a.example.com",
			ServerPort: 443,
		},
	})
	if err != nil {
		t.Fatalf("CreateManualNode(replacement) error = %v", err)
	}
	if len(replacement.NodeSet.Nodes) != 2 {
		t.Fatalf("len(replacement.NodeSet.Nodes) = %d, want 2 after replacement", len(replacement.NodeSet.Nodes))
	}
	if replacement.NodeSet.Nodes[0].Server != "new-a.example.com" {
		t.Fatalf("replacement server = %q, want new-a.example.com", replacement.NodeSet.Nodes[0].Server)
	}

	deleted, err := service.DeleteManualNode(ManualNodeDeleteInput{Name: "手动添加", Tag: "Manual A"})
	if err != nil {
		t.Fatalf("DeleteManualNode() error = %v", err)
	}
	if len(deleted.NodeSet.Nodes) != 1 {
		t.Fatalf("len(deleted.NodeSet.Nodes) = %d, want 1", len(deleted.NodeSet.Nodes))
	}
	if deleted.NodeSet.Nodes[0].Tag != "Manual B" {
		t.Fatalf("remaining node tag = %q, want Manual B", deleted.NodeSet.Nodes[0].Tag)
	}
}

func TestImportClashFlattensNodeTransportFieldsInJSON(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	service := NewService(store)

	result, err := service.ImportClash(ClashImportInput{
		Name: "Trojan",
		Content: `
proxies:
  - name: test-trojan
    type: trojan
    server: trojan.example.com
    port: 443
    password: secret
    skip-cert-verify: true
    sni: m.ctrip.com
`,
	})
	if err != nil {
		t.Fatalf("ImportClash() error = %v", err)
	}

	serialized, err := json.Marshal(result.NodeSet.Nodes[0])
	if err != nil {
		t.Fatalf("json.Marshal(node) error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(serialized, &payload); err != nil {
		t.Fatalf("json.Unmarshal(node) error = %v", err)
	}
	if payload["password"] != "secret" {
		t.Fatalf("payload[password] = %#v, want secret", payload["password"])
	}
	tls, ok := payload["tls"].(map[string]any)
	if !ok || tls["enabled"] != true || tls["server_name"] != "m.ctrip.com" {
		t.Fatalf("payload[tls] = %#v, want flattened tls object", payload["tls"])
	}
	if _, ok := payload["transport"]; ok {
		t.Fatalf("payload should not contain transport wrapper: %s", string(serialized))
	}
}

func TestImportClashKeepsThirdRuleSegmentAsOutboundWhenUsingNoResolve(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	service := NewService(store)

	result, err := service.ImportClash(ClashImportInput{
		Name: "Rules",
		Content: `
rules:
  - IP-CIDR,127.0.0.0/8,DIRECT,no-resolve
`,
	})
	if err != nil {
		t.Fatalf("ImportClash() error = %v", err)
	}
	if result.RuleSet == nil || len(result.RuleSet.Rules) != 1 {
		t.Fatalf("expected one generated rule set rule, got %+v", result.RuleSet)
	}

	rule := result.RuleSet.Rules[0]
	if rule.Outbound != "DIRECT" {
		t.Fatalf("rule.Outbound = %q, want DIRECT", rule.Outbound)
	}
	ipCIDR, _ := rule.Fields["ip_cidr"].([]string)
	if len(ipCIDR) != 1 || ipCIDR[0] != "127.0.0.0/8" {
		t.Fatalf("rule.Fields[ip_cidr] = %#v, want [127.0.0.0/8]", rule.Fields["ip_cidr"])
	}
}

func TestImportClashNormalizesBuiltInOutboundTags(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	service := NewService(store)

	result, err := service.ImportClash(ClashImportInput{
		Name: "BuiltIns",
		Content: `
rules:
  - DOMAIN,example.com,direct
  - DOMAIN,ads.example.com,REJECT
  - DOMAIN,drop.example.com,REJECT-DROP
`,
	})
	if err != nil {
		t.Fatalf("ImportClash() error = %v", err)
	}
	if result.RuleSet == nil || len(result.RuleSet.Rules) != 2 {
		t.Fatalf("expected two generated rule set rules after aggregation, got %+v", result.RuleSet)
	}
	if result.RuleSet.Rules[0].Outbound != "DIRECT" {
		t.Fatalf("rule[0].Outbound = %q, want DIRECT", result.RuleSet.Rules[0].Outbound)
	}
	if result.RuleSet.Rules[1].Outbound != "REJECT" {
		t.Fatalf("rule[1].Outbound = %q, want REJECT", result.RuleSet.Rules[1].Outbound)
	}
	rejectedDomains, _ := result.RuleSet.Rules[1].Fields["domain"].([]string)
	if len(rejectedDomains) != 2 || rejectedDomains[0] != "ads.example.com" || rejectedDomains[1] != "drop.example.com" {
		t.Fatalf("REJECT domains = %#v, want both rejected domains", result.RuleSet.Rules[1].Fields["domain"])
	}
}

func TestImportPlainNodesDoesNotOverwriteExistingStateOnFailure(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	service := NewService(store)

	_, err = store.CreateNodeSet(repository.NodeSetResource{
		Metadata: repository.Metadata{
			Name:       "Existing",
			OriginType: repository.OriginPlainNode,
		},
		Nodes: []repository.NormalizedNode{{
			ID:         "node-1",
			Tag:        "Existing node",
			Type:       "trojan",
			Server:     "example.com",
			ServerPort: 443,
		}},
	})
	if err != nil {
		t.Fatalf("CreateNodeSet() error = %v", err)
	}

	if _, err := service.ImportPlainNodes(PlainNodeImportInput{
		Name:    "Broken",
		Content: "not-a-supported-uri",
	}); err == nil {
		t.Fatalf("ImportPlainNodes() error = nil, want failure")
	}

	nodeSets, err := store.ListNodeSets()
	if err != nil {
		t.Fatalf("ListNodeSets() error = %v", err)
	}
	if len(nodeSets) != 1 {
		t.Fatalf("len(nodeSets) = %d, want 1", len(nodeSets))
	}
	if nodeSets[0].Name != "Existing" {
		t.Fatalf("existing node set was unexpectedly replaced: %+v", nodeSets[0])
	}
}
