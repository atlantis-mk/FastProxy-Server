package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/atlantis-mk/FastProxy-Server/internal/core"
	"github.com/atlantis-mk/FastProxy-Server/internal/httpjson"
	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
	"gopkg.in/yaml.v3"
)

const defaultExternalController = "127.0.0.1:9090"
const singBoxFakeIPServerTag = "fakeip"
const singBoxBuiltInGeoIPRepositoryID = "metacubex-meta-rules-dat"

func (s *Server) handleRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	status := s.sv.Status()
	active, err := s.activeRuntimeProfile()
	if err == nil && status.Core != "" && active.SelectedCore != status.Core {
		httpjson.Write(w, http.StatusOK, map[string]any{
			"core":           status.Core,
			"selectedCore":   active.SelectedCore,
			"state":          status.State,
			"startedAt":      status.StartedAt,
			"error":          status.Error,
			"pendingRestart": true,
		})
		return
	}
	httpjson.Write(w, http.StatusOK, status)
}

func (s *Server) handleRuntimeStart(w http.ResponseWriter, r *http.Request) {
	if err := s.startActiveRuntime(r.Context()); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "runtime_start_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, s.sv.Status())
}

func (s *Server) handleRuntimeStop(w http.ResponseWriter, r *http.Request) {
	if err := s.sv.Stop(r.Context()); err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "runtime_stop_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, s.sv.Status())
}

func (s *Server) handleRuntimeRestart(w http.ResponseWriter, r *http.Request) {
	if err := s.startActiveRuntime(r.Context()); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "runtime_restart_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, s.sv.Status())
}

func (s *Server) handleRuntimeRestartAndApply(w http.ResponseWriter, r *http.Request) {
	if err := s.startActiveRuntime(r.Context()); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "runtime_apply_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, s.sv.Status())
}

func (s *Server) handleRuntimeControllerProxy(w http.ResponseWriter, r *http.Request) {
	target, secret, err := s.runtimeControllerTarget()
	if err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "runtime_controller_unavailable", err.Error())
		return
	}

	prefix := "/api/runtime/controller"
	proxyPath := strings.TrimPrefix(r.URL.Path, prefix)
	if proxyPath == "" {
		proxyPath = "/"
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = singleJoiningSlash(target.Path, proxyPath)
		req.URL.RawPath = ""
		req.URL.RawQuery = r.URL.RawQuery
		req.Host = target.Host
		req.Header.Del("Authorization")
		if secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		httpjson.WriteError(w, http.StatusBadGateway, "runtime_controller_proxy_failed", err.Error())
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) startActiveRuntime(ctx context.Context) error {
	active, err := s.activeRuntimeProfile()
	if err != nil {
		return err
	}
	adapter, err := core.For(active.SelectedCore)
	if err != nil {
		return err
	}
	binaryPath, err := s.binaryFor(ctx, active.SelectedCore)
	if err != nil {
		return err
	}
	compiled, err := s.compileRuntime(ctx, active, binaryPath)
	if err != nil {
		return err
	}
	configPath := adapter.GeneratedConfigPath(s.cfg.DataDir)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(configPath, compiled.Data, 0o644); err != nil {
		return err
	}
	if err := adapter.Validate(ctx, binaryPath, configPath); err != nil {
		return err
	}
	return s.sv.Restart(ctx, adapter, binaryPath, configPath, core.RuntimeConfig{
		ExternalController: compiled.ExternalController,
		Secret:             compiled.Secret,
	})
}

func (s *Server) activeRuntimeProfile() (repository.ProfileResource, error) {
	state, err := s.store.State()
	if err != nil {
		return repository.ProfileResource{}, err
	}
	if state.ActiveProfileID == "" {
		return repository.ProfileResource{}, repository.ErrNotFound
	}
	return s.store.GetProfile(state.ActiveProfileID)
}

func (s *Server) binaryFor(ctx context.Context, coreName repository.Core) (string, error) {
	configuredPath := s.cfg.MihomoBinaryPath
	if coreName == repository.CoreSingBox {
		configuredPath = s.cfg.SingBoxBinaryPath
	}
	return core.ResolveBinary(ctx, s.cfg.DataDir, coreName, configuredPath)
}

type compiledRuntime struct {
	Data               []byte
	ExternalController string
	Secret             string
}

type runtimeCompileOptions struct {
	SingBoxDNS14 bool
}

func (s *Server) compileRuntime(ctx context.Context, active repository.ProfileResource, binaryPath string) (compiledRuntime, error) {
	bootstrap, err := s.store.Bootstrap()
	if err != nil {
		return compiledRuntime{}, err
	}
	nodes := selectedNodes(active, bootstrap.NodeSets)
	groups := selectedGroups(active, bootstrap.GroupSets)
	rules := selectedRules(active, bootstrap.RoutingRuleSets)
	if len(active.RuleSetIDs) > 0 {
		if len(active.NodeSetIDs) == 0 {
			nodes = relatedNodes(active, bootstrap.NodeSets, bootstrap.RoutingRuleSets)
		}
		if len(active.GroupSetIDs) == 0 {
			groups = relatedGroups(active, bootstrap.GroupSets, bootstrap.RoutingRuleSets)
		}
	}
	externalController := stringField(bootstrap.Config.Fields, "externalController", defaultExternalController)
	if strings.TrimSpace(externalController) == "" {
		externalController = defaultExternalController
	}
	secret := stringField(bootstrap.Config.Fields, "secret", "")

	switch active.SelectedCore {
	case repository.CoreMihomo:
		data, err := marshalMihomoRuntimeConfig(mihomoRuntimeConfig(bootstrap.Config, nodes, groups, rules, externalController))
		return compiledRuntime{Data: data, ExternalController: externalController, Secret: secret}, err
	case repository.CoreSingBox:
		data, err := json.MarshalIndent(singBoxRuntimeConfig(bootstrap.Config, nodes, groups, rules, bootstrap.SingBoxRuleSets, bootstrap.RuleSourceRepositories, externalController, runtimeCompileOptions{
			SingBoxDNS14: singBoxSupportsDNS14(ctx, binaryPath),
		}), "", "  ")
		return compiledRuntime{Data: data, ExternalController: externalController, Secret: secret}, err
	default:
		return compiledRuntime{}, fmt.Errorf("unsupported core %q", active.SelectedCore)
	}
}

func singBoxSupportsDNS14(ctx context.Context, binaryPath string) bool {
	output, err := exec.CommandContext(ctx, binaryPath, "version").Output()
	if err != nil {
		return false
	}
	fields := strings.Fields(string(output))
	for index, field := range fields {
		if field == "version" && index+1 < len(fields) {
			return semanticVersionAtLeast(fields[index+1], 1, 14)
		}
	}
	return false
}

func semanticVersionAtLeast(version string, major int, minor int) bool {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	parsedMajor, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minorPart := parts[1]
	if cut := strings.IndexFunc(minorPart, func(r rune) bool { return r < '0' || r > '9' }); cut >= 0 {
		minorPart = minorPart[:cut]
	}
	parsedMinor, err := strconv.Atoi(minorPart)
	if err != nil {
		return false
	}
	if parsedMajor != major {
		return parsedMajor > major
	}
	return parsedMinor >= minor
}

func (s *Server) runtimeControllerTarget() (*url.URL, string, error) {
	bootstrap, err := s.store.Bootstrap()
	if err != nil {
		return nil, "", err
	}
	externalController := stringField(bootstrap.Config.Fields, "externalController", defaultExternalController)
	if strings.TrimSpace(externalController) == "" {
		externalController = defaultExternalController
	}
	target, err := parseRuntimeControllerTarget(externalController)
	if err != nil {
		return nil, "", err
	}
	return target, strings.TrimSpace(stringField(bootstrap.Config.Fields, "secret", "")), nil
}

func parseRuntimeControllerTarget(externalController string) (*url.URL, error) {
	raw := strings.TrimSpace(externalController)
	if raw == "" {
		raw = defaultExternalController
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	target, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if target.Host == "" {
		return nil, fmt.Errorf("external controller host is required")
	}
	host := target.Hostname()
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = "127.0.0.1"
	}
	if port := target.Port(); port != "" {
		target.Host = net.JoinHostPort(host, port)
	} else {
		target.Host = host
	}
	return target, nil
}

func singleJoiningSlash(left string, right string) string {
	leftSlash := strings.HasSuffix(left, "/")
	rightSlash := strings.HasPrefix(right, "/")
	switch {
	case leftSlash && rightSlash:
		return left + right[1:]
	case !leftSlash && !rightSlash:
		return left + "/" + right
	default:
		return left + right
	}
}

func selectedNodes(active repository.ProfileResource, nodeSets []repository.NodeSetResource) []repository.NormalizedNode {
	selected := map[string]bool{}
	for _, id := range active.NodeSetIDs {
		selected[id] = true
	}
	var nodes []repository.NormalizedNode
	for _, set := range nodeSets {
		if selected[set.ID] {
			nodes = append(nodes, set.Nodes...)
		}
	}
	return nodes
}

func selectedGroups(active repository.ProfileResource, groupSets []repository.GroupSetResource) []repository.NormalizedGroup {
	selected := map[string]bool{}
	for _, id := range active.GroupSetIDs {
		selected[id] = true
	}
	var groups []repository.NormalizedGroup
	for _, set := range groupSets {
		if selected[set.ID] {
			groups = append(groups, set.Groups...)
		}
	}
	return groups
}

func selectedRules(active repository.ProfileResource, ruleSets []repository.RuleSetResource) []repository.NormalizedRule {
	selected := map[string]bool{}
	for _, id := range active.RuleSetIDs {
		selected[id] = true
	}
	var rules []repository.NormalizedRule
	for _, set := range ruleSets {
		if selected[set.ID] {
			rules = append(rules, set.Rules...)
		}
	}
	return rules
}

func relatedNodes(active repository.ProfileResource, nodeSets []repository.NodeSetResource, ruleSets []repository.RuleSetResource) []repository.NormalizedNode {
	names := relatedRuleSetNames(active, ruleSets)
	var nodes []repository.NormalizedNode
	for _, set := range nodeSets {
		if names[set.ID] || names[set.Name] {
			nodes = append(nodes, set.Nodes...)
		}
	}
	return nodes
}

func relatedGroups(active repository.ProfileResource, groupSets []repository.GroupSetResource, ruleSets []repository.RuleSetResource) []repository.NormalizedGroup {
	names := relatedRuleSetNames(active, ruleSets)
	var groups []repository.NormalizedGroup
	for _, set := range groupSets {
		if names[set.ID] || names[set.Name] {
			groups = append(groups, set.Groups...)
		}
	}
	return groups
}

func relatedRuleSetNames(active repository.ProfileResource, ruleSets []repository.RuleSetResource) map[string]bool {
	selected := map[string]bool{}
	for _, id := range active.RuleSetIDs {
		selected[id] = true
	}
	for _, set := range ruleSets {
		if selected[set.ID] {
			selected[set.Name] = true
		}
	}
	return selected
}

func marshalMihomoRuntimeConfig(config map[string]any) ([]byte, error) {
	trailingKeys := []string{"proxy-groups", "proxies", "rules"}
	trailing := map[string]bool{}
	for _, key := range trailingKeys {
		trailing[key] = true
	}

	keys := make([]string, 0, len(config))
	for key := range config {
		if !trailing[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range trailingKeys {
		if _, ok := config[key]; ok {
			keys = append(keys, key)
		}
	}

	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, key := range keys {
		valueNode := &yaml.Node{}
		if err := valueNode.Encode(config[key]); err != nil {
			return nil, err
		}
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, valueNode)
	}
	return yaml.Marshal(node)
}

func mihomoRuntimeConfig(config repository.GlobalConfig, nodes []repository.NormalizedNode, groups []repository.NormalizedGroup, rules []repository.NormalizedRule, externalController string) map[string]any {
	fields := config.Fields
	result := map[string]any{
		"mode":                stringField(fields, "mode", "rule"),
		"unified-delay":       boolField(fields, "unifiedDelay", false),
		"ipv6":                boolField(fields, "ipv6", false),
		"allow-lan":           boolField(fields, "allowLan", true),
		"bind-address":        stringField(fields, "bindAddress", "*"),
		"external-controller": externalController,
		"log-level":           stringField(fields, "logLevel", "info"),
		"clash-for-android":   map[string]any{"append-system-dns": false},
		"experimental":        map[string]any{"sniff-tls-sni": true},
		"proxies":             mihomoProxies(nodes),
		"proxy-groups":        mihomoGroups(groups),
		"rules":               mihomoRules(rules),
		"dns":                 mihomoDNS(config),
		"geox-url":            mihomoGeoxURL(config),
	}
	setStrings(result, "lan-allowed-ips", splitLines(stringField(fields, "lanAllowedIps", "")))
	setStrings(result, "lan-disallowed-ips", splitLines(stringField(fields, "lanDisallowedIps", "")))
	setStrings(result, "authentication", mihomoAuthentication(config))
	setStrings(result, "skip-auth-prefixes", splitLines(stringField(fields, "skipAuthPrefixes", "")))
	setString(result, "external-controller-unix", stringField(fields, "externalControllerUnix", ""))
	setString(result, "external-controller-pipe", stringField(fields, "externalControllerPipe", ""))
	setString(result, "external-controller-tls", stringField(fields, "externalControllerTls", ""))
	setString(result, "external-doh-server", stringField(fields, "externalDohServer", ""))
	setString(result, "secret", stringField(fields, "secret", ""))
	setString(result, "external-ui", mihomoExternalUIPath(fields))
	setString(result, "external-ui-name", stringField(fields, "externalUiName", ""))
	setString(result, "external-ui-url", stringField(fields, "externalUiUrl", ""))
	setString(result, "geodata-loader", stringField(fields, "geodataLoader", ""))
	setString(result, "global-ua", stringField(fields, "globalUa", ""))
	setString(result, "interface-name", stringField(fields, "networkInterface", ""))
	setInt(result, "keep-alive-interval", intField(fields, "keepAliveInterval", 0))
	setInt(result, "keep-alive-idle", intField(fields, "keepAliveIdle", 0))
	setInt(result, "routing-mark", intField(fields, "networkRoutingMark", 0))
	setInt(result, "geo-update-interval", intField(fields, "geoUpdateInterval", 0))
	if sniffer := mihomoSniffer(config); len(sniffer) > 0 {
		result["sniffer"] = sniffer
	}
	if ntp := mihomoNTP(config); len(ntp) > 0 {
		result["ntp"] = ntp
	}
	if profile := mihomoProfile(config); len(profile) > 0 {
		result["profile"] = profile
	}
	for _, inbound := range config.Inbounds {
		if !inbound.Enabled {
			continue
		}
		switch inbound.Kind {
		case "http":
			if inbound.Listen.Port == 0 {
				continue
			}
			result["port"] = inbound.Listen.Port
		case "socks":
			if inbound.Listen.Port == 0 {
				continue
			}
			result["socks-port"] = inbound.Listen.Port
		case "mixed":
			if inbound.Listen.Port == 0 {
				continue
			}
			result["mixed-port"] = inbound.Listen.Port
		case "redirect":
			if inbound.Listen.Port == 0 {
				continue
			}
			result["redir-port"] = inbound.Listen.Port
		case "tproxy":
			if inbound.Listen.Port == 0 {
				continue
			}
			result["tproxy-port"] = inbound.Listen.Port
		case "tun":
			result["tun"] = mihomoTun(inbound.Tun)
		}
	}
	return result
}

func mihomoExternalUIPath(fields map[string]any) string {
	return strings.TrimSpace(stringField(fields, "externalUi", ""))
}

func mihomoSniffer(config repository.GlobalConfig) map[string]any {
	fields := config.Fields
	sniff := map[string]any{}
	if ports := splitLines(stringField(fields, "snifferQuicPorts", "")); len(ports) > 0 {
		sniff["QUIC"] = map[string]any{"ports": ports}
	}
	if ports := splitLines(stringField(fields, "snifferTlsPorts", "")); len(ports) > 0 {
		sniff["TLS"] = map[string]any{"ports": ports}
	}
	if ports := splitLines(stringField(fields, "snifferHttpPorts", "")); len(ports) > 0 {
		sniff["HTTP"] = map[string]any{
			"ports":                ports,
			"override-destination": boolField(fields, "snifferHttpOverrideDestination", true),
		}
	}
	sniffer := map[string]any{
		"enable":               boolField(fields, "snifferEnabled", true),
		"override-destination": boolField(fields, "snifferOverrideDestination", true),
		"parse-pure-ip":        boolField(fields, "snifferParsePureIp", true),
	}
	if len(sniff) > 0 {
		sniffer["sniff"] = sniff
	}
	setStrings(sniffer, "force-domain", splitLines(stringField(fields, "snifferForceDomain", "")))
	setStrings(sniffer, "skip-domain", splitLines(stringField(fields, "snifferSkipDomain", "")))
	setStrings(sniffer, "skip-dst-address", splitLines(stringField(fields, "snifferSkipDstAddress", "")))
	return sniffer
}

func mihomoNTP(config repository.GlobalConfig) map[string]any {
	fields := config.Fields
	ntp := map[string]any{
		"enable": boolField(fields, "ntpEnabled", false),
	}
	setString(ntp, "server", stringField(fields, "ntpServer", ""))
	setInt(ntp, "port", intField(fields, "ntpServerPort", 0))
	setInt(ntp, "interval", intField(fields, "ntpInterval", 0))
	ntp["write-to-system"] = boolField(fields, "ntpWriteToSystem", true)
	return ntp
}

func mihomoProfile(config repository.GlobalConfig) map[string]any {
	fields := config.Fields
	return map[string]any{
		"store-selected": boolField(fields, "profileStoreSelected", true),
	}
}

func mihomoGeoxURL(config repository.GlobalConfig) map[string]any {
	fields := config.Fields
	geoxURL := map[string]any{}
	setString(geoxURL, "geoip", stringField(fields, "geoxUrlGeoip", ""))
	setString(geoxURL, "geosite", stringField(fields, "geoxUrlGeosite", ""))
	setString(geoxURL, "mmdb", stringField(fields, "geoxUrlMmdb", ""))
	setString(geoxURL, "asn", stringField(fields, "geoxUrlAsn", ""))
	return geoxURL
}

func mihomoTun(tun repository.InboundTun) map[string]any {
	item := map[string]any{"enable": true}
	if tun.InterfaceName != "" {
		item["interface-name"] = tun.InterfaceName
	}
	if tun.Device != "" {
		item["device"] = tun.Device
	}
	if tun.Stack != "" {
		item["stack"] = tun.Stack
	}
	if tun.MTU > 0 {
		item["mtu"] = tun.MTU
	}
	item["auto-route"] = tun.AutoRoute
	item["auto-redirect"] = tun.AutoRedirect
	item["auto-detect-interface"] = tun.AutoDetectInterface || tun.AutoRoute
	item["strict-route"] = tun.StrictRoute
	if len(tun.DNSHijack) > 0 {
		item["dns-hijack"] = tun.DNSHijack
	}
	if len(tun.RouteAddress) > 0 {
		item["route-address"] = tun.RouteAddress
	}
	if len(tun.RouteExcludeAddress) > 0 {
		item["route-exclude-address"] = tun.RouteExcludeAddress
	}
	if len(tun.RouteAddressSet) > 0 {
		item["route-address-set"] = tun.RouteAddressSet
	}
	if len(tun.RouteExcludeSet) > 0 {
		item["route-exclude-address-set"] = tun.RouteExcludeSet
	}
	if len(tun.IncludeInterface) > 0 {
		item["include-interface"] = tun.IncludeInterface
	}
	if len(tun.ExcludeInterface) > 0 {
		item["exclude-interface"] = tun.ExcludeInterface
	}
	return item
}

func mihomoAuthentication(config repository.GlobalConfig) []string {
	credentials := splitLines(stringField(config.Fields, "authentication", ""))
	seen := map[string]bool{}
	items := []string{}
	for _, credential := range credentials {
		if seen[credential] {
			continue
		}
		seen[credential] = true
		items = append(items, credential)
	}
	for _, inbound := range config.Inbounds {
		if !inbound.Enabled {
			continue
		}
		switch inbound.Kind {
		case "http", "socks", "mixed":
			for _, credential := range inboundUserCredentials(inbound.Auth.Users) {
				if seen[credential] {
					continue
				}
				seen[credential] = true
				items = append(items, credential)
			}
		}
	}
	return items
}

func singBoxRuntimeConfig(config repository.GlobalConfig, nodes []repository.NormalizedNode, groups []repository.NormalizedGroup, rules []repository.NormalizedRule, ruleSets []repository.SingBoxRuleSetResource, repositories []repository.RuleSourceRepository, externalController string, options runtimeCompileOptions) map[string]any {
	clashAPI := map[string]any{"external_controller": externalController}
	setString(clashAPI, "secret", stringField(config.Fields, "secret", ""))

	return map[string]any{
		"log":          map[string]any{"level": stringField(config.Fields, "logLevel", "info")},
		"inbounds":     singBoxInbounds(config.Inbounds),
		"outbounds":    singBoxOutbounds(nodes, groups),
		"route":        singBoxRoute(config, rules, ruleSets, repositories),
		"dns":          singBoxDNS(config, options),
		"experimental": map[string]any{"clash_api": clashAPI},
	}
}

func mihomoProxies(nodes []repository.NormalizedNode) []map[string]any {
	proxies := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		proxy := copyMap(node.Raw)
		proxy["name"] = node.Tag
		proxy["type"] = node.Type
		if node.Server != "" {
			proxy["server"] = node.Server
		}
		if node.ServerPort > 0 {
			proxy["port"] = node.ServerPort
		}
		proxies = append(proxies, proxy)
	}
	return proxies
}

func mihomoGroups(groups []repository.NormalizedGroup) []map[string]any {
	items := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		item := copyMap(group.Raw)
		item["name"] = group.Tag
		item["type"] = group.Type
		item["proxies"] = group.Outbounds
		items = append(items, item)
	}
	return items
}

func mihomoRules(rules []repository.NormalizedRule) []string {
	lines := []string{}
	for _, rule := range rules {
		if len(rule.Raw) > 0 {
			lines = append(lines, rule.Raw...)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "MATCH,DIRECT")
	}
	return lines
}

func mihomoDNS(config repository.GlobalConfig) map[string]any {
	fields := config.Fields
	dns := map[string]any{
		"enable":              boolField(fields, "dnsEnabled", true),
		"ipv6":                boolField(fields, "dnsIpv6", false),
		"enhanced-mode":       stringField(fields, "dnsMode", "fake-ip"),
		"fake-ip-filter-mode": stringField(fields, "dnsFakeIpFilterMode", "blacklist"),
		"use-hosts":           boolField(fields, "dnsUseHosts", false),
		"respect-rules":       boolField(fields, "dnsMihomoRespectRules", true),
	}
	setString(dns, "listen", stringField(fields, "dnsListen", "0.0.0.0:7874"))
	setString(dns, "fake-ip-range", stringField(fields, "dnsFakeIpRange", "198.18.0.1/15"))
	setString(dns, "fake-ip-range6", stringField(fields, "dnsFakeIpRange6", ""))
	setStrings(dns, "fake-ip-filter", splitLines(stringField(fields, "dnsFakeIpFilters", "")))
	setStrings(dns, "default-nameserver", mihomoDNSServersByRole(config.DNSServers, "bootstrap"))
	setStrings(dns, "nameserver", mihomoDNSServersByRole(config.DNSServers, "default"))
	setStrings(dns, "fallback", mihomoDNSServersByRole(config.DNSServers, "fallback"))
	setStrings(dns, "direct-nameserver", mihomoDNSServersByRole(config.DNSServers, "direct"))
	setStrings(dns, "proxy-server-nameserver", mihomoDNSServersByRole(config.DNSServers, "proxy"))
	if policy := mihomoNameserverPolicy(config.DNSServers, config.DNSRules); len(policy) > 0 {
		dns["nameserver-policy"] = policy
	}
	return dns
}

func mihomoDNSServersByRole(servers []repository.GlobalDNSServer, role string) []string {
	items := []string{}
	for _, server := range servers {
		if server.Role != role {
			continue
		}
		if formatted := formatMihomoDNSServer(server); formatted != "" {
			items = append(items, formatted)
		}
	}
	return items
}

func formatMihomoDNSServer(server repository.GlobalDNSServer) string {
	address := strings.TrimSpace(server.Address)
	if address == "" {
		return ""
	}
	protocol := strings.TrimSpace(server.Protocol)
	if protocol == "system" {
		return "system"
	}

	port := strings.TrimSpace(server.Port)
	path := strings.TrimSpace(server.Path)
	endpoint := address
	switch protocol {
	case "https", "h3":
		queryPath := path
		if queryPath == "" {
			queryPath = "/dns-query"
		}
		endpoint = "https://" + address + optionalPort(port) + queryPath
	case "tls":
		endpoint = "tls://" + address + optionalPort(port)
	case "quic":
		endpoint = "quic://" + address + optionalPort(port)
	case "tcp":
		endpoint = "tcp://" + address + optionalPort(port)
	default:
		if port != "" && port != "53" {
			endpoint = address + ":" + port
		}
	}

	params := []string{}
	if detour := strings.TrimSpace(server.Detour); detour != "" {
		params = append(params, detour)
	}
	if protocol == "h3" {
		params = append(params, "h3=true")
	}
	if server.SkipCertVerify {
		params = append(params, "skip-cert-verify=true")
	}
	if subnet := strings.TrimSpace(server.ClientSubnet); subnet != "" {
		params = append(params, "ecs="+subnet)
	}
	if len(params) > 0 {
		return endpoint + "#" + strings.Join(params, "&")
	}
	return endpoint
}

func mihomoNameserverPolicy(servers []repository.GlobalDNSServer, rules []repository.GlobalDNSRule) map[string][]string {
	byName := map[string][]string{}
	for _, server := range servers {
		name := strings.TrimSpace(server.Name)
		if name == "" {
			continue
		}
		if formatted := formatMihomoDNSServer(server); formatted != "" {
			byName[name] = append(byName[name], formatted)
		}
	}
	policy := map[string][]string{}
	for _, rule := range rules {
		value := strings.TrimSpace(rule.Value)
		if value == "" {
			continue
		}
		targets := byName[strings.TrimSpace(rule.ServerName)]
		if len(targets) == 0 {
			continue
		}
		key := value
		switch rule.Matcher {
		case "domain_suffix":
			key = "+." + strings.TrimPrefix(strings.TrimPrefix(value, "+."), ".")
		case "geosite":
			key = "geosite:" + value
		case "rule_set":
			key = "rule-set:" + value
		}
		policy[key] = targets
	}
	return policy
}

func optionalPort(port string) string {
	if port == "" {
		return ""
	}
	return ":" + port
}

func splitLines(value string) []string {
	lines := []string{}
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func stringValues(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok {
				values = append(values, value)
			}
		}
		return values
	case string:
		return []string{typed}
	default:
		return nil
	}
}

func appendUniqueStrings(values []string, additions ...string) []string {
	result := make([]string, 0, len(values)+len(additions))
	seen := map[string]bool{}
	for _, value := range append(values, additions...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func singBoxGeoIPCode(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func setString(target map[string]any, key string, value string) {
	if strings.TrimSpace(value) != "" {
		target[key] = strings.TrimSpace(value)
	}
}

func setStrings(target map[string]any, key string, values []string) {
	if len(values) > 0 {
		target[key] = values
	}
}

func singBoxInbounds(inbounds []repository.ManagedInbound) []map[string]any {
	items := []map[string]any{}
	for _, inbound := range inbounds {
		if !inbound.Enabled {
			continue
		}
		if !singBoxInboundSupportedOnCurrentOS(inbound.Kind) {
			continue
		}
		item := copyMap(inbound.Raw)
		item["type"] = inbound.Kind
		item["tag"] = inbound.Tag
		if inbound.Listen.Address != "" {
			item["listen"] = inbound.Listen.Address
		}
		if inbound.Listen.Port > 0 {
			item["listen_port"] = inbound.Listen.Port
		}
		if singBoxInboundSupportsNetwork(inbound.Kind) && inbound.Network != "" {
			item["network"] = inbound.Network
		} else {
			delete(item, "network")
		}
		if users := singBoxInboundUsers(inbound.Auth.Users); len(users) > 0 {
			item["users"] = users
		}
		if inbound.Kind == "tun" {
			mergeMap(item, singBoxInboundTun(inbound.Tun))
		}
		items = append(items, item)
	}
	return items
}

func singBoxInboundSupportsNetwork(kind string) bool {
	return kind == "tproxy"
}

func singBoxInboundSupportedOnCurrentOS(kind string) bool {
	if kind == "tproxy" && runtime.GOOS != "linux" {
		return false
	}
	return true
}

func singBoxInboundUsers(users []repository.InboundUser) []map[string]any {
	items := []map[string]any{}
	for _, user := range users {
		item := map[string]any{}
		if username := strings.TrimSpace(user.Username); username != "" {
			item["username"] = username
		}
		if password := strings.TrimSpace(user.Password); password != "" {
			item["password"] = password
		}
		if len(item) > 0 {
			items = append(items, item)
		}
	}
	return items
}

func singBoxInboundTun(tun repository.InboundTun) map[string]any {
	item := map[string]any{}
	addresses := tun.Address
	if len(addresses) == 0 {
		addresses = []string{"172.19.0.1/30"}
	}
	setStrings(item, "address", addresses)
	setString(item, "interface_name", tun.InterfaceName)
	setString(item, "stack", tun.Stack)
	setInt(item, "mtu", tun.MTU)
	item["auto_route"] = tun.AutoRoute
	item["auto_redirect"] = tun.AutoRedirect
	item["strict_route"] = tun.StrictRoute
	setStrings(item, "route_address", tun.RouteAddress)
	setStrings(item, "route_exclude_address", tun.RouteExcludeAddress)
	setStrings(item, "route_address_set", tun.RouteAddressSet)
	setStrings(item, "route_exclude_address_set", tun.RouteExcludeSet)
	setStrings(item, "include_interface", tun.IncludeInterface)
	setStrings(item, "exclude_interface", tun.ExcludeInterface)
	return item
}

func inboundUserCredentials(users []repository.InboundUser) []string {
	items := []string{}
	for _, user := range users {
		username := strings.TrimSpace(user.Username)
		password := strings.TrimSpace(user.Password)
		if username == "" {
			continue
		}
		if password == "" {
			items = append(items, username)
			continue
		}
		items = append(items, username+":"+password)
	}
	return items
}

func singBoxOutbounds(nodes []repository.NormalizedNode, groups []repository.NormalizedGroup) []map[string]any {
	items := []map[string]any{{"type": "direct", "tag": "DIRECT"}, {"type": "block", "tag": "REJECT"}}
	for _, node := range nodes {
		item := copyMap(node.Transport)
		item["type"] = node.Type
		item["tag"] = node.Tag
		if node.Server != "" {
			item["server"] = node.Server
		}
		if node.ServerPort > 0 {
			item["server_port"] = node.ServerPort
		}
		items = append(items, item)
	}
	for _, group := range groups {
		items = append(items, singBoxGroup(group))
	}
	return items
}

func singBoxGroup(group repository.NormalizedGroup) map[string]any {
	item := map[string]any{
		"type":      singBoxGroupType(group.Type),
		"tag":       group.Tag,
		"outbounds": group.Outbounds,
	}
	for _, key := range []string{"url", "interval", "tolerance"} {
		if value, ok := group.Raw[key]; ok {
			item[key] = value
		}
	}
	return item
}

func singBoxRules(rules []repository.NormalizedRule) []map[string]any {
	items := []map[string]any{}
	for _, rule := range rules {
		item, ok := singBoxRule(rule)
		if ok {
			items = append(items, item)
		}
	}
	return items
}

func singBoxRule(rule repository.NormalizedRule) (map[string]any, bool) {
	if singBoxRuleHasRemovedSourceGeoIP(rule) {
		return nil, false
	}
	item := copyMap(rule.Fields)
	if geoIPValues := stringValues(item["geoip"]); len(geoIPValues) > 0 {
		delete(item, "geoip")
		item["rule_set"] = appendUniqueStrings(stringValues(item["rule_set"]), singBoxBuiltInGeoIPRuleSetTags(geoIPValues)...)
	}
	if rule.Type != "" {
		item["type"] = rule.Type
	}
	if rule.Mode != "" {
		item["mode"] = rule.Mode
	}
	if len(rule.Rules) > 0 {
		nested := singBoxRules(rule.Rules)
		if len(nested) == 0 {
			return nil, false
		}
		item["rules"] = nested
	}
	if rule.Outbound != "" {
		item["outbound"] = rule.Outbound
	}
	if rule.Action != "" {
		item["action"] = rule.Action
	}
	return item, len(item) > 0
}

func singBoxRuleHasRemovedSourceGeoIP(rule repository.NormalizedRule) bool {
	_, ok := rule.Fields["source_geoip"]
	return ok
}

func singBoxBuiltInGeoIPRuleSetTags(values []string) []string {
	tags := make([]string, 0, len(values))
	for _, value := range values {
		if code := singBoxGeoIPCode(value); code != "" {
			tags = append(tags, "geoip-"+code)
		}
	}
	return appendUniqueStrings(nil, tags...)
}

func singBoxRoute(config repository.GlobalConfig, rules []repository.NormalizedRule, ruleSets []repository.SingBoxRuleSetResource, repositories []repository.RuleSourceRepository) map[string]any {
	fields := config.Fields
	route := map[string]any{"rules": append(singBoxDefaultRouteRules(), singBoxRules(rules)...)}
	if renderedRuleSets := singBoxRouteRuleSets(rules, ruleSets, repositories); len(renderedRuleSets) > 0 {
		route["rule_set"] = renderedRuleSets
	}
	if resolver := singBoxDefaultDomainResolver(config); len(resolver) > 0 {
		route["default_domain_resolver"] = resolver
	}
	if boolField(fields, "routeAutoDetectInterface", false) {
		route["auto_detect_interface"] = true
	}
	if boolField(fields, "routeOverrideAndroidVpn", false) {
		route["override_android_vpn"] = true
	}
	setString(route, "default_interface", stringField(fields, "networkInterface", ""))
	if mark := intField(fields, "networkRoutingMark", 0); mark > 0 {
		route["default_mark"] = mark
	}
	return route
}

func singBoxRouteRuleSets(rules []repository.NormalizedRule, ruleSets []repository.SingBoxRuleSetResource, repositories []repository.RuleSourceRepository) []map[string]any {
	items := []map[string]any{}
	seen := map[string]bool{}
	for _, item := range singBoxConfiguredRuleSets(ruleSets, repositories) {
		if tag, ok := item["tag"].(string); ok && tag != "" && !seen[tag] {
			seen[tag] = true
			items = append(items, item)
		}
	}
	for _, tag := range singBoxReferencedBuiltInGeoIPRuleSetTags(rules) {
		if seen[tag] {
			continue
		}
		seen[tag] = true
		items = append(items, singBoxBuiltInGeoIPRuleSet(tag, repositories))
	}
	return items
}

func singBoxConfiguredRuleSets(ruleSets []repository.SingBoxRuleSetResource, repositories []repository.RuleSourceRepository) []map[string]any {
	repositoriesByID := map[string]repository.RuleSourceRepository{}
	for _, repo := range repositories {
		repositoriesByID[repo.ID] = repo
	}

	items := make([]map[string]any, 0, len(ruleSets))
	for _, ruleSet := range ruleSets {
		item := map[string]any{
			"tag":    ruleSet.Tag,
			"format": ruleSet.Format,
		}
		switch ruleSet.SourceMode {
		case repository.RuleAssetSourceModeRepositoryFile:
			repo, ok := repositoriesByID[ruleSet.RepositoryID]
			if !ok {
				continue
			}
			rawURL, err := repository.BuildRepositoryRawURL(repo, repository.CoreSingBox, ruleSet.Path, ruleSet.Ref)
			if err != nil {
				continue
			}
			item["type"] = "remote"
			item["url"] = rawURL
		case repository.RuleAssetSourceModeRemote:
			item["type"] = "remote"
			item["url"] = ruleSet.URL
		case repository.RuleAssetSourceModeLocal:
			item["type"] = "local"
			item["path"] = ruleSet.LocalPath
		default:
			continue
		}
		setString(item, "update_interval", ruleSet.UpdateInterval)
		items = append(items, item)
	}
	return items
}

func singBoxReferencedBuiltInGeoIPRuleSetTags(rules []repository.NormalizedRule) []string {
	tags := []string{}
	for _, rule := range rules {
		tags = append(tags, singBoxBuiltInGeoIPRuleSetTags(stringValues(rule.Fields["geoip"]))...)
		if len(rule.Rules) > 0 {
			tags = append(tags, singBoxReferencedBuiltInGeoIPRuleSetTags(rule.Rules)...)
		}
	}
	return appendUniqueStrings(nil, tags...)
}

func singBoxBuiltInGeoIPRuleSet(tag string, repositories []repository.RuleSourceRepository) map[string]any {
	code := strings.TrimPrefix(tag, "geoip-")
	ruleSetURL := "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/geoip/" + url.PathEscape(code) + ".srs"
	for _, repo := range repositories {
		if repo.ID != singBoxBuiltInGeoIPRepositoryID {
			continue
		}
		if rawURL, err := repository.BuildRepositoryRawURL(repo, repository.CoreSingBox, "geo/geoip/"+code+".srs", ""); err == nil {
			ruleSetURL = rawURL
		}
		break
	}
	return map[string]any{
		"type":   "remote",
		"tag":    tag,
		"format": "binary",
		"url":    ruleSetURL,
	}
}

func singBoxDefaultRouteRules() []map[string]any {
	return []map[string]any{
		map[string]any{"action": "sniff"},
		map[string]any{"protocol": "dns", "action": "hijack-dns"},
		map[string]any{"ip_is_private": true, "outbound": "direct"},
	}
}

func singBoxDefaultDomainResolver(config repository.GlobalConfig) map[string]any {
	server := firstDNSServerNameByRole(config.DNSServers, "default")
	if server == "" {
		server = firstDNSServerName(config.DNSServers)
	}
	if server == "" {
		return nil
	}
	resolver := map[string]any{"server": server}
	setString(resolver, "strategy", stringField(config.Fields, "dnsDefaultStrategy", "prefer_ipv4"))
	setString(resolver, "client_subnet", stringField(config.Fields, "dnsSingBoxClientSubnet", ""))
	return resolver
}

func singBoxDNSServers(servers []repository.GlobalDNSServer) []map[string]any {
	items := []map[string]any{}
	for _, server := range servers {
		item := singBoxDNSServer(server, len(items))
		if len(item) == 0 {
			continue
		}
		items = append(items, item)
	}
	return items
}

func singBoxDNS(config repository.GlobalConfig, options runtimeCompileOptions) map[string]any {
	fields := config.Fields
	servers := singBoxDNSServers(config.DNSServers)
	if fakeIP := singBoxDNSFakeIPServer(fields); len(fakeIP) > 0 {
		servers = append(servers, fakeIP)
	}

	dns := map[string]any{
		"servers":         servers,
		"disable_cache":   !boolField(fields, "dnsCacheEnabled", true),
		"reverse_mapping": boolField(fields, "dnsSingBoxReverseMapping", false),
	}
	defaultServer := firstDNSServerNameByRole(config.DNSServers, "default")
	rules := singBoxDNSFakeIPRules(fields, defaultServer)
	rules = append(rules, singBoxDNSRules(config.DNSRules, defaultServer)...)
	if catchAll := singBoxDNSFakeIPCatchAllRule(fields); len(catchAll) > 0 {
		rules = append(rules, catchAll)
	}
	rules = mergeAdjacentSingBoxDNSRules(rules)
	if len(rules) > 0 {
		dns["rules"] = rules
	}
	setString(dns, "final", singBoxDNSFinalServer(fields, defaultServer))
	setString(dns, "strategy", stringField(fields, "dnsDefaultStrategy", "prefer_ipv4"))
	if capacity := intField(fields, "dnsCacheCapacity", 0); capacity > 0 {
		dns["cache_capacity"] = capacity
	}
	if options.SingBoxDNS14 && boolField(fields, "dnsOptimisticEnabled", false) {
		optimistic := map[string]any{"enabled": true}
		setString(optimistic, "timeout", stringField(fields, "dnsOptimisticTimeout", "3d"))
		dns["optimistic"] = optimistic
	}
	if options.SingBoxDNS14 {
		setString(dns, "timeout", stringField(fields, "dnsTimeout", "10s"))
	}
	setString(dns, "client_subnet", stringField(fields, "dnsSingBoxClientSubnet", ""))
	return dns
}

func singBoxDNSFakeIPServer(fields map[string]any) map[string]any {
	if !singBoxDNSFakeIPEnabled(fields) {
		return nil
	}
	item := map[string]any{"tag": singBoxFakeIPServerTag, "type": "fakeip"}
	setString(item, "inet4_range", stringField(fields, "dnsFakeIpRange", "198.18.0.1/15"))
	setString(item, "inet6_range", stringField(fields, "dnsFakeIpRange6", ""))
	return item
}

func singBoxDNSFakeIPEnabled(fields map[string]any) bool {
	return stringField(fields, "dnsMode", "fake-ip") == "fake-ip" && boolField(fields, "dnsFakeIpEnabled", true)
}

func singBoxDNSFinalServer(fields map[string]any, defaultServer string) string {
	return defaultServer
}

func singBoxDNSFakeIPCatchAllRule(fields map[string]any) map[string]any {
	if !singBoxDNSFakeIPEnabled(fields) || stringField(fields, "dnsFakeIpFilterMode", "blacklist") != "blacklist" {
		return nil
	}
	return map[string]any{"query_type": []string{"A", "AAAA"}, "server": singBoxFakeIPServerTag}
}

func singBoxDNSFakeIPRules(fields map[string]any, defaultServer string) []map[string]any {
	if !singBoxDNSFakeIPEnabled(fields) {
		return nil
	}
	filters := splitLines(stringField(fields, "dnsFakeIpFilters", ""))
	mode := stringField(fields, "dnsFakeIpFilterMode", "blacklist")
	if mode == "rule" {
		items := []map[string]any{}
		for _, filter := range filters {
			if item := singBoxDNSRuleFromFakeIPRuleLine(filter, defaultServer); len(item) > 0 {
				items = append(items, item)
			}
		}
		return items
	}

	server := defaultServer
	if mode == "whitelist" {
		server = singBoxFakeIPServerTag
	}
	items := []map[string]any{}
	for _, filter := range filters {
		if item := singBoxDNSRuleFromFakeIPFilter(filter, server); len(item) > 0 {
			items = append(items, item)
		}
	}
	return items
}

func singBoxDNSRuleFromFakeIPFilter(value string, server string) map[string]any {
	if strings.TrimSpace(server) == "" {
		return nil
	}
	item := singBoxDNSMatcherFromValue(value)
	if len(item) == 0 {
		return nil
	}
	item["server"] = strings.TrimSpace(server)
	return item
}

func singBoxDNSRuleFromFakeIPRuleLine(line string, defaultServer string) map[string]any {
	parts := trimNonEmptyCommaParts(line)
	if len(parts) < 2 {
		return nil
	}
	ruleType := strings.ToUpper(parts[0])
	if ruleType == "MATCH" {
		server := fakeIPActionServer(parts[1], defaultServer)
		if server == "" {
			return nil
		}
		return map[string]any{"server": server}
	}
	if len(parts) < 3 {
		return nil
	}
	server := fakeIPActionServer(parts[2], defaultServer)
	if server == "" {
		return nil
	}
	switch ruleType {
	case "DOMAIN":
		return map[string]any{"domain": []string{parts[1]}, "server": server}
	case "DOMAIN-SUFFIX":
		return map[string]any{"domain_suffix": []string{parts[1]}, "server": server}
	case "DOMAIN-KEYWORD":
		return map[string]any{"domain_keyword": []string{parts[1]}, "server": server}
	case "DOMAIN-REGEX":
		return map[string]any{"domain_regex": []string{parts[1]}, "server": server}
	case "DOMAIN-WILDCARD":
		return map[string]any{"domain_regex": []string{wildcardToRegex(parts[1])}, "server": server}
	case "GEOSITE":
		return map[string]any{"geosite": []string{parts[1]}, "server": server}
	case "RULE-SET":
		return map[string]any{"rule_set": []string{parts[1]}, "server": server}
	default:
		return nil
	}
}

func singBoxDNSMatcherFromValue(value string) map[string]any {
	item := strings.TrimSpace(value)
	if item == "" {
		return nil
	}
	lower := strings.ToLower(item)
	if strings.HasPrefix(lower, "geosite:") {
		value := strings.TrimSpace(item[len("geosite:"):])
		if value == "" {
			return nil
		}
		return map[string]any{"geosite": []string{value}}
	}
	if strings.HasPrefix(lower, "rule-set:") {
		value := strings.TrimSpace(item[len("rule-set:"):])
		if value == "" {
			return nil
		}
		return map[string]any{"rule_set": []string{value}}
	}
	if strings.HasPrefix(item, "*.") || strings.HasPrefix(item, "+.") {
		return map[string]any{"domain_suffix": []string{item[2:]}}
	}
	if strings.Contains(item, "*") {
		return map[string]any{"domain_regex": []string{wildcardToRegex(item)}}
	}
	return map[string]any{"domain": []string{item}}
}

func fakeIPActionServer(action string, defaultServer string) string {
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(action)), "_", "-")
	switch normalized {
	case "fake-ip", "fakeip":
		return singBoxFakeIPServerTag
	case "real-ip", "realip":
		return strings.TrimSpace(defaultServer)
	default:
		return ""
	}
}

func trimNonEmptyCommaParts(value string) []string {
	rawParts := strings.Split(value, ",")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func wildcardToRegex(value string) string {
	return "^" + strings.ReplaceAll(regexp.QuoteMeta(value), `\*`, ".*") + "$"
}

func singBoxDNSRules(rules []repository.GlobalDNSRule, defaultServer string) []map[string]any {
	items := []map[string]any{}
	for _, rule := range rules {
		value := strings.TrimSpace(rule.Value)
		if value == "" {
			continue
		}
		item := map[string]any{}
		switch rule.Matcher {
		case "domain":
			item["domain"] = []string{value}
		case "domain_suffix":
			item["domain_suffix"] = []string{value}
		case "geosite":
			item["geosite"] = []string{value}
		case "rule_set":
			item["rule_set"] = []string{value}
		default:
			continue
		}
		serverName := strings.TrimSpace(rule.ServerName)
		if serverName == "" {
			serverName = defaultServer
		}
		setString(item, "server", serverName)
		setString(item, "strategy", rule.Strategy)
		setString(item, "client_subnet", rule.ClientSubnet)
		items = append(items, item)
	}
	return items
}

var singBoxDNSMatcherKeys = []string{"domain", "domain_suffix", "domain_keyword", "domain_regex", "geosite", "rule_set"}

func mergeAdjacentSingBoxDNSRules(rules []map[string]any) []map[string]any {
	merged := []map[string]any{}
	for _, rule := range rules {
		signature, ok := mergeableSingBoxDNSRuleSignature(rule)
		if ok && len(merged) > 0 {
			if previousSignature, previousOK := mergeableSingBoxDNSRuleSignature(merged[len(merged)-1]); previousOK && previousSignature == signature {
				mergeSingBoxDNSRuleMatchers(merged[len(merged)-1], rule)
				continue
			}
		}
		merged = append(merged, copyMap(rule))
	}
	return merged
}

func mergeableSingBoxDNSRuleSignature(rule map[string]any) (string, bool) {
	matcherCount := 0
	signature := map[string]any{}
	for key, value := range rule {
		if isSingBoxDNSMatcherKey(key) {
			if values, ok := value.([]string); ok && len(values) > 0 {
				matcherCount++
			}
			continue
		}
		signature[key] = value
	}
	if matcherCount == 0 {
		return "", false
	}
	data, err := json.Marshal(signature)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func mergeSingBoxDNSRuleMatchers(target map[string]any, source map[string]any) {
	for _, key := range singBoxDNSMatcherKeys {
		values, ok := source[key].([]string)
		if !ok || len(values) == 0 {
			continue
		}
		current, _ := target[key].([]string)
		target[key] = append(current, values...)
	}
}

func isSingBoxDNSMatcherKey(key string) bool {
	for _, matcherKey := range singBoxDNSMatcherKeys {
		if key == matcherKey {
			return true
		}
	}
	return false
}

func firstDNSServerNameByRole(servers []repository.GlobalDNSServer, role string) string {
	for _, server := range servers {
		if server.Role == role {
			if name := strings.TrimSpace(server.Name); name != "" {
				return name
			}
		}
	}
	return ""
}

func firstDNSServerName(servers []repository.GlobalDNSServer) string {
	for _, server := range servers {
		if name := strings.TrimSpace(server.Name); name != "" {
			return name
		}
	}
	return ""
}

func singBoxDNSServer(server repository.GlobalDNSServer, index int) map[string]any {
	tag := strings.TrimSpace(server.Name)
	protocol := strings.TrimSpace(server.Protocol)
	address := strings.TrimSpace(server.Address)
	if tag == "" {
		tag = fmt.Sprintf("%s-%d", server.Role, index+1)
	}
	if protocol == "" {
		protocol = "udp"
	}
	if protocol == "system" {
		protocol = "local"
	}
	if address == "" {
		return nil
	}

	item := map[string]any{"type": protocol, "tag": tag, "server": address}
	if port := singBoxDNSPort(server.Port); port > 0 {
		item["server_port"] = port
	}
	if (protocol == "https" || protocol == "h3") && strings.TrimSpace(server.Path) != "" {
		item["path"] = strings.TrimSpace(server.Path)
	}
	setString(item, "detour", server.Detour)
	setString(item, "client_subnet", server.ClientSubnet)
	if server.SkipCertVerify {
		item["tls"] = map[string]any{"insecure": true}
	}
	return item
}

func singBoxDNSPort(value string) int {
	if value == "" {
		return 0
	}
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port <= 0 {
		return 0
	}
	return port
}

func singBoxGroupType(groupType string) string {
	switch groupType {
	case "select", "selector":
		return "selector"
	case "urltest", "url-test":
		return "urltest"
	case "fallback":
		return "urltest"
	default:
		return groupType
	}
}

func copyMap(input map[string]any) map[string]any {
	output := map[string]any{}
	for key, value := range input {
		output[key] = value
	}
	return output
}

func mergeMap(target map[string]any, input map[string]any) {
	for key, value := range input {
		target[key] = value
	}
}

func stringField(fields map[string]any, key string, fallback string) string {
	if value, ok := fields[key].(string); ok {
		return value
	}
	return fallback
}

func boolField(fields map[string]any, key string, fallback bool) bool {
	if value, ok := fields[key].(bool); ok {
		return value
	}
	return fallback
}

func intField(fields map[string]any, key string, fallback int) int {
	switch value := fields[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return fallback
}

func setInt(target map[string]any, key string, value int) {
	if value > 0 {
		target[key] = value
	}
}
