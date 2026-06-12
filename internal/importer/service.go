package importer

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
	"gopkg.in/yaml.v3"
)

var ErrInvalidImport = errors.New("invalid import payload")

type Diagnostics struct {
	Warnings []string `json:"warnings,omitempty"`
}

type ClashImportInput struct {
	Name      string `json:"name"`
	SourceURL string `json:"sourceUrl"`
	Content   string `json:"content"`
}

type PlainNodeImportInput struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type ManualNodeImportInput struct {
	Name string                    `json:"name"`
	Node repository.NormalizedNode `json:"node"`
}

type ManualNodeDeleteInput struct {
	Name string `json:"name"`
	Tag  string `json:"tag"`
}

type Result struct {
	Diagnostics  Diagnostics                      `json:"diagnostics"`
	Subscription *repository.SubscriptionResource `json:"subscription,omitempty"`
	NodeSet      *repository.NodeSetResource      `json:"nodeSet,omitempty"`
	RuleSet      *repository.RuleSetResource      `json:"ruleSet,omitempty"`
	GroupSet     *repository.GroupSetResource     `json:"groupSet,omitempty"`
}

type Service struct {
	store *repository.Store
}

func NewService(store *repository.Store) *Service {
	return &Service{store: store}
}

func ParseClashContent(content string) (repository.NormalizedConfig, Diagnostics, error) {
	if strings.TrimSpace(content) == "" {
		return repository.NormalizedConfig{}, Diagnostics{}, fmt.Errorf("%w: content is required", ErrInvalidImport)
	}

	type clashPayload struct {
		Proxies     []map[string]any `yaml:"proxies" json:"proxies"`
		ProxyGroups []map[string]any `yaml:"proxy-groups" json:"proxy-groups"`
		Rules       []string         `yaml:"rules" json:"rules"`
	}

	var payload clashPayload
	if err := yaml.Unmarshal([]byte(content), &payload); err != nil {
		return repository.NormalizedConfig{}, Diagnostics{}, fmt.Errorf("%w: %s", ErrInvalidImport, err)
	}
	if len(payload.Proxies) == 0 && len(payload.ProxyGroups) == 0 && len(payload.Rules) == 0 {
		return repository.NormalizedConfig{}, Diagnostics{}, fmt.Errorf("%w: no supported clash resources found", ErrInvalidImport)
	}

	normalized := repository.NormalizedConfig{
		Nodes:  make([]repository.NormalizedNode, 0, len(payload.Proxies)),
		Groups: make([]repository.NormalizedGroup, 0, len(payload.ProxyGroups)),
		Rules:  make([]repository.NormalizedRule, 0, len(payload.Rules)),
	}
	diagnostics := Diagnostics{}

	for index, proxy := range payload.Proxies {
		normalized.Nodes = append(normalized.Nodes, normalizeClashProxy(proxy, index))
	}
	for index, group := range payload.ProxyGroups {
		normalized.Groups = append(normalized.Groups, repository.NormalizedGroup{
			ID:        repository.NewID("group"),
			Tag:       stringValue(group["name"], fmt.Sprintf("group-%d", index+1)),
			Type:      stringValue(group["type"], "select"),
			Outbounds: stringSlice(group["proxies"]),
			Raw:       group,
		})
	}
	translatedRules, ruleWarnings := normalizeClashRules(payload.Rules)
	diagnostics.Warnings = append(diagnostics.Warnings, ruleWarnings...)
	normalized.Rules = translatedRules
	return normalized, diagnostics, nil
}

func (s *Service) ImportClash(input ClashImportInput) (Result, error) {
	normalized, diagnostics, err := ParseClashContent(input.Content)
	if err != nil {
		return Result{}, err
	}

	subscription, err := s.store.CreateSubscription(repository.SubscriptionResource{
		Metadata: repository.Metadata{
			Name:       fallbackName(input.Name, "Imported Clash subscription"),
			OriginType: repository.OriginClashSubscription,
		},
		SourceURL: input.SourceURL,
		Fetch: repository.SubscriptionFetchOptions{
			SourceInput: input.SourceURL,
			UserAgent:   "Clash",
		},
	})
	if err != nil {
		return Result{}, err
	}

	nodeSet, err := s.store.UpsertManagedNodeSet(repository.NodeSetResource{
		Metadata: repository.Metadata{
			Name:       repository.SubscriptionNodeSetName(subscription.Name),
			OriginType: repository.OriginClashSubscription,
		},
		Nodes: assignNodeSource(normalized.Nodes, subscription.Name),
	})
	if err != nil {
		return Result{}, err
	}
	if err := s.store.UpsertNodeCache(repository.NodeCacheUpsert{
		SourceType:     repository.NodeCacheSourceSubscription,
		SourceID:       subscription.ID,
		SubscriptionID: subscription.ID,
		NodeSetID:      nodeSet.ID,
		Nodes:          nodeSet.Nodes,
	}); err != nil {
		return Result{}, err
	}

	groupSet, err := s.store.CreateGroupSet(repository.GroupSetResource{
		Metadata: repository.Metadata{
			Name:       repository.SubscriptionGroupSetName(subscription.Name),
			OriginType: repository.OriginClashSubscription,
		},
		Groups: normalized.Groups,
	})
	if err != nil {
		return Result{}, err
	}

	ruleSet, err := s.store.CreateRuleSet(repository.RuleSetResource{
		Metadata: repository.Metadata{
			Name:       repository.SubscriptionRuleSetName(subscription.Name),
			OriginType: repository.OriginClashSubscription,
		},
		Rules: normalized.Rules,
	})
	if err != nil {
		return Result{}, err
	}

	return Result{
		Diagnostics:  diagnostics,
		Subscription: &subscription,
		NodeSet:      &nodeSet,
		GroupSet:     &groupSet,
		RuleSet:      &ruleSet,
	}, nil
}

func (s *Service) ImportPlainNodes(input PlainNodeImportInput) (Result, error) {
	lines := strings.Split(strings.TrimSpace(input.Content), "\n")
	nodes := make([]repository.NormalizedNode, 0, len(lines))
	diagnostics := Diagnostics{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		node, warning, err := parseNodeURI(line)
		if err != nil {
			return Result{}, err
		}
		if warning != "" {
			diagnostics.Warnings = append(diagnostics.Warnings, warning)
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return Result{}, fmt.Errorf("%w: no node URI found", ErrInvalidImport)
	}

	nodeSet, err := s.store.CreateNodeSet(repository.NodeSetResource{
		Metadata: repository.Metadata{
			Name:       fallbackName(input.Name, "Imported node set"),
			OriginType: repository.OriginPlainNode,
		},
		Nodes: nodes,
	})
	if err != nil {
		return Result{}, err
	}
	if err := s.store.UpsertNodeCache(repository.NodeCacheUpsert{
		SourceType: repository.NodeCacheSourceImport,
		SourceID:   nodeSet.ID,
		NodeSetID:  nodeSet.ID,
		Nodes:      nodeSet.Nodes,
	}); err != nil {
		return Result{}, err
	}
	return Result{
		Diagnostics: diagnostics,
		NodeSet:     &nodeSet,
	}, nil
}

func (s *Service) CreateManualNode(input ManualNodeImportInput) (Result, error) {
	if strings.TrimSpace(input.Node.Tag) == "" || strings.TrimSpace(input.Node.Type) == "" {
		return Result{}, fmt.Errorf("%w: manual node requires name and type", ErrInvalidImport)
	}
	nodeSetName := fallbackName(input.Name, input.Node.Tag)
	node := assignNodeSource([]repository.NormalizedNode{withNodeDefaults(input.Node)}, nodeSetName)[0]
	nodes := []repository.NormalizedNode{node}
	if current, err := s.store.GetNodeSet(nodeSetName); err == nil {
		nodes = mergeManualNodes(current.Nodes, node)
	} else if !errors.Is(err, repository.ErrNotFound) {
		return Result{}, err
	}
	nodeSet, err := s.store.UpsertManagedNodeSet(repository.NodeSetResource{
		Metadata: repository.Metadata{
			Name:       nodeSetName,
			OriginType: repository.OriginManual,
		},
		Nodes: nodes,
	})
	if err != nil {
		return Result{}, err
	}
	if err := s.store.UpsertNodeCache(repository.NodeCacheUpsert{
		SourceType: repository.NodeCacheSourceManual,
		SourceID:   nodeSet.ID,
		NodeSetID:  nodeSet.ID,
		Nodes:      nodeSet.Nodes,
	}); err != nil {
		return Result{}, err
	}
	return Result{NodeSet: &nodeSet}, nil
}

func mergeManualNodes(nodes []repository.NormalizedNode, node repository.NormalizedNode) []repository.NormalizedNode {
	result := make([]repository.NormalizedNode, 0, len(nodes)+1)
	replaced := false
	for _, item := range nodes {
		if item.Tag == node.Tag {
			result = append(result, node)
			replaced = true
			continue
		}
		result = append(result, item)
	}
	if !replaced {
		result = append(result, node)
	}
	return result
}

func (s *Service) DeleteManualNode(input ManualNodeDeleteInput) (Result, error) {
	nodeSetName := strings.TrimSpace(input.Name)
	tag := strings.TrimSpace(input.Tag)
	if nodeSetName == "" || tag == "" {
		return Result{}, fmt.Errorf("%w: manual node delete requires name and tag", ErrInvalidImport)
	}
	current, err := s.store.GetNodeSet(nodeSetName)
	if err != nil {
		return Result{}, err
	}
	nodes := make([]repository.NormalizedNode, 0, len(current.Nodes))
	removed := false
	for _, node := range current.Nodes {
		if node.Tag == tag {
			removed = true
			continue
		}
		nodes = append(nodes, node)
	}
	if !removed {
		return Result{}, repository.ErrNotFound
	}
	nodeSet, err := s.store.UpsertManagedNodeSet(repository.NodeSetResource{
		Metadata: repository.Metadata{
			Name:       nodeSetName,
			OriginType: repository.OriginManual,
		},
		Nodes: nodes,
	})
	if err != nil {
		return Result{}, err
	}
	if err := s.store.UpsertNodeCache(repository.NodeCacheUpsert{
		SourceType: repository.NodeCacheSourceManual,
		SourceID:   nodeSet.ID,
		NodeSetID:  nodeSet.ID,
		Nodes:      nodeSet.Nodes,
	}); err != nil {
		return Result{}, err
	}
	return Result{NodeSet: &nodeSet}, nil
}

func assignNodeSource(nodes []repository.NormalizedNode, source string) []repository.NormalizedNode {
	result := make([]repository.NormalizedNode, 0, len(nodes))
	for _, node := range nodes {
		node.Source = source
		result = append(result, node)
	}
	return result
}

func normalizeClashProxy(proxy map[string]any, index int) repository.NormalizedNode {
	nodeType := normalizeClashProxyType(stringValue(proxy["type"], "unknown"))
	transport := filterMap(proxy, "name", "type", "server", "port")

	normalizedTransport := map[string]any{}
	switch nodeType {
	case "shadowsocks":
		copyIfPresent(normalizedTransport, "method", transport["cipher"], transport["method"])
		copyIfPresent(normalizedTransport, "password", transport["password"])
		if plugin := stringValue(transport["plugin"], ""); plugin != "" {
			if plugin == "obfs" {
				plugin = "obfs-local"
			}
			normalizedTransport["plugin"] = plugin
		}
		copyIfPresent(normalizedTransport, "plugin_opts", normalizePluginOptions(transport["plugin-opts"]))
		copyIfPresent(normalizedTransport, "udp_over_tcp", transport["udp-over-tcp"])
	case "vmess":
		copyIfPresent(normalizedTransport, "uuid", transport["uuid"], transport["id"])
		copyIfPresent(normalizedTransport, "security", transport["cipher"], transport["security"])
		copyIfPresent(normalizedTransport, "alter_id", transport["alterId"])
		copyIfPresent(normalizedTransport, "packet_encoding", transport["packet-encoding"])
		copyIfPresent(normalizedTransport, "global_padding", transport["global-padding"])
		copyIfPresent(normalizedTransport, "authenticated_length", transport["authenticated-length"])
	case "vless":
		copyIfPresent(normalizedTransport, "uuid", transport["uuid"], transport["id"])
		copyIfPresent(normalizedTransport, "flow", transport["flow"])
		copyIfPresent(normalizedTransport, "packet_encoding", transport["packet-encoding"])
	case "trojan":
		copyIfPresent(normalizedTransport, "password", transport["password"])
	case "hysteria":
		copyIfPresent(normalizedTransport, "up_mbps", transport["up"], transport["up_mbps"])
		copyIfPresent(normalizedTransport, "down_mbps", transport["down"], transport["down_mbps"])
		copyIfPresent(normalizedTransport, "obfs", transport["obfs"])
		copyIfPresent(normalizedTransport, "auth", transport["auth"])
		copyIfPresent(normalizedTransport, "auth_str", transport["auth-str"])
	case "hysteria2":
		copyIfPresent(normalizedTransport, "up_mbps", transport["up"], transport["up_mbps"])
		copyIfPresent(normalizedTransport, "down_mbps", transport["down"], transport["down_mbps"])
		copyIfPresent(normalizedTransport, "password", transport["password"], transport["auth"])
		if obfsPassword := stringValue(transport["obfs-password"], ""); obfsPassword != "" {
			normalizedTransport["obfs"] = map[string]any{
				"type":     fallbackName(stringValue(transport["obfs"], ""), "salamander"),
				"password": obfsPassword,
			}
		}
	case "tuic":
		copyIfPresent(normalizedTransport, "uuid", transport["uuid"], transport["id"])
		copyIfPresent(normalizedTransport, "password", transport["password"], transport["token"])
		copyIfPresent(normalizedTransport, "congestion_control", transport["congestion-controller"])
		copyIfPresent(normalizedTransport, "udp_relay_mode", transport["udp-relay-mode"])
		copyIfPresent(normalizedTransport, "udp_over_stream", transport["udp-over-stream"])
		copyIfPresent(normalizedTransport, "zero_rtt_handshake", transport["reduce-rtt"])
		copyIfPresent(normalizedTransport, "heartbeat", transport["heartbeat-interval"])
	case "wireguard":
		copyIfPresent(normalizedTransport, "local_address", stringSlice(transport["ip"]))
		copyIfPresent(normalizedTransport, "private_key", transport["private-key"])
		copyIfPresent(normalizedTransport, "peer_public_key", transport["public-key"])
		copyIfPresent(normalizedTransport, "pre_shared_key", transport["pre-shared-key"])
		copyIfPresent(normalizedTransport, "reserved", transport["reserved"])
		copyIfPresent(normalizedTransport, "mtu", transport["mtu"])
	}
	normalizeClashCommonProxyFields(normalizedTransport, transport)

	if network := stringValue(transport["network"], ""); network != "" {
		switch network {
		case "tcp", "udp":
			normalizedTransport["network"] = network
		case "ws":
			normalizedTransport["transport"] = normalizeWSOpts(transport["ws-opts"])
		case "grpc":
			normalizedTransport["transport"] = normalizeGRPCOpts(transport["grpc-opts"])
		case "http", "h2":
			normalizedTransport["transport"] = normalizeHTTPOpts(transport["http-opts"], transport["h2-opts"])
		case "quic":
			normalizedTransport["transport"] = map[string]any{"type": "quic"}
		}
	}

	if tls := buildClashTLS(proxy); tls != nil {
		normalizedTransport["tls"] = tls
	}

	return withNodeDefaults(repository.NormalizedNode{
		ID:         repository.NewID("node"),
		Tag:        stringValue(proxy["name"], fmt.Sprintf("proxy-%d", index+1)),
		Type:       nodeType,
		Server:     stringValue(proxy["server"], ""),
		ServerPort: intValue(proxy["port"]),
		Source:     "clash-subscription",
		Transport:  normalizedTransport,
		Raw:        proxy,
	})
}

func normalizeClashCommonProxyFields(normalizedTransport map[string]any, transport map[string]any) {
	copyIfPresent(normalizedTransport, "detour", transport["detour"], transport["dialer-proxy"])
	copyIfPresent(normalizedTransport, "bind_interface", transport["bind_interface"], transport["interface-name"])
	copyIfPresent(normalizedTransport, "inet4_bind_address", transport["inet4_bind_address"])
	copyIfPresent(normalizedTransport, "inet6_bind_address", transport["inet6_bind_address"])
	copyIfPresent(normalizedTransport, "bind_address_no_port", transport["bind_address_no_port"])
	copyIfPresent(normalizedTransport, "routing_mark", transport["routing_mark"], transport["routing-mark"])
	copyIfPresent(normalizedTransport, "reuse_addr", transport["reuse_addr"])
	copyIfPresent(normalizedTransport, "netns", transport["netns"])
	copyIfPresent(normalizedTransport, "connect_timeout", transport["connect_timeout"])
	copyIfPresent(normalizedTransport, "tcp_fast_open", transport["tcp_fast_open"], transport["tfo"])
	copyIfPresent(normalizedTransport, "tcp_multi_path", transport["tcp_multi_path"], transport["mptcp"])
	copyIfPresent(normalizedTransport, "disable_tcp_keep_alive", transport["disable_tcp_keep_alive"])
	copyIfPresent(normalizedTransport, "tcp_keep_alive", transport["tcp_keep_alive"])
	copyIfPresent(normalizedTransport, "tcp_keep_alive_interval", transport["tcp_keep_alive_interval"])
	copyIfPresent(normalizedTransport, "udp_fragment", transport["udp_fragment"])
	copyIfPresent(normalizedTransport, "domain_resolver", transport["domain_resolver"])
	copyIfPresent(normalizedTransport, "network_strategy", transport["network_strategy"])
	copyIfPresent(normalizedTransport, "network_type", transport["network_type"])
	copyIfPresent(normalizedTransport, "fallback_network_type", transport["fallback_network_type"])
	copyIfPresent(normalizedTransport, "fallback_delay", transport["fallback_delay"])
	copyIfPresent(normalizedTransport, "domain_strategy", transport["domain_strategy"])
	copyIfPresent(normalizedTransport, "udp", transport["udp"])
	copyIfPresent(normalizedTransport, "mihomo_ip_version", transport["ip-version"])
	copyIfPresent(normalizedTransport, "multiplex", normalizeClashSmux(transport["smux"]))
	if _, exists := normalizedTransport["domain_strategy"]; !exists {
		strategy := singBoxDomainStrategyFromMihomoIPVersion(transport["ip-version"])
		if strategy == "" {
			return
		}
		normalizedTransport["domain_strategy"] = strategy
	}
}

func singBoxDomainStrategyFromMihomoIPVersion(value any) string {
	switch strings.ToLower(strings.TrimSpace(stringValue(value, ""))) {
	case "ipv4":
		return "ipv4_only"
	case "ipv6":
		return "ipv6_only"
	case "ipv4-prefer":
		return "prefer_ipv4"
	case "ipv6-prefer":
		return "prefer_ipv6"
	default:
		return ""
	}
}

func normalizeClashSmux(value any) map[string]any {
	smux, ok := value.(map[string]any)
	if !ok || len(smux) == 0 {
		return nil
	}
	multiplex := map[string]any{}
	copyIfPresent(multiplex, "enabled", smux["enabled"])
	copyIfPresent(multiplex, "protocol", smux["protocol"])
	copyIfPresent(multiplex, "max_connections", smux["max-connections"])
	copyIfPresent(multiplex, "min_streams", smux["min-streams"])
	copyIfPresent(multiplex, "max_streams", smux["max-streams"])
	copyIfPresent(multiplex, "padding", smux["padding"])
	copyIfPresent(multiplex, "brutal", normalizeClashSmuxBrutal(smux["brutal-opts"]))
	if len(multiplex) == 0 {
		return nil
	}
	return multiplex
}

func normalizeClashSmuxBrutal(value any) map[string]any {
	opts, ok := value.(map[string]any)
	if !ok || len(opts) == 0 {
		return nil
	}
	brutal := map[string]any{}
	copyIfPresent(brutal, "enabled", opts["enabled"])
	copyIfPresent(brutal, "up_mbps", opts["up"])
	copyIfPresent(brutal, "down_mbps", opts["down"])
	if len(brutal) == 0 {
		return nil
	}
	return brutal
}

func normalizeClashRules(lines []string) ([]repository.NormalizedRule, []string) {
	result := make([]repository.NormalizedRule, 0, len(lines))
	warnings := []string{}

	for index, line := range lines {
		rule, warning, err := parseClashRule(line)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("rule %d was skipped: %v", index+1, err))
			continue
		}
		if strings.TrimSpace(rule.Outbound) == "" {
			warnings = append(warnings, fmt.Sprintf("rule %d was skipped because target outbound is empty", index+1))
			continue
		}
		if len(result) == 0 {
			result = append(result, rule)
			continue
		}

		merged, ok := mergeContiguousAtomicRules(result[len(result)-1], rule)
		if ok {
			result[len(result)-1] = merged
			continue
		}
		result = append(result, rule)
	}
	return result, warnings
}

func mergeContiguousAtomicRules(left repository.NormalizedRule, right repository.NormalizedRule) (repository.NormalizedRule, bool) {
	if left.Type != "" || right.Type != "" {
		return repository.NormalizedRule{}, false
	}
	if left.Mode != "" || right.Mode != "" || len(left.Rules) > 0 || len(right.Rules) > 0 {
		return repository.NormalizedRule{}, false
	}
	leftAction := strings.TrimSpace(left.Action)
	if leftAction == "" {
		leftAction = "route"
	}
	rightAction := strings.TrimSpace(right.Action)
	if rightAction == "" {
		rightAction = "route"
	}
	if leftAction != rightAction {
		return repository.NormalizedRule{}, false
	}
	if left.Outbound != right.Outbound {
		return repository.NormalizedRule{}, false
	}
	if len(left.Fields) == 0 || len(right.Fields) == 0 {
		return repository.NormalizedRule{}, false
	}
	if mergeRuleFieldGroup(left.Fields) != mergeRuleFieldGroup(right.Fields) {
		return repository.NormalizedRule{}, false
	}

	merged := left
	merged.Fields = cloneRuleFields(left.Fields)
	for key, value := range right.Fields {
		merged.Fields[key] = mergeRuleFieldValue(merged.Fields[key], value)
	}
	merged.Raw = mergeRuleRaw(left.Raw, right.Raw)
	return merged, true
}

func mergeRuleRaw(left []string, right []string) []string {
	merged := make([]string, 0, len(left)+len(right))
	merged = append(merged, left...)
	merged = append(merged, right...)
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func mergeRuleFieldGroup(fields map[string]any) string {
	if len(fields) == 0 || fields["invert"] != nil {
		return ""
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}

	group := ""
	for _, key := range keys {
		current := classifyRuleField(key)
		if current == "" {
			return ""
		}
		if group == "" {
			group = current
			continue
		}
		if group != current {
			return ""
		}
	}
	return group
}

func classifyRuleField(key string) string {
	switch key {
	case "domain", "domain_suffix", "domain_keyword", "domain_regex", "geosite", "geoip", "ip_cidr":
		return "destination"
	case "source_geoip", "source_ip_cidr":
		return "source"
	case "port", "port_range":
		return "port"
	case "source_port", "source_port_range":
		return "source_port"
	case "auth_user", "inbound", "process_path", "process_path_regex", "process_name", "user_id", "network", "rule_set":
		return key
	default:
		return ""
	}
}

func cloneRuleFields(fields map[string]any) map[string]any {
	cloned := make(map[string]any, len(fields))
	for key, value := range fields {
		switch typed := value.(type) {
		case []string:
			cloned[key] = append([]string(nil), typed...)
		case []any:
			cloned[key] = append([]any(nil), typed...)
		default:
			cloned[key] = value
		}
	}
	return cloned
}

func mergeRuleFieldValue(left any, right any) any {
	switch leftTyped := left.(type) {
	case []string:
		switch rightTyped := right.(type) {
		case []string:
			return append(append([]string(nil), leftTyped...), rightTyped...)
		case []any:
			merged := append([]string(nil), leftTyped...)
			for _, item := range rightTyped {
				if itemString, ok := item.(string); ok {
					merged = append(merged, itemString)
				}
			}
			return merged
		}
	case []any:
		switch rightTyped := right.(type) {
		case []any:
			return append(append([]any(nil), leftTyped...), rightTyped...)
		case []string:
			merged := append([]any(nil), leftTyped...)
			for _, item := range rightTyped {
				merged = append(merged, item)
			}
			return merged
		}
	}
	return right
}

func parseClashRule(line string) (repository.NormalizedRule, string, error) {
	raw := strings.TrimSpace(line)
	if raw == "" {
		return repository.NormalizedRule{}, "", fmt.Errorf("empty rule")
	}
	parts := splitTopLevel(raw, ',')
	if len(parts) < 2 {
		return repository.NormalizedRule{}, "", fmt.Errorf("invalid clash rule %q", raw)
	}

	ruleType := strings.ToUpper(strings.TrimSpace(parts[0]))
	args, outbound, extras, err := splitClashRuleParts(ruleType, parts)
	if err != nil {
		return repository.NormalizedRule{}, "", err
	}
	warning := clashRuleExtraWarning(extras)

	switch ruleType {
	case "MATCH":
		return newSingBoxRule(map[string]any{}, outbound, raw), warning, nil
	case "DOMAIN":
		return newSingBoxRule(map[string]any{"domain": trimStrings(args)}, outbound, raw), warning, nil
	case "DOMAIN-SUFFIX":
		return newSingBoxRule(map[string]any{"domain_suffix": trimStrings(args)}, outbound, raw), warning, nil
	case "DOMAIN-KEYWORD":
		return newSingBoxRule(map[string]any{"domain_keyword": trimStrings(args)}, outbound, raw), warning, nil
	case "DOMAIN-REGEX":
		return newSingBoxRule(map[string]any{"domain_regex": trimStrings(args)}, outbound, raw), warning, nil
	case "DOMAIN-WILDCARD":
		return newSingBoxRule(map[string]any{"domain_regex": wildcardListToRegex(trimStrings(args))}, outbound, raw), appendWarning(warning, "DOMAIN-WILDCARD was converted to domain_regex"), nil
	case "GEOSITE":
		return newSingBoxRule(map[string]any{"geosite": trimStrings(args)}, outbound, raw), warning, nil
	case "IP-CIDR", "IP-CIDR6":
		fieldName := "ip_cidr"
		if hasClashRuleExtra(extras, "src") {
			fieldName = "source_ip_cidr"
		}
		return newSingBoxRule(map[string]any{fieldName: trimStrings(args)}, outbound, raw), warning, nil
	case "SRC-IP-CIDR":
		return newSingBoxRule(map[string]any{"source_ip_cidr": trimStrings(args)}, outbound, raw), warning, nil
	case "GEOIP":
		return newSingBoxRule(map[string]any{"geoip": trimStrings(args)}, outbound, raw), appendWarning(warning, "GEOIP is deprecated in sing-box and was kept as geoip"), nil
	case "SRC-GEOIP":
		return newSingBoxRule(map[string]any{"source_geoip": trimStrings(args)}, outbound, raw), appendWarning(warning, "SRC-GEOIP is deprecated in sing-box and was kept as source_geoip"), nil
	case "DST-PORT":
		return newSingBoxRule(portFields("port", trimStrings(args)), outbound, raw), warning, nil
	case "SRC-PORT":
		return newSingBoxRule(portFields("source_port", trimStrings(args)), outbound, raw), warning, nil
	case "IN-USER":
		return newSingBoxRule(map[string]any{"auth_user": splitBySlash(args)}, outbound, raw), warning, nil
	case "IN-NAME":
		return newSingBoxRule(map[string]any{"inbound": trimStrings(args)}, outbound, raw), warning, nil
	case "IN-TYPE":
		return newSingBoxRule(map[string]any{"inbound": clashInboundTypesToTags(args)}, outbound, raw), appendWarning(warning, "IN-TYPE was converted to static inbound tags"), nil
	case "PROCESS-PATH":
		return newSingBoxRule(map[string]any{"process_path": trimStrings(args)}, outbound, raw), warning, nil
	case "PROCESS-PATH-REGEX":
		return newSingBoxRule(map[string]any{"process_path_regex": trimStrings(args)}, outbound, raw), warning, nil
	case "PROCESS-PATH-WILDCARD":
		return newSingBoxRule(map[string]any{"process_path_regex": wildcardListToRegex(trimStrings(args))}, outbound, raw), appendWarning(warning, "PROCESS-PATH-WILDCARD was converted to process_path_regex"), nil
	case "PROCESS-NAME":
		return newSingBoxRule(map[string]any{"process_name": trimStrings(args)}, outbound, raw), warning, nil
	case "UID":
		return newSingBoxRule(map[string]any{"user_id": trimStrings(args)}, outbound, raw), warning, nil
	case "NETWORK":
		return newSingBoxRule(map[string]any{"network": lowerStrings(args)}, outbound, raw), warning, nil
	case "RULE-SET":
		return newSingBoxRule(map[string]any{"rule_set": trimStrings(args)}, outbound, raw), warning, nil
	case "AND", "OR":
		logicalRules, err := parseClashLogicalArgs(args)
		if err != nil {
			return repository.NormalizedRule{}, "", err
		}
		return repository.NormalizedRule{
			ID:       repository.NewID("rule"),
			Type:     "logical",
			Mode:     strings.ToLower(ruleType),
			Rules:    logicalRules,
			Action:   "route",
			Outbound: outbound,
			Raw:      []string{raw},
		}, warning, nil
	case "NOT":
		logicalRules, err := parseClashLogicalArgs(args)
		if err != nil {
			return repository.NormalizedRule{}, "", err
		}
		return repository.NormalizedRule{
			ID:       repository.NewID("rule"),
			Type:     "logical",
			Mode:     "or",
			Rules:    logicalRules,
			Action:   "route",
			Outbound: outbound,
			Fields:   map[string]any{"invert": true},
			Raw:      []string{raw},
		}, warning, nil
	default:
		return repository.NormalizedRule{}, "", fmt.Errorf("unsupported clash rule type %q", ruleType)
	}
}

func splitClashRuleParts(ruleType string, parts []string) ([]string, string, []string, error) {
	if ruleType == "MATCH" {
		if len(parts) < 2 {
			return nil, "", nil, fmt.Errorf("invalid clash rule %q", strings.Join(parts, ","))
		}
		return nil, normalizeClashRuleOutbound(parts[1]), trimStrings(parts[2:]), nil
	}
	if len(parts) < 3 {
		return nil, "", nil, fmt.Errorf("invalid clash rule %q", strings.Join(parts, ","))
	}
	return []string{strings.TrimSpace(parts[1])}, normalizeClashRuleOutbound(parts[2]), trimStrings(parts[3:]), nil
}

func hasClashRuleExtra(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func clashRuleExtraWarning(extras []string) string {
	warnings := []string{}
	if hasClashRuleExtra(extras, "no-resolve") {
		warnings = append(warnings, "no-resolve was ignored during sing-box conversion")
	}
	return strings.Join(warnings, "; ")
}

func appendWarning(base string, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	if base == "" {
		return extra
	}
	if extra == "" {
		return base
	}
	return base + "; " + extra
}

func parseClashLogicalArgs(args []string) ([]repository.NormalizedRule, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("logical rule has empty payload")
	}
	payload := strings.TrimSpace(strings.Join(args, ","))
	payload = strings.TrimPrefix(payload, "(")
	payload = strings.TrimSuffix(payload, ")")
	branches := splitLogicalBranches(payload)
	result := make([]repository.NormalizedRule, 0, len(branches))
	for _, branch := range branches {
		rule, _, err := parseClashRule(branch + ",DIRECT")
		if err != nil {
			return nil, err
		}
		rule.Action = ""
		rule.Outbound = ""
		result = append(result, rule)
	}
	return result, nil
}

func newSingBoxRule(fields map[string]any, outbound string, raw string) repository.NormalizedRule {
	return repository.NormalizedRule{
		ID:       repository.NewID("rule"),
		Action:   "route",
		Outbound: outbound,
		Fields:   fields,
		Raw:      []string{raw},
	}
}

func splitTopLevel(value string, sep rune) []string {
	result := []string{}
	current := strings.Builder{}
	depth := 0
	for _, char := range value {
		switch char {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if char == sep && depth == 0 {
			result = append(result, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}
		current.WriteRune(char)
	}
	if current.Len() > 0 {
		result = append(result, strings.TrimSpace(current.String()))
	}
	return result
}

func splitLogicalBranches(value string) []string {
	parts := []string{}
	current := strings.Builder{}
	depth := 0
	for _, char := range value {
		if char == '(' {
			if depth > 0 {
				current.WriteRune(char)
			}
			depth++
			continue
		}
		if char == ')' {
			depth--
			if depth > 0 {
				current.WriteRune(char)
			}
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(current.String()))
				current.Reset()
			}
			continue
		}
		if depth > 0 {
			current.WriteRune(char)
		}
	}
	return parts
}

func trimStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func lowerStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range trimStrings(values) {
		result = append(result, strings.ToLower(value))
	}
	return result
}

func splitBySlash(values []string) []string {
	result := []string{}
	for _, value := range values {
		for _, part := range strings.Split(value, "/") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				result = append(result, trimmed)
			}
		}
	}
	return result
}

func wildcardListToRegex(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		replacer := strings.NewReplacer(".", "\\.", "*", ".*", "?", ".")
		result = append(result, "^"+replacer.Replace(value)+"$")
	}
	return result
}

func portFields(key string, values []string) map[string]any {
	ports := []string{}
	ranges := []string{}
	for _, value := range values {
		if strings.Contains(value, "-") {
			ranges = append(ranges, value)
		} else {
			ports = append(ports, value)
		}
	}
	fields := map[string]any{}
	if len(ports) > 0 {
		fields[key] = ports
	}
	if len(ranges) > 0 {
		fields[key+"_range"] = ranges
	}
	return fields
}

func clashInboundTypesToTags(values []string) []string {
	result := []string{}
	for _, value := range splitBySlash(values) {
		switch strings.ToUpper(value) {
		case "SOCKS":
			result = append(result, "socks-in")
		case "HTTP":
			result = append(result, "http-in")
		case "MIXED":
			result = append(result, "mixed-in")
		}
	}
	return result
}

func filterExtraRuleArgs(values []string) []string {
	result := []string{}
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "no-resolve", "src":
			continue
		default:
			result = append(result, value)
		}
	}
	return result
}

func normalizeClashRuleOutbound(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DIRECT":
		return "DIRECT"
	case "REJECT", "REJECT-DROP":
		return "REJECT"
	default:
		return strings.TrimSpace(value)
	}
}

func normalizeClashProxyType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ss":
		return "shadowsocks"
	case "socks5", "socks4", "socks4a":
		return "socks"
	case "hy2":
		return "hysteria2"
	case "wg":
		return "wireguard"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func buildClashTLS(proxy map[string]any) map[string]any {
	enabled := false
	tls := map[string]any{}
	if value, ok := proxy["tls"].(bool); ok && value {
		enabled = true
	}
	if serverName := stringValue(proxy["servername"], ""); serverName != "" {
		tls["server_name"] = serverName
		enabled = true
	}
	if serverName := stringValue(proxy["sni"], ""); serverName != "" {
		tls["server_name"] = serverName
		enabled = true
	}
	if insecure, ok := proxy["skip-cert-verify"].(bool); ok {
		tls["insecure"] = insecure
		enabled = true
	}
	if fingerprint := stringValue(proxy["client-fingerprint"], ""); fingerprint != "" {
		tls["utls"] = map[string]any{
			"enabled":     true,
			"fingerprint": fingerprint,
		}
		enabled = true
	}
	if realityOpts, ok := proxy["reality-opts"].(map[string]any); ok {
		reality := map[string]any{"enabled": true}
		copyIfPresent(reality, "public_key", realityOpts["public-key"])
		copyIfPresent(reality, "short_id", realityOpts["short-id"])
		if len(reality) > 1 {
			tls["reality"] = reality
			enabled = true
		}
	}
	if !enabled {
		return nil
	}
	tls["enabled"] = true
	return tls
}

func normalizeWSOpts(raw any) map[string]any {
	opts, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{"type": "ws"}
	}
	result := map[string]any{"type": "ws"}
	copyIfPresent(result, "path", opts["path"])
	copyIfPresent(result, "headers", opts["headers"])
	copyIfPresent(result, "max_early_data", opts["max-early-data"])
	copyIfPresent(result, "early_data_header_name", opts["early-data-header-name"])
	return result
}

func normalizeGRPCOpts(raw any) map[string]any {
	opts, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{"type": "grpc"}
	}
	result := map[string]any{"type": "grpc"}
	copyIfPresent(result, "service_name", opts["grpc-service-name"])
	return result
}

func normalizeHTTPOpts(values ...any) map[string]any {
	result := map[string]any{"type": "http"}
	for _, raw := range values {
		opts, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		copyIfPresent(result, "host", opts["host"])
		copyIfPresent(result, "path", opts["path"])
		copyIfPresent(result, "method", opts["method"])
		copyIfPresent(result, "headers", opts["headers"])
	}
	return result
}

func parseNodeURI(raw string) (repository.NormalizedNode, string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return repository.NormalizedNode{}, "", fmt.Errorf("%w: %s", ErrInvalidImport, err)
	}
	switch parsed.Scheme {
	case "trojan", "vless":
		host, port, err := net.SplitHostPort(parsed.Host)
		if err != nil {
			return repository.NormalizedNode{}, "", fmt.Errorf("%w: invalid host %q", ErrInvalidImport, parsed.Host)
		}
		portValue, err := strconv.Atoi(port)
		if err != nil {
			return repository.NormalizedNode{}, "", fmt.Errorf("%w: invalid port %q", ErrInvalidImport, port)
		}
		return withNodeDefaults(repository.NormalizedNode{
			Tag:        strings.TrimPrefix(parsed.Fragment, "#"),
			Type:       parsed.Scheme,
			Server:     host,
			ServerPort: portValue,
			Source:     "plain-node",
			Raw: map[string]any{
				"uri": raw,
			},
		}), "", nil
	case "vmess":
		payload := strings.TrimSpace(strings.TrimPrefix(raw, "vmess://"))
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(payload)
			if err != nil {
				return repository.NormalizedNode{}, "", fmt.Errorf("%w: invalid vmess payload", ErrInvalidImport)
			}
		}
		var body map[string]any
		if err := json.Unmarshal(decoded, &body); err != nil {
			return repository.NormalizedNode{}, "", fmt.Errorf("%w: invalid vmess json", ErrInvalidImport)
		}
		return withNodeDefaults(repository.NormalizedNode{
			Tag:        stringValue(body["ps"], "vmess-node"),
			Type:       "vmess",
			Server:     stringValue(body["add"], ""),
			ServerPort: intFromStringValue(body["port"]),
			Source:     "plain-node",
			Raw:        body,
		}), "", nil
	case "ss":
		return parseShadowsocksNode(raw)
	default:
		return repository.NormalizedNode{}, "", fmt.Errorf("%w: unsupported node scheme %q", ErrInvalidImport, parsed.Scheme)
	}
}

func parseShadowsocksNode(raw string) (repository.NormalizedNode, string, error) {
	payload := strings.TrimPrefix(raw, "ss://")
	name := ""
	if fragmentIndex := strings.Index(payload, "#"); fragmentIndex >= 0 {
		name = payload[fragmentIndex+1:]
		payload = payload[:fragmentIndex]
	}
	if atIndex := strings.Index(payload, "@"); atIndex < 0 {
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(payload)
			if err != nil {
				return repository.NormalizedNode{}, "", fmt.Errorf("%w: invalid ss payload", ErrInvalidImport)
			}
		}
		payload = string(decoded)
	}
	parts := strings.Split(payload, "@")
	if len(parts) != 2 {
		return repository.NormalizedNode{}, "", fmt.Errorf("%w: invalid ss payload", ErrInvalidImport)
	}
	host, port, err := net.SplitHostPort(parts[1])
	if err != nil {
		return repository.NormalizedNode{}, "", fmt.Errorf("%w: invalid ss host", ErrInvalidImport)
	}
	portValue, err := strconv.Atoi(port)
	if err != nil {
		return repository.NormalizedNode{}, "", fmt.Errorf("%w: invalid ss port", ErrInvalidImport)
	}
	return withNodeDefaults(repository.NormalizedNode{
		Tag:        fallbackName(name, host),
		Type:       "ss",
		Server:     host,
		ServerPort: portValue,
		Source:     "plain-node",
		Raw: map[string]any{
			"credentials": parts[0],
			"uri":         raw,
		},
	}), "stored ss credentials in raw metadata only", nil
}

func withNodeDefaults(node repository.NormalizedNode) repository.NormalizedNode {
	if node.ID == "" {
		node.ID = repository.NewID("node")
	}
	if node.Tag == "" {
		node.Tag = node.Type + "-" + node.ID
	}
	return node
}

func filterMap(input map[string]any, exclude ...string) map[string]any {
	blocked := map[string]struct{}{}
	for _, key := range exclude {
		blocked[key] = struct{}{}
	}
	output := map[string]any{}
	for key, value := range input {
		if _, ok := blocked[key]; ok {
			continue
		}
		output[key] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func copyIfPresent(target map[string]any, key string, values ...any) {
	for _, value := range values {
		switch typed := value.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
			target[key] = strings.TrimSpace(typed)
			return
		case []string:
			if len(typed) == 0 {
				continue
			}
			target[key] = typed
			return
		case map[string]any:
			if len(typed) == 0 {
				continue
			}
			target[key] = typed
			return
		default:
			target[key] = typed
			return
		}
	}
}

func normalizePluginOptions(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return strings.TrimSpace(typed)
	case map[string]any:
		if len(typed) == 0 {
			return nil
		}
		parts := make([]string, 0, len(typed))
		for key, value := range typed {
			parts = append(parts, fmt.Sprintf("%s=%v", key, value))
		}
		return strings.Join(parts, ";")
	default:
		return nil
	}
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if stringItem, ok := item.(string); ok {
			result = append(result, stringItem)
		}
	}
	return result
}

func stringValue(value any, fallback string) string {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return fallback
	}
	return strings.TrimSpace(text)
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		number, _ := strconv.Atoi(strings.TrimSpace(typed))
		return number
	default:
		return 0
	}
}

func intFromStringValue(value any) int {
	return intValue(value)
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func fallbackName(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "Imported resource"
}
