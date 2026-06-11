package subsync

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/atlantis-mk/FastProxy-Server/internal/importer"
	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

type Service struct {
	store  *repository.Store
	logger *slog.Logger
	client *http.Client
}

func NewService(store *repository.Store, logger *slog.Logger) *Service {
	return &Service{
		store:  store,
		logger: logger,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *Service) RefreshSubscription(ctx context.Context, id string) (repository.SubscriptionResource, error) {
	subscription, err := s.store.GetSubscription(id)
	if err != nil {
		return repository.SubscriptionResource{}, err
	}

	content, revision, resolvedURL, err := s.download(ctx, subscription)
	if err != nil {
		return s.markSyncFailure(subscription, err)
	}
	normalized, _, err := importer.ParseClashContent(content)
	if err != nil {
		return s.markSyncFailure(subscription, err)
	}

	subscription.SourceURL = resolvedURL
	subscription.Revision = revision
	subscription.Sync = repository.SubscriptionSyncStatus{
		LastSyncedAt: time.Now().UTC(),
	}

	nodeSet, err := s.upsertNodeSet(subscription, normalized)
	if err != nil {
		return s.markSyncFailure(subscription, err)
	}
	if err := s.store.UpsertNodeCache(repository.NodeCacheUpsert{
		SourceType:     repository.NodeCacheSourceSubscription,
		SourceID:       subscription.ID,
		SubscriptionID: subscription.ID,
		NodeSetID:      nodeSet.ID,
		RefreshedAt:    subscription.Sync.LastSyncedAt,
		Nodes:          nodeSet.Nodes,
	}); err != nil {
		return s.markSyncFailure(subscription, err)
	}
	if _, err := s.upsertGroupSet(subscription, normalized); err != nil {
		return s.markSyncFailure(subscription, err)
	}
	if _, err := s.upsertRuleSet(subscription, normalized); err != nil {
		return s.markSyncFailure(subscription, err)
	}

	updated, err := s.store.UpdateSubscription(subscription.ID, subscription)
	if err != nil {
		return repository.SubscriptionResource{}, err
	}
	return updated, nil
}

func (s *Service) download(ctx context.Context, subscription repository.SubscriptionResource) (content string, revision string, resolvedURL string, err error) {
	requestURL, resolvedURL, err := buildRequestURL(subscription)
	if err != nil {
		return "", "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", "", "", err
	}
	userAgent := strings.TrimSpace(subscription.Fetch.UserAgent)
	if userAgent == "" {
		userAgent = "Clash"
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", "", "", fmt.Errorf("subscription request failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", err
	}
	revision = strings.TrimSpace(resp.Header.Get("ETag"))
	if revision == "" {
		revision = strings.TrimSpace(resp.Header.Get("Last-Modified"))
	}
	return string(body), revision, resolvedURL, nil
}

func buildRequestURL(subscription repository.SubscriptionResource) (string, string, error) {
	sources := extractSourceURLs(subscription.Fetch.SourceInput, subscription.SourceURL)
	if len(sources) == 0 {
		return "", "", fmt.Errorf("subscription source URL is required")
	}
	resolvedURL := sources[0]
	return resolvedURL, resolvedURL, nil
}

func extractSourceURLs(sourceInput string, sourceURL string) []string {
	parts := strings.FieldsFunc(strings.TrimSpace(sourceInput), func(r rune) bool {
		return r == '\n' || r == '\r' || r == '|'
	})
	if len(parts) == 0 && strings.TrimSpace(sourceURL) != "" {
		parts = []string{sourceURL}
	}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func (s *Service) upsertNodeSet(subscription repository.SubscriptionResource, normalized repository.NormalizedConfig) (repository.NodeSetResource, error) {
	input := repository.NodeSetResource{
		Metadata: repository.Metadata{
			Name:        repository.SubscriptionNodeSetName(subscription.Name),
			Description: subscription.Description,
			OriginType:  subscription.OriginType,
		},
		Nodes: markNodeSource(normalized.Nodes, subscription.Name),
	}
	input.ID = repository.SubscriptionNodeSetName(subscription.Name)
	return s.store.UpsertManagedNodeSet(input)
}

func (s *Service) upsertGroupSet(subscription repository.SubscriptionResource, normalized repository.NormalizedConfig) (repository.GroupSetResource, error) {
	input := repository.GroupSetResource{
		Metadata: repository.Metadata{
			Name:        repository.SubscriptionGroupSetName(subscription.Name),
			Description: subscription.Description,
			OriginType:  subscription.OriginType,
		},
		Groups: normalized.Groups,
	}
	input.ID = repository.SubscriptionGroupSetName(subscription.Name)
	if _, err := s.store.GetGroupSet(input.ID); err == nil {
		return s.store.UpdateGroupSet(input.ID, input)
	}
	return s.store.CreateGroupSet(input)
}

func (s *Service) upsertRuleSet(subscription repository.SubscriptionResource, normalized repository.NormalizedConfig) (repository.RuleSetResource, error) {
	input := repository.RuleSetResource{
		Metadata: repository.Metadata{
			Name:        repository.SubscriptionRuleSetName(subscription.Name),
			Description: subscription.Description,
			OriginType:  subscription.OriginType,
		},
		Rules: normalized.Rules,
	}
	input.ID = repository.SubscriptionRuleSetName(subscription.Name)
	if _, err := s.store.GetRuleSet(input.ID); err == nil {
		return s.store.UpdateRuleSet(input.ID, input)
	}
	return s.store.CreateRuleSet(input)
}

func (s *Service) markSyncFailure(subscription repository.SubscriptionResource, syncErr error) (repository.SubscriptionResource, error) {
	subscription.Sync = repository.SubscriptionSyncStatus{
		LastSyncedAt:  subscription.Sync.LastSyncedAt,
		LastSyncError: syncErr.Error(),
	}
	updated, err := s.store.UpdateSubscription(subscription.ID, subscription)
	if err != nil {
		return repository.SubscriptionResource{}, err
	}
	s.logger.Warn("subscription sync failed", "id", subscription.ID, "name", subscription.Name, "error", syncErr)
	return updated, syncErr
}

func markNodeSource(nodes []repository.NormalizedNode, source string) []repository.NormalizedNode {
	result := make([]repository.NormalizedNode, 0, len(nodes))
	for _, node := range nodes {
		node.Source = source
		result = append(result, node)
	}
	return result
}

type AutoUpdater struct {
	store    *repository.Store
	service  *Service
	logger   *slog.Logger
	interval time.Duration
}

func NewAutoUpdater(store *repository.Store, service *Service, logger *slog.Logger) *AutoUpdater {
	return &AutoUpdater{
		store:    store,
		service:  service,
		logger:   logger,
		interval: time.Minute,
	}
}

func (a *AutoUpdater) Start(ctx context.Context) {
	a.scan(ctx)

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.scan(ctx)
		}
	}
}

func (a *AutoUpdater) scan(ctx context.Context) {
	subscriptions, err := a.store.ListSubscriptions()
	if err != nil {
		a.logger.Warn("auto update list subscriptions failed", "error", err)
		return
	}
	now := time.Now().UTC()
	for _, subscription := range subscriptions {
		if !shouldRefresh(subscription, now) {
			continue
		}
		if _, err := a.service.RefreshSubscription(ctx, subscription.ID); err != nil {
			a.logger.Warn("auto update refresh failed", "id", subscription.ID, "error", err)
		}
	}
}

func shouldRefresh(subscription repository.SubscriptionResource, now time.Time) bool {
	if !subscription.AutoUpdate.Enabled {
		return false
	}
	if subscription.AutoUpdate.IntervalMinutes <= 0 {
		return false
	}
	if len(extractSourceURLs(subscription.Fetch.SourceInput, subscription.SourceURL)) == 0 {
		return false
	}
	if subscription.Sync.LastSyncedAt.IsZero() {
		return true
	}
	nextSyncAt := subscription.Sync.LastSyncedAt.Add(time.Duration(subscription.AutoUpdate.IntervalMinutes) * time.Minute)
	return !nextSyncAt.After(now)
}
