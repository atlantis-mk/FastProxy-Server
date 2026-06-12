package repository

import (
	"encoding/json"
	"time"
)

type Core string

const (
	CoreMihomo  Core = "mihomo"
	CoreSingBox Core = "sing-box"
)

type ResourceKind string

const (
	KindSubscription       ResourceKind = "subscription"
	KindNodeSet            ResourceKind = "node-set"
	KindRoutingRuleSet     ResourceKind = "routing-rule-set"
	KindRuleSourceRepo     ResourceKind = "rule-source-repository"
	KindSingBoxRuleSet     ResourceKind = "sing-box-rule-set"
	KindMihomoRuleProvider ResourceKind = "mihomo-rule-provider"
	KindGroupSet           ResourceKind = "group-set"
	KindProfile            ResourceKind = "profile"
)

type OriginType string

const (
	OriginManual            OriginType = "manual"
	OriginClashSubscription OriginType = "clash-subscription"
	OriginPlainNode         OriginType = "plain-node"
)

type Metadata struct {
	ID          string       `json:"id"`
	Kind        ResourceKind `json:"kind"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	OriginType  OriginType   `json:"originType"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

type ManagedInbound struct {
	ID      string         `json:"id"`
	Enabled bool           `json:"enabled"`
	Tag     string         `json:"tag"`
	Kind    string         `json:"kind"`
	Listen  InboundListen  `json:"listen,omitempty"`
	Network string         `json:"network,omitempty"`
	Auth    InboundAuth    `json:"auth,omitempty"`
	Tun     InboundTun     `json:"tun,omitempty"`
	Raw     map[string]any `json:"raw,omitempty"`
}

type InboundListen struct {
	Address string `json:"address,omitempty"`
	Port    int    `json:"port,omitempty"`
}

type InboundAuth struct {
	Users []InboundUser `json:"users,omitempty"`
}

type InboundUser struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type InboundTun struct {
	Address             []string `json:"address,omitempty"`
	InterfaceName       string   `json:"interfaceName,omitempty"`
	Device              string   `json:"device,omitempty"`
	Stack               string   `json:"stack,omitempty"`
	MTU                 int      `json:"mtu,omitempty"`
	AutoRoute           bool     `json:"autoRoute,omitempty"`
	AutoRedirect        bool     `json:"autoRedirect,omitempty"`
	AutoDetectInterface bool     `json:"autoDetectInterface,omitempty"`
	StrictRoute         bool     `json:"strictRoute,omitempty"`
	DNSHijack           []string `json:"dnsHijack,omitempty"`
	RouteAddress        []string `json:"routeAddress,omitempty"`
	RouteExcludeAddress []string `json:"routeExcludeAddress,omitempty"`
	RouteAddressSet     []string `json:"routeAddressSet,omitempty"`
	RouteExcludeSet     []string `json:"routeExcludeAddressSet,omitempty"`
	IncludeInterface    []string `json:"includeInterface,omitempty"`
	ExcludeInterface    []string `json:"excludeInterface,omitempty"`
}

type NormalizedNode struct {
	ID         string         `json:"id"`
	Tag        string         `json:"tag"`
	Type       string         `json:"type"`
	Server     string         `json:"server,omitempty"`
	ServerPort int            `json:"server_port,omitempty"`
	Source     string         `json:"source,omitempty"`
	Transport  map[string]any `json:"transport,omitempty"`
	Raw        map[string]any `json:"raw,omitempty"`
}

func (n NormalizedNode) MarshalJSON() ([]byte, error) {
	payload := map[string]any{
		"id":   n.ID,
		"tag":  n.Tag,
		"type": n.Type,
	}
	if n.Server != "" {
		payload["server"] = n.Server
	}
	if n.ServerPort > 0 {
		payload["server_port"] = n.ServerPort
	}
	if n.Source != "" {
		payload["source"] = n.Source
	}
	for key, value := range n.Transport {
		payload[key] = value
	}
	if len(n.Raw) > 0 {
		payload["raw"] = n.Raw
	}
	return json.Marshal(payload)
}

func (n *NormalizedNode) UnmarshalJSON(data []byte) error {
	type nodeAlias struct {
		ID         string         `json:"id"`
		Tag        string         `json:"tag"`
		Type       string         `json:"type"`
		Server     string         `json:"server,omitempty"`
		ServerPort int            `json:"server_port,omitempty"`
		Source     string         `json:"source,omitempty"`
		Raw        map[string]any `json:"raw,omitempty"`
	}

	var alias nodeAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	delete(raw, "id")
	delete(raw, "tag")
	delete(raw, "type")
	delete(raw, "server")
	delete(raw, "server_port")
	delete(raw, "source")
	delete(raw, "raw")

	n.ID = alias.ID
	n.Tag = alias.Tag
	n.Type = alias.Type
	n.Server = alias.Server
	n.ServerPort = alias.ServerPort
	n.Source = alias.Source
	n.Transport = raw
	n.Raw = alias.Raw
	return nil
}

type NormalizedRule struct {
	ID       string           `json:"id"`
	Type     string           `json:"type,omitempty"`
	Mode     string           `json:"mode,omitempty"`
	Rules    []NormalizedRule `json:"rules,omitempty"`
	Action   string           `json:"action,omitempty"`
	Outbound string           `json:"outbound,omitempty"`
	Fields   map[string]any   `json:"fields,omitempty"`
	Raw      []string         `json:"raw,omitempty"`
}

func (r NormalizedRule) MarshalJSON() ([]byte, error) {
	payload := map[string]any{
		"id": r.ID,
	}
	if r.Type != "" {
		payload["type"] = r.Type
	}
	if r.Mode != "" {
		payload["mode"] = r.Mode
	}
	if len(r.Rules) > 0 {
		payload["rules"] = r.Rules
	}
	if r.Action != "" {
		payload["action"] = r.Action
	}
	if r.Outbound != "" {
		payload["outbound"] = r.Outbound
	}
	for key, value := range r.Fields {
		payload[key] = value
	}
	if len(r.Raw) > 0 {
		payload["raw"] = r.Raw
	}
	return json.Marshal(payload)
}

func (r *NormalizedRule) UnmarshalJSON(data []byte) error {
	type ruleAlias struct {
		ID       string           `json:"id"`
		Type     string           `json:"type,omitempty"`
		Mode     string           `json:"mode,omitempty"`
		Rules    []NormalizedRule `json:"rules,omitempty"`
		Action   string           `json:"action,omitempty"`
		Outbound string           `json:"outbound,omitempty"`
		Raw      []string         `json:"raw,omitempty"`
	}

	var alias ruleAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	delete(raw, "id")
	delete(raw, "type")
	delete(raw, "mode")
	delete(raw, "rules")
	delete(raw, "action")
	delete(raw, "outbound")
	delete(raw, "raw")

	r.ID = alias.ID
	r.Type = alias.Type
	r.Mode = alias.Mode
	r.Rules = alias.Rules
	r.Action = alias.Action
	r.Outbound = alias.Outbound
	r.Fields = raw
	r.Raw = alias.Raw
	return nil
}

type NormalizedGroup struct {
	ID        string         `json:"id"`
	Tag       string         `json:"tag"`
	Type      string         `json:"type"`
	Outbounds []string       `json:"outbounds,omitempty"`
	Raw       map[string]any `json:"raw,omitempty"`
}

type NormalizedConfig struct {
	Nodes  []NormalizedNode  `json:"nodes,omitempty"`
	Rules  []NormalizedRule  `json:"rules,omitempty"`
	Groups []NormalizedGroup `json:"groups,omitempty"`
	Extras map[string]any    `json:"extras,omitempty"`
}

type SubscriptionFetchOptions struct {
	SourceInput string `json:"sourceInput,omitempty"`
	UserAgent   string `json:"userAgent,omitempty"`
}

type SubscriptionAutoUpdate struct {
	Enabled         bool `json:"enabled,omitempty"`
	IntervalMinutes int  `json:"intervalMinutes,omitempty"`
}

type SubscriptionSyncStatus struct {
	LastSyncedAt  time.Time `json:"lastSyncedAt,omitempty"`
	LastSyncError string    `json:"lastSyncError,omitempty"`
}

type SubscriptionResource struct {
	Metadata
	SourceURL  string                   `json:"sourceUrl,omitempty"`
	Revision   string                   `json:"revision,omitempty"`
	Fetch      SubscriptionFetchOptions `json:"fetch,omitempty"`
	AutoUpdate SubscriptionAutoUpdate   `json:"autoUpdate,omitempty"`
	Sync       SubscriptionSyncStatus   `json:"sync,omitempty"`
}

type NodeSetResource struct {
	Metadata
	Nodes []NormalizedNode `json:"nodes,omitempty"`
}

type NodeSetFile struct {
	FileName string          `json:"fileName"`
	NodeSet  NodeSetResource `json:"nodeSet"`
}

type NodeCacheSourceType string

const (
	NodeCacheSourceSubscription NodeCacheSourceType = "subscription"
	NodeCacheSourceNodeSet      NodeCacheSourceType = "node-set"
	NodeCacheSourceImport       NodeCacheSourceType = "import"
	NodeCacheSourceManual       NodeCacheSourceType = "manual"
)

type NodeCacheUpsert struct {
	SourceType     NodeCacheSourceType
	SourceID       string
	SubscriptionID string
	NodeSetID      string
	RefreshedAt    time.Time
	Nodes          []NormalizedNode
}

type NodeCacheQuery struct {
	Offset         int
	Limit          int
	Query          string
	Protocol       string
	Address        string
	Tag            string
	Source         string
	SubscriptionID string
	NodeSetID      string
}

type NodeCachePage struct {
	Nodes      []NormalizedNode `json:"nodes"`
	Offset     int              `json:"offset"`
	Limit      int              `json:"limit"`
	Total      int              `json:"total"`
	NextOffset int              `json:"nextOffset"`
	HasMore    bool             `json:"hasMore"`
}

type HealthCheckSample struct {
	ID           int64     `json:"id,omitempty"`
	NodeID       string    `json:"nodeId"`
	CheckType    string    `json:"checkType"`
	LatencyMS    int       `json:"latencyMs"`
	Success      bool      `json:"success"`
	ErrorSummary string    `json:"errorSummary,omitempty"`
	CheckedAt    time.Time `json:"checkedAt"`
}

type HealthCheckQuery struct {
	NodeID    string
	CheckType string
	Limit     int
}

type HealthTrendSummary struct {
	Samples          []HealthCheckSample `json:"samples"`
	Total            int                 `json:"total"`
	SuccessCount     int                 `json:"successCount"`
	FailureCount     int                 `json:"failureCount"`
	AverageLatencyMS int                 `json:"averageLatencyMs"`
	Limit            int                 `json:"limit"`
}

type HealthHistoryRetention struct {
	Before     time.Time
	MaxPerNode int
}

type OperationEvent struct {
	ID           int64          `json:"id,omitempty"`
	Severity     string         `json:"severity"`
	EventType    string         `json:"eventType"`
	ResourceType string         `json:"resourceType,omitempty"`
	ResourceID   string         `json:"resourceId,omitempty"`
	ProfileID    string         `json:"profileId,omitempty"`
	Core         Core           `json:"core,omitempty"`
	Message      string         `json:"message"`
	ErrorCode    string         `json:"errorCode,omitempty"`
	Context      map[string]any `json:"context,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
}

type OperationEventQuery struct {
	Offset       int
	Limit        int
	Since        time.Time
	Until        time.Time
	Severity     string
	EventType    string
	ResourceType string
	ResourceID   string
	ProfileID    string
	Core         Core
}

type OperationEventPage struct {
	Events     []OperationEvent `json:"events"`
	Offset     int              `json:"offset"`
	Limit      int              `json:"limit"`
	Total      int              `json:"total"`
	NextOffset int              `json:"nextOffset"`
	HasMore    bool             `json:"hasMore"`
}

type RuleSetResource struct {
	Metadata
	SupportedCores []Core              `json:"supportedCores,omitempty"`
	Rules          []NormalizedRule    `json:"rules,omitempty"`
	RuleCards      []RoutingRuleCardUI `json:"ruleCards,omitempty"`
}

type RoutingRuleCardUI struct {
	Enabled         bool                 `json:"enabled"`
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	OutboundTarget  *RoutingRuleTargetUI `json:"outboundTarget,omitempty"`
	Rules           []RoutingRuleLeafUI  `json:"rules,omitempty"`
	SourceRule      *NormalizedRule      `json:"sourceRule,omitempty"`
	SourceSignature string               `json:"sourceSignature,omitempty"`
}

type RoutingRuleTargetUI struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type RoutingRuleLeafUI struct {
	Condition string `json:"condition"`
	ID        string `json:"id"`
	Target    string `json:"target"`
	Value     any    `json:"value"`
}

type RuleSourceRepositoryProvider string

const (
	RuleSourceRepositoryProviderGitHub RuleSourceRepositoryProvider = "github"
)

type RuleSourceCoreMapping struct {
	Core     Core   `json:"core"`
	Ref      string `json:"ref"`
	RootPath string `json:"rootPath,omitempty"`
}

type RuleSourceRepository struct {
	Metadata
	Provider       RuleSourceRepositoryProvider `json:"provider"`
	Owner          string                       `json:"owner"`
	Repository     string                       `json:"repository"`
	BuiltIn        bool                         `json:"builtIn,omitempty"`
	CoreMappings   []RuleSourceCoreMapping      `json:"coreMappings,omitempty"`
	SupportedCores []Core                       `json:"supportedCores,omitempty"`
}

type RuleSourceTreeEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

type RuleSourceTree struct {
	RepositoryID string                `json:"repositoryId"`
	Core         Core                  `json:"core"`
	Ref          string                `json:"ref"`
	Path         string                `json:"path,omitempty"`
	Entries      []RuleSourceTreeEntry `json:"entries,omitempty"`
}

type RuleSourceSelectableFile struct {
	Name     string       `json:"name"`
	Path     string       `json:"path"`
	Kind     ResourceKind `json:"kind"`
	Format   string       `json:"format,omitempty"`
	Behavior string       `json:"behavior,omitempty"`
}

type RuleSourceSelectableFiles struct {
	RepositoryID string                     `json:"repositoryId"`
	Core         Core                       `json:"core"`
	Ref          string                     `json:"ref"`
	RefreshedAt  time.Time                  `json:"refreshedAt"`
	Files        []RuleSourceSelectableFile `json:"files,omitempty"`
}

type RuleSourceIndexFile struct {
	Core        Core         `json:"core"`
	Path        string       `json:"path"`
	LogicalPath string       `json:"logicalPath"`
	Name        string       `json:"name"`
	Kind        ResourceKind `json:"kind"`
	Format      string       `json:"format,omitempty"`
	Behavior    string       `json:"behavior,omitempty"`
	RawURL      string       `json:"rawUrl"`
}

type RuleSourceIndexEntry struct {
	LogicalPath string                       `json:"logicalPath"`
	Name        string                       `json:"name"`
	Files       map[Core]RuleSourceIndexFile `json:"files"`
}

type RuleSourceIndexDirectory struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type RuleSourceIndex struct {
	RepositoryID string                     `json:"repositoryId"`
	Owner        string                     `json:"owner"`
	Repository   string                     `json:"repository"`
	Path         string                     `json:"path,omitempty"`
	Refs         map[Core]string            `json:"refs"`
	RefreshedAt  time.Time                  `json:"refreshedAt,omitempty"`
	Offset       int                        `json:"offset,omitempty"`
	Limit        int                        `json:"limit,omitempty"`
	Total        int                        `json:"total,omitempty"`
	NextOffset   int                        `json:"nextOffset,omitempty"`
	HasMore      bool                       `json:"hasMore,omitempty"`
	Directories  []RuleSourceIndexDirectory `json:"directories,omitempty"`
	Entries      []RuleSourceIndexEntry     `json:"entries,omitempty"`
}

type RuleSourceIndexSearchFilters struct {
	Offset     int
	Core       Core
	Format     string
	Behavior   string
	Kind       ResourceKind
	PathPrefix string
}

type RuleAssetSourceMode string

const (
	RuleAssetSourceModeRepositoryFile RuleAssetSourceMode = "repository-file"
	RuleAssetSourceModeRemote         RuleAssetSourceMode = "remote"
	RuleAssetSourceModeLocal          RuleAssetSourceMode = "local"
)

type SingBoxRuleSetResource struct {
	Metadata
	Tag            string              `json:"tag"`
	SourceMode     RuleAssetSourceMode `json:"sourceMode"`
	RepositoryID   string              `json:"repositoryId,omitempty"`
	Ref            string              `json:"ref,omitempty"`
	Path           string              `json:"path,omitempty"`
	URL            string              `json:"url,omitempty"`
	LocalPath      string              `json:"localPath,omitempty"`
	Format         string              `json:"format,omitempty"`
	UpdateInterval string              `json:"updateInterval,omitempty"`
}

type MihomoRuleProviderResource struct {
	Metadata
	Provider     string              `json:"provider"`
	SourceMode   RuleAssetSourceMode `json:"sourceMode"`
	RepositoryID string              `json:"repositoryId,omitempty"`
	Ref          string              `json:"ref,omitempty"`
	Path         string              `json:"path,omitempty"`
	URL          string              `json:"url,omitempty"`
	LocalPath    string              `json:"localPath,omitempty"`
	Behavior     string              `json:"behavior,omitempty"`
	Format       string              `json:"format,omitempty"`
	Interval     string              `json:"interval,omitempty"`
}

type GroupSetResource struct {
	Metadata
	Groups []NormalizedGroup `json:"groups,omitempty"`
}

type ProfileResource struct {
	Metadata
	SelectedCore    Core     `json:"selectedCore"`
	SubscriptionIDs []string `json:"subscriptionIds,omitempty"`
	NodeSetIDs      []string `json:"nodeSetIds,omitempty"`
	RuleSetIDs      []string `json:"ruleSetIds,omitempty"`
	GroupSetIDs     []string `json:"groupSetIds,omitempty"`
}

type GlobalConfig struct {
	Fields     map[string]any    `json:"fields,omitempty"`
	DNSServers []GlobalDNSServer `json:"dnsServers,omitempty"`
	DNSRules   []GlobalDNSRule   `json:"dnsRules,omitempty"`
	Inbounds   []ManagedInbound  `json:"inbounds,omitempty"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

type GlobalDNSServer struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Role           string `json:"role"`
	Protocol       string `json:"protocol"`
	Address        string `json:"address"`
	Port           string `json:"port,omitempty"`
	Path           string `json:"path,omitempty"`
	Detour         string `json:"detour,omitempty"`
	ClientSubnet   string `json:"clientSubnet,omitempty"`
	SkipCertVerify bool   `json:"skipCertVerify,omitempty"`
}

type GlobalDNSRule struct {
	ID           string `json:"id"`
	Matcher      string `json:"matcher"`
	Value        string `json:"value"`
	ServerName   string `json:"serverName,omitempty"`
	Strategy     string `json:"strategy,omitempty"`
	ClientSubnet string `json:"clientSubnet,omitempty"`
}

type Bootstrap struct {
	Profiles               []ProfileResource            `json:"profiles"`
	Subscriptions          []SubscriptionResource       `json:"subscriptions"`
	NodeSets               []NodeSetResource            `json:"nodeSets"`
	RoutingRuleSets        []RuleSetResource            `json:"routingRuleSets"`
	RuleSourceRepositories []RuleSourceRepository       `json:"ruleSourceRepositories"`
	SingBoxRuleSets        []SingBoxRuleSetResource     `json:"singBoxRuleSets"`
	MihomoRuleProviders    []MihomoRuleProviderResource `json:"mihomoRuleProviders"`
	GroupSets              []GroupSetResource           `json:"groupSets"`
	Config                 GlobalConfig                 `json:"config"`
}

func NewProfile(name string) ProfileResource {
	now := time.Now().UTC()
	if name == "" {
		name = "Untitled profile"
	}
	return ProfileResource{
		Metadata: Metadata{
			ID:         NewID("profile"),
			Kind:       KindProfile,
			Name:       name,
			OriginType: OriginManual,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		SelectedCore: CoreMihomo,
	}
}

func SubscriptionNodeSetName(subscriptionName string) string {
	return subscriptionName
}

func SubscriptionRuleSetName(subscriptionName string) string {
	return subscriptionName
}

func SubscriptionGroupSetName(subscriptionName string) string {
	return subscriptionName
}
