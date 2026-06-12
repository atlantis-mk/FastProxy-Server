package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/atlantis-mk/FastProxy-Server/internal/appconfig"
	"github.com/atlantis-mk/FastProxy-Server/internal/core"
	"github.com/atlantis-mk/FastProxy-Server/internal/httpjson"
	"github.com/atlantis-mk/FastProxy-Server/internal/importer"
	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
	"github.com/atlantis-mk/FastProxy-Server/internal/subsync"
	"github.com/atlantis-mk/FastProxy-Server/internal/webui"
)

type Server struct {
	cfg      appconfig.Config
	logger   *slog.Logger
	store    *repository.Store
	settings *appconfig.SettingsStore
	imports  *importer.Service
	syncs    *subsync.Service
	browser  *repository.RuleSourceRepositoryBrowser
	sv       *core.Supervisor
	mux      *http.ServeMux
}

func NewServer(cfg appconfig.Config, logger *slog.Logger, store *repository.Store) *Server {
	server := &Server{
		cfg:      cfg,
		logger:   logger,
		store:    store,
		settings: appconfig.NewSettingsStore(cfg.DataDir),
		imports:  importer.NewService(store),
		syncs:    subsync.NewService(store, logger),
		browser:  repository.NewRuleSourceRepositoryBrowser(),
		sv:       core.NewSupervisor(logger),
		mux:      http.NewServeMux(),
	}
	core.SetGitHubTokenProvider(server.settings.GitHubToken)
	server.browser.SetGitHubTokenProvider(server.githubToken)
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.logRequests(s.mux)
}

type coreInventoryItem struct {
	Core             repository.Core `json:"core"`
	BinaryName       string          `json:"binaryName"`
	Configured       bool            `json:"configured"`
	ConfiguredPath   string          `json:"configuredPath,omitempty"`
	Cached           bool            `json:"cached"`
	CachedPath       string          `json:"cachedPath,omitempty"`
	CachedVersion    string          `json:"cachedVersion,omitempty"`
	FirstStartAction string          `json:"firstStartAction"`
}

type githubTokenSetting struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source,omitempty"`
}

func ListenAndServe(ctx context.Context, addr string, handler http.Handler) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/bootstrap", s.handleBootstrap)
	s.mux.HandleFunc("GET /api/repository/bootstrap", s.handleRepositoryBootstrap)
	s.mux.HandleFunc("GET /api/operation-events", s.handleQueryOperationEvents)
	s.mux.HandleFunc("GET /api/settings/github-token", s.handleGetGitHubTokenSetting)
	s.mux.HandleFunc("PUT /api/settings/github-token", s.handleSaveGitHubTokenSetting)
	s.mux.HandleFunc("GET /api/cores", s.handleCoreInventory)
	s.mux.HandleFunc("POST /api/cores/{core}/check-update", s.handleCoreCheckUpdate)
	s.mux.HandleFunc("POST /api/cores/{core}/update", s.handleCoreUpdate)
	s.mux.HandleFunc("POST /api/cores/{core}/upload", s.handleCoreUpload)
	s.mux.HandleFunc("GET /api/runtime/status", s.handleRuntimeStatus)
	s.mux.HandleFunc("PUT /api/runtime/core", s.handleSelectRuntimeCore)
	s.mux.HandleFunc("POST /api/runtime/start", s.handleRuntimeStart)
	s.mux.HandleFunc("POST /api/runtime/stop", s.handleRuntimeStop)
	s.mux.HandleFunc("POST /api/runtime/restart", s.handleRuntimeRestart)
	s.mux.HandleFunc("POST /api/runtime/compile-and-start", s.handleRuntimeRestartAndApply)
	s.mux.HandleFunc("POST /api/runtime/restart-and-apply", s.handleRuntimeRestartAndApply)
	s.mux.HandleFunc("/api/runtime/controller/", s.handleRuntimeControllerProxy)
	s.mux.HandleFunc("/api/runtime/controller", s.handleRuntimeControllerProxy)
	s.mux.HandleFunc("GET /api/profiles", s.handleListProfiles)
	s.mux.HandleFunc("POST /api/profiles", s.handleCreateProfile)
	s.mux.HandleFunc("GET /api/profiles/{id}", s.handleGetProfile)
	s.mux.HandleFunc("PUT /api/profiles/{id}", s.handleUpdateProfile)
	s.mux.HandleFunc("DELETE /api/profiles/{id}", s.handleDeleteProfile)
	s.mux.HandleFunc("GET /api/repository/subscriptions", s.handleListSubscriptions)
	s.mux.HandleFunc("POST /api/repository/subscriptions", s.handleCreateSubscription)
	s.mux.HandleFunc("GET /api/repository/subscriptions/{id}", s.handleGetSubscription)
	s.mux.HandleFunc("PUT /api/repository/subscriptions/{id}", s.handleUpdateSubscription)
	s.mux.HandleFunc("DELETE /api/repository/subscriptions/{id}", s.handleDeleteSubscription)
	s.mux.HandleFunc("POST /api/repository/subscriptions/{id}/refresh", s.handleRefreshSubscription)
	s.mux.HandleFunc("GET /api/repository/node-sets/files", s.handleListNodeSetFiles)
	s.mux.HandleFunc("GET /api/repository/node-sets", s.handleListNodeSets)
	s.mux.HandleFunc("GET /api/repository/nodes", s.handleQueryNodes)
	s.mux.HandleFunc("GET /api/repository/nodes/health/latest", s.handleLatestNodeHealth)
	s.mux.HandleFunc("GET /api/repository/nodes/health/trend", s.handleNodeHealthTrend)
	s.mux.HandleFunc("POST /api/repository/nodes/health/cleanup", s.handleCleanupNodeHealth)
	s.mux.HandleFunc("POST /api/repository/nodes/health-check", s.handleCheckNodeHealth)
	s.mux.HandleFunc("POST /api/repository/node-sets", s.handleCreateNodeSet)
	s.mux.HandleFunc("GET /api/repository/node-sets/{id}", s.handleGetNodeSet)
	s.mux.HandleFunc("PUT /api/repository/node-sets/{id}", s.handleUpdateNodeSet)
	s.mux.HandleFunc("DELETE /api/repository/node-sets/{id}", s.handleDeleteNodeSet)
	s.mux.HandleFunc("GET /api/repository/routing-rule-sets", s.handleListRoutingRuleSets)
	s.mux.HandleFunc("POST /api/repository/routing-rule-sets", s.handleCreateRoutingRuleSet)
	s.mux.HandleFunc("GET /api/repository/routing-rule-sets/{id}", s.handleGetRoutingRuleSet)
	s.mux.HandleFunc("PUT /api/repository/routing-rule-sets/{id}", s.handleUpdateRoutingRuleSet)
	s.mux.HandleFunc("DELETE /api/repository/routing-rule-sets/{id}", s.handleDeleteRoutingRuleSet)
	s.mux.HandleFunc("GET /api/repository/rule-source-repositories", s.handleListRuleSourceRepositories)
	s.mux.HandleFunc("POST /api/repository/rule-source-repositories", s.handleCreateRuleSourceRepository)
	s.mux.HandleFunc("GET /api/repository/rule-source-repositories/{id}", s.handleGetRuleSourceRepository)
	s.mux.HandleFunc("PUT /api/repository/rule-source-repositories/{id}", s.handleUpdateRuleSourceRepository)
	s.mux.HandleFunc("DELETE /api/repository/rule-source-repositories/{id}", s.handleDeleteRuleSourceRepository)
	s.mux.HandleFunc("GET /api/repository/rule-source-repositories/{id}/tree", s.handleBrowseRuleSourceRepositoryTree)
	s.mux.HandleFunc("GET /api/repository/rule-source-repositories/{id}/index", s.handleGetRuleSourceRepositoryIndex)
	s.mux.HandleFunc("GET /api/repository/rule-source-repositories/{id}/index/search", s.handleSearchRuleSourceRepositoryIndex)
	s.mux.HandleFunc("POST /api/repository/rule-source-repositories/{id}/index/refresh", s.handleRefreshRuleSourceRepositoryIndex)
	s.mux.HandleFunc("POST /api/repository/rule-source-repositories/{id}/selectable-files/refresh", s.handleRefreshRuleSourceSelectableFiles)
	s.mux.HandleFunc("GET /api/repository/sing-box-rule-sets", s.handleListSingBoxRuleSets)
	s.mux.HandleFunc("POST /api/repository/sing-box-rule-sets", s.handleCreateSingBoxRuleSet)
	s.mux.HandleFunc("GET /api/repository/sing-box-rule-sets/{id}", s.handleGetSingBoxRuleSet)
	s.mux.HandleFunc("PUT /api/repository/sing-box-rule-sets/{id}", s.handleUpdateSingBoxRuleSet)
	s.mux.HandleFunc("DELETE /api/repository/sing-box-rule-sets/{id}", s.handleDeleteSingBoxRuleSet)
	s.mux.HandleFunc("GET /api/repository/mihomo-rule-providers", s.handleListMihomoRuleProviders)
	s.mux.HandleFunc("POST /api/repository/mihomo-rule-providers", s.handleCreateMihomoRuleProvider)
	s.mux.HandleFunc("GET /api/repository/mihomo-rule-providers/{id}", s.handleGetMihomoRuleProvider)
	s.mux.HandleFunc("PUT /api/repository/mihomo-rule-providers/{id}", s.handleUpdateMihomoRuleProvider)
	s.mux.HandleFunc("DELETE /api/repository/mihomo-rule-providers/{id}", s.handleDeleteMihomoRuleProvider)
	s.mux.HandleFunc("GET /api/repository/group-sets", s.handleListGroupSets)
	s.mux.HandleFunc("POST /api/repository/group-sets", s.handleCreateGroupSet)
	s.mux.HandleFunc("GET /api/repository/group-sets/{id}", s.handleGetGroupSet)
	s.mux.HandleFunc("PUT /api/repository/group-sets/{id}", s.handleUpdateGroupSet)
	s.mux.HandleFunc("DELETE /api/repository/group-sets/{id}", s.handleDeleteGroupSet)
	s.mux.HandleFunc("GET /api/repository/config", s.handleGetGlobalConfig)
	s.mux.HandleFunc("PUT /api/repository/config", s.handleUpdateGlobalConfig)
	s.mux.HandleFunc("GET /api/repository/config/inbounds", s.handleGetGlobalInbounds)
	s.mux.HandleFunc("PUT /api/repository/config/inbounds", s.handleUpdateGlobalInbounds)
	s.mux.HandleFunc("POST /api/repository/imports/clash", s.handleImportClash)
	s.mux.HandleFunc("POST /api/repository/imports/plain-nodes", s.handleImportPlainNodes)
	s.mux.HandleFunc("POST /api/repository/imports/manual-node", s.handleImportManualNode)
	s.mux.HandleFunc("DELETE /api/repository/imports/manual-node", s.handleDeleteManualNode)
	s.mux.Handle("/", newWebAppHandler(webui.EmbeddedDist()))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpjson.Write(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "fastproxy-server",
	})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	httpjson.Write(w, http.StatusOK, map[string]any{
		"dataDir": s.cfg.DataDir,
	})
}

func (s *Server) handleGetGitHubTokenSetting(w http.ResponseWriter, r *http.Request) {
	httpjson.Write(w, http.StatusOK, s.githubTokenSetting())
}

func (s *Server) handleSaveGitHubTokenSetting(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_github_token_setting", err.Error())
		return
	}
	settings, err := s.settings.Get()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "github_token_setting_read_failed", err.Error())
		return
	}
	settings.GitHubToken = input.Token
	if err := s.settings.Save(settings); err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "github_token_setting_save_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, s.githubTokenSetting())
}

func (s *Server) githubTokenSetting() githubTokenSetting {
	settings, err := s.settings.Get()
	if err == nil && settings.GitHubToken != "" {
		return githubTokenSetting{Configured: true, Source: "saved"}
	}
	if os.Getenv("GITHUB_TOKEN") != "" {
		return githubTokenSetting{Configured: true, Source: "environment"}
	}
	return githubTokenSetting{}
}

func (s *Server) githubToken() string {
	if token := strings.TrimSpace(s.settings.GitHubToken()); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

func (s *Server) handleCoreInventory(w http.ResponseWriter, r *http.Request) {
	items := []coreInventoryItem{
		s.coreInventoryFor(repository.CoreMihomo, s.cfg.MihomoBinaryPath),
		s.coreInventoryFor(repository.CoreSingBox, s.cfg.SingBoxBinaryPath),
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"cores": items})
}

func (s *Server) handleCoreCheckUpdate(w http.ResponseWriter, r *http.Request) {
	coreName := repository.Core(r.PathValue("core"))
	if err := core.ValidateCore(coreName); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "unsupported_core", err.Error())
		return
	}
	info, err := core.CheckUpdate(r.Context(), s.cfg.DataDir, coreName)
	if err != nil {
		httpjson.WriteError(w, http.StatusBadGateway, "core_update_check_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, info)
}

func (s *Server) handleCoreUpdate(w http.ResponseWriter, r *http.Request) {
	coreName := repository.Core(r.PathValue("core"))
	if err := core.ValidateCore(coreName); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "unsupported_core", err.Error())
		return
	}
	info, err := core.DownloadLatest(r.Context(), s.cfg.DataDir, coreName)
	if err != nil {
		httpjson.WriteError(w, http.StatusBadGateway, "core_update_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, info)
}

func (s *Server) handleCoreUpload(w http.ResponseWriter, r *http.Request) {
	coreName := repository.Core(r.PathValue("core"))
	if err := core.ValidateCore(coreName); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "unsupported_core", err.Error())
		return
	}
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_core_upload", err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "core_upload_file_missing", err.Error())
		return
	}
	defer file.Close()
	cache, err := core.InstallLocal(s.cfg.DataDir, coreName, header.Filename, file)
	if err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "core_upload_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, cache)
}

func (s *Server) coreInventoryFor(coreName repository.Core, configuredPath string) coreInventoryItem {
	cache := core.CachedBinary(s.cfg.DataDir, coreName)
	firstStartAction := "download"
	if configuredPath != "" || cache.Exists {
		firstStartAction = "use-local"
	}
	return coreInventoryItem{
		Core:             coreName,
		BinaryName:       core.CoreBinaryName(coreName),
		Configured:       configuredPath != "",
		ConfiguredPath:   configuredPath,
		Cached:           cache.Exists,
		CachedPath:       cache.Path,
		CachedVersion:    cache.Version,
		FirstStartAction: firstStartAction,
	}
}

func (s *Server) handleRepositoryBootstrap(w http.ResponseWriter, r *http.Request) {
	bootstrap, err := s.store.Bootstrap()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "repository_bootstrap_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, bootstrap)
}

func (s *Server) handleQueryOperationEvents(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	since, err := parseOptionalTime(query.Get("since"))
	if err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_operation_event_query", "since must be RFC3339Nano")
		return
	}
	until, err := parseOptionalTime(query.Get("until"))
	if err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_operation_event_query", "until must be RFC3339Nano")
		return
	}
	page, err := s.store.QueryOperationEvents(repository.OperationEventQuery{
		Offset:       parseOptionalPositiveInt(query.Get("offset"), 0),
		Limit:        parseOptionalPositiveInt(query.Get("limit"), 100),
		Since:        since,
		Until:        until,
		Severity:     strings.TrimSpace(query.Get("severity")),
		EventType:    strings.TrimSpace(query.Get("eventType")),
		ResourceType: strings.TrimSpace(query.Get("resourceType")),
		ResourceID:   strings.TrimSpace(query.Get("resourceId")),
		ProfileID:    strings.TrimSpace(query.Get("profileId")),
		Core:         repository.Core(strings.TrimSpace(query.Get("core"))),
	})
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "operation_events_query_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, page)
}

func (s *Server) handleSelectRuntimeCore(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Core repository.Core `json:"core"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_runtime_core", "request body must be valid JSON")
		return
	}
	if err := core.ValidateCore(input.Core); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "unsupported_core", err.Error())
		return
	}
	config, err := s.store.GlobalConfig()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "global_config_read_failed", err.Error())
		return
	}
	if config.Fields == nil {
		config.Fields = map[string]any{}
	}
	config.Fields["selectedCore"] = string(input.Core)
	updated, err := s.store.UpdateGlobalConfig(config)
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "runtime_core_update_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, updated)
}

func (s *Server) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.store.ListProfiles()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "profiles_read_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, profiles)
}

func (s *Server) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	var input repository.ProfileResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_profile", "request body must be valid JSON")
		return
	}
	item, err := s.store.CreateProfile(input)
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "profile_create_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusCreated, item)
}

func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetProfile(r.PathValue("id"))
	if err != nil {
		status := http.StatusInternalServerError
		code := "profile_read_failed"
		if errors.Is(err, repository.ErrNotFound) {
			status = http.StatusNotFound
			code = "profile_not_found"
		}
		httpjson.WriteError(w, status, code, err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, item)
}

func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var input repository.ProfileResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_profile", "request body must be valid JSON")
		return
	}
	item, err := s.store.UpdateProfile(r.PathValue("id"), input)
	if err != nil {
		status := http.StatusInternalServerError
		code := "profile_update_failed"
		if errors.Is(err, repository.ErrNotFound) {
			status = http.StatusNotFound
			code = "profile_not_found"
		}
		httpjson.WriteError(w, status, code, err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, item)
}

func (s *Server) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteProfile(r.PathValue("id")); err != nil {
		status := http.StatusInternalServerError
		code := "profile_delete_failed"
		if errors.Is(err, repository.ErrNotFound) {
			status = http.StatusNotFound
			code = "profile_not_found"
		}
		httpjson.WriteError(w, status, code, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetGlobalInbounds(w http.ResponseWriter, r *http.Request) {
	inbounds, err := s.store.GlobalInbounds()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "global_inbounds_read_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, inbounds)
}

func (s *Server) handleGetGlobalConfig(w http.ResponseWriter, r *http.Request) {
	config, err := s.store.GlobalConfig()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "global_config_read_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, config)
}

func (s *Server) handleUpdateGlobalConfig(w http.ResponseWriter, r *http.Request) {
	var input repository.GlobalConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_global_config", "request body must be valid JSON")
		return
	}
	config, err := s.store.UpdateGlobalConfig(input)
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "global_config_update_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, config)
}

func (s *Server) handleUpdateGlobalInbounds(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Inbounds []repository.ManagedInbound `json:"inbounds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_global_inbounds", "request body must be valid JSON")
		return
	}
	config, err := s.store.UpdateGlobalInbounds(input.Inbounds)
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "global_inbounds_update_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, config)
}

func (s *Server) handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListSubscriptions()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "subscriptions_read_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, items)
}

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetSubscription(r.PathValue("id"))
	s.writeRepositoryResponse(w, item, err, "subscription")
}

func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	var input repository.SubscriptionResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_subscription", "request body must be valid JSON")
		return
	}
	item, err := s.store.CreateSubscription(input)
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "subscription_create_failed", err.Error())
		return
	}
	if shouldSyncSubscription(item) {
		synced, syncErr := s.syncs.RefreshSubscription(r.Context(), item.ID)
		if syncErr == nil {
			item = synced
		} else if errors.Is(syncErr, repository.ErrNotFound) {
			httpjson.WriteError(w, http.StatusNotFound, "subscription_not_found", syncErr.Error())
			return
		} else {
			item = synced
		}
	}
	httpjson.Write(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	var input repository.SubscriptionResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_subscription", "request body must be valid JSON")
		return
	}
	item, err := s.store.UpdateSubscription(r.PathValue("id"), input)
	if err != nil {
		s.writeRepositoryMutation(w, item, err, "subscription_update_failed", "subscription")
		return
	}
	if shouldSyncSubscription(item) {
		synced, syncErr := s.syncs.RefreshSubscription(r.Context(), item.ID)
		if syncErr == nil {
			item = synced
		} else if errors.Is(syncErr, repository.ErrNotFound) {
			httpjson.WriteError(w, http.StatusNotFound, "subscription_not_found", syncErr.Error())
			return
		} else {
			item = synced
		}
	}
	httpjson.Write(w, http.StatusOK, item)
}

func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	s.deleteRepositoryResource(w, s.store.DeleteSubscription(r.PathValue("id")), "subscription_delete_failed", "subscription")
}

func (s *Server) handleRefreshSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item, err := s.syncs.RefreshSubscription(r.Context(), id)
	if err != nil {
		s.recordOperationEvent(repository.OperationEvent{
			Severity:     "error",
			EventType:    "subscription.refresh.failed",
			ResourceType: string(repository.KindSubscription),
			ResourceID:   id,
			Message:      "Subscription refresh failed",
			ErrorCode:    "subscription_refresh_failed",
			Context:      map[string]any{"error": err.Error()},
		})
		if errors.Is(err, repository.ErrNotFound) {
			httpjson.WriteError(w, http.StatusNotFound, "subscription_not_found", err.Error())
			return
		}
		httpjson.WriteError(w, http.StatusBadGateway, "subscription_refresh_failed", err.Error())
		return
	}
	s.recordOperationEvent(repository.OperationEvent{
		Severity:     "info",
		EventType:    "subscription.refresh.succeeded",
		ResourceType: string(repository.KindSubscription),
		ResourceID:   item.ID,
		Message:      "Subscription refreshed",
		Context: map[string]any{
			"revision": item.Revision,
		},
	})
	httpjson.Write(w, http.StatusOK, item)
}

func shouldSyncSubscription(item repository.SubscriptionResource) bool {
	return strings.TrimSpace(item.Fetch.SourceInput) != "" || strings.TrimSpace(item.SourceURL) != ""
}

func (s *Server) handleListNodeSets(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListNodeSets()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "node_sets_read_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, items)
}

func (s *Server) handleListNodeSetFiles(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListNodeSetFiles()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "node_set_files_read_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, items)
}

func (s *Server) handleQueryNodes(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, err := s.store.QueryNodeCache(repository.NodeCacheQuery{
		Offset:         parseOptionalPositiveInt(query.Get("offset"), 0),
		Limit:          parseOptionalPositiveInt(query.Get("limit"), 100),
		Query:          query.Get("q"),
		Protocol:       strings.TrimSpace(query.Get("protocol")),
		Address:        strings.TrimSpace(query.Get("address")),
		Tag:            strings.TrimSpace(query.Get("tag")),
		Source:         strings.TrimSpace(query.Get("source")),
		SubscriptionID: strings.TrimSpace(query.Get("subscriptionId")),
		NodeSetID:      strings.TrimSpace(query.Get("nodeSetId")),
	})
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "nodes_query_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, page)
}

type nodeHealthCheckRequest struct {
	Tag       string `json:"tag"`
	CheckType string `json:"checkType,omitempty"`
	TimeoutMS int    `json:"timeoutMs,omitempty"`
}

type nodeHealthCleanupRequest struct {
	Before     string `json:"before,omitempty"`
	MaxPerNode int    `json:"maxPerNode,omitempty"`
}

func (s *Server) handleCheckNodeHealth(w http.ResponseWriter, r *http.Request) {
	var input nodeHealthCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_node_health_check", "request body must be valid JSON")
		return
	}
	tag := strings.TrimSpace(input.Tag)
	if tag == "" {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_node_health_check", "tag is required")
		return
	}
	nodeID, node, err := s.store.FindNodeCacheNodeByTag(tag)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httpjson.WriteError(w, http.StatusNotFound, "node_not_found", err.Error())
			return
		}
		httpjson.WriteError(w, http.StatusInternalServerError, "node_read_failed", err.Error())
		return
	}
	sample := s.checkNodeTCP(r.Context(), nodeID, node, input)
	sample, err = s.store.RecordHealthCheckSample(sample)
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "node_health_check_record_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, sample)
}

func (s *Server) handleLatestNodeHealth(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	samples, err := s.store.LatestHealthCheckSamples(repository.HealthCheckQuery{
		NodeID:    strings.TrimSpace(query.Get("nodeId")),
		CheckType: strings.TrimSpace(query.Get("checkType")),
		Limit:     parseOptionalPositiveInt(query.Get("limit"), 100),
	})
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "node_health_latest_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, samples)
}

func (s *Server) handleNodeHealthTrend(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	trend, err := s.store.HealthCheckTrend(repository.HealthCheckQuery{
		NodeID:    strings.TrimSpace(query.Get("nodeId")),
		CheckType: strings.TrimSpace(query.Get("checkType")),
		Limit:     parseOptionalPositiveInt(query.Get("limit"), 100),
	})
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "node_health_trend_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, trend)
}

func (s *Server) handleCleanupNodeHealth(w http.ResponseWriter, r *http.Request) {
	var input nodeHealthCleanupRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_node_health_cleanup", "request body must be valid JSON")
		return
	}
	var before time.Time
	if strings.TrimSpace(input.Before) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, input.Before)
		if err != nil {
			httpjson.WriteError(w, http.StatusBadRequest, "invalid_node_health_cleanup", "before must be RFC3339Nano")
			return
		}
		before = parsed
	}
	deleted, err := s.store.CleanupHealthHistory(repository.HealthHistoryRetention{
		Before:     before,
		MaxPerNode: input.MaxPerNode,
	})
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "node_health_cleanup_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]int{"deleted": deleted})
}

func (s *Server) checkNodeTCP(ctx context.Context, nodeID string, node repository.NormalizedNode, input nodeHealthCheckRequest) repository.HealthCheckSample {
	checkType := strings.TrimSpace(input.CheckType)
	if checkType == "" {
		checkType = "tcp"
	}
	timeout := time.Duration(input.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	started := time.Now()
	sample := repository.HealthCheckSample{
		NodeID:    nodeID,
		CheckType: checkType,
		CheckedAt: started.UTC(),
	}
	if strings.TrimSpace(node.Server) == "" || node.ServerPort <= 0 {
		sample.ErrorSummary = "node address or port is missing"
		return sample
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(node.Server, strconv.Itoa(node.ServerPort)))
	if err != nil {
		sample.ErrorSummary = err.Error()
		return sample
	}
	_ = conn.Close()
	sample.Success = true
	sample.LatencyMS = int(time.Since(started).Milliseconds())
	return sample
}

func (s *Server) handleGetNodeSet(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetNodeSet(r.PathValue("id"))
	s.writeRepositoryResponse(w, item, err, "node_set")
}

func (s *Server) handleCreateNodeSet(w http.ResponseWriter, r *http.Request) {
	var input repository.NodeSetResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_node_set", "request body must be valid JSON")
		return
	}
	item, err := s.store.CreateNodeSet(input)
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "node_set_create_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateNodeSet(w http.ResponseWriter, r *http.Request) {
	var input repository.NodeSetResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_node_set", "request body must be valid JSON")
		return
	}
	item, err := s.store.UpdateNodeSet(r.PathValue("id"), input)
	s.writeRepositoryMutation(w, item, err, "node_set_update_failed", "node_set")
}

func (s *Server) handleDeleteNodeSet(w http.ResponseWriter, r *http.Request) {
	s.deleteRepositoryResource(w, s.store.DeleteNodeSet(r.PathValue("id")), "node_set_delete_failed", "node_set")
}

func (s *Server) handleListRoutingRuleSets(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListRuleSets()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "routing_rule_sets_read_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, items)
}

func (s *Server) handleGetRoutingRuleSet(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetRuleSet(r.PathValue("id"))
	s.writeRepositoryResponse(w, item, err, "routing_rule_set")
}

func (s *Server) handleCreateRoutingRuleSet(w http.ResponseWriter, r *http.Request) {
	var input repository.RuleSetResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_routing_rule_set", "request body must be valid JSON")
		return
	}
	item, err := s.store.CreateRuleSet(input)
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "routing_rule_set_create_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateRoutingRuleSet(w http.ResponseWriter, r *http.Request) {
	var input repository.RuleSetResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_routing_rule_set", "request body must be valid JSON")
		return
	}
	item, err := s.store.UpdateRuleSet(r.PathValue("id"), input)
	s.writeRepositoryMutation(w, item, err, "routing_rule_set_update_failed", "routing_rule_set")
}

func (s *Server) handleDeleteRoutingRuleSet(w http.ResponseWriter, r *http.Request) {
	s.deleteRepositoryResource(w, s.store.DeleteRuleSet(r.PathValue("id")), "routing_rule_set_delete_failed", "routing_rule_set")
}

func (s *Server) handleListRuleSourceRepositories(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListRuleSourceRepositories()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "rule_source_repositories_read_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, items)
}

func (s *Server) handleGetRuleSourceRepository(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetRuleSourceRepository(r.PathValue("id"))
	s.writeRepositoryResponse(w, item, err, "rule_source_repository")
}

func (s *Server) handleCreateRuleSourceRepository(w http.ResponseWriter, r *http.Request) {
	var input repository.RuleSourceRepository
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_rule_source_repository", "request body must be valid JSON")
		return
	}
	item, err := s.store.CreateRuleSourceRepository(input)
	if err != nil {
		s.writeRuleRepositoryError(w, err, "rule_source_repository_create_failed", "rule_source_repository")
		return
	}
	httpjson.Write(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateRuleSourceRepository(w http.ResponseWriter, r *http.Request) {
	var input repository.RuleSourceRepository
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_rule_source_repository", "request body must be valid JSON")
		return
	}
	item, err := s.store.UpdateRuleSourceRepository(r.PathValue("id"), input)
	if err != nil {
		s.writeRuleRepositoryError(w, err, "rule_source_repository_update_failed", "rule_source_repository")
		return
	}
	httpjson.Write(w, http.StatusOK, item)
}

func (s *Server) handleDeleteRuleSourceRepository(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRuleSourceRepository(r.PathValue("id")); err != nil {
		s.writeRuleRepositoryError(w, err, "rule_source_repository_delete_failed", "rule_source_repository")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBrowseRuleSourceRepositoryTree(w http.ResponseWriter, r *http.Request) {
	repo, err := s.store.GetRuleSourceRepository(r.PathValue("id"))
	if err != nil {
		s.writeRuleRepositoryError(w, err, "rule_source_repository_tree_failed", "rule_source_repository")
		return
	}
	coreID := repository.Core(strings.TrimSpace(r.URL.Query().Get("core")))
	if coreID == "" {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_rule_source_repository_core", "core query parameter is required")
		return
	}
	tree, err := s.browser.Browse(repo, coreID, r.URL.Query().Get("path"))
	if err != nil {
		s.writeRuleRepositoryError(w, err, "rule_source_repository_tree_failed", "rule_source_repository")
		return
	}
	httpjson.Write(w, http.StatusOK, tree)
}

func (s *Server) handleGetRuleSourceRepositoryIndex(w http.ResponseWriter, r *http.Request) {
	offset := parseOptionalPositiveInt(r.URL.Query().Get("offset"), 0)
	limit := parseOptionalPositiveInt(r.URL.Query().Get("limit"), 500)
	index, err := s.store.GetRuleSourceIndexPage(r.PathValue("id"), r.URL.Query().Get("path"), offset, limit)
	if err != nil {
		s.writeRuleRepositoryError(w, err, "rule_source_repository_index_failed", "rule_source_repository")
		return
	}
	httpjson.Write(w, http.StatusOK, index)
}

func parseOptionalPositiveInt(value string, fallback int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func parseOptionalTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}

func (s *Server) handleSearchRuleSourceRepositoryIndex(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := parseOptionalPositiveInt(query.Get("limit"), 100)
	filters := repository.RuleSourceIndexSearchFilters{
		Offset:     parseOptionalPositiveInt(query.Get("offset"), 0),
		Core:       repository.Core(strings.TrimSpace(query.Get("core"))),
		Format:     strings.TrimSpace(query.Get("format")),
		Behavior:   strings.TrimSpace(query.Get("behavior")),
		Kind:       repository.ResourceKind(strings.TrimSpace(query.Get("kind"))),
		PathPrefix: strings.TrimSpace(query.Get("pathPrefix")),
	}
	index, err := s.store.SearchRuleSourceIndex(r.PathValue("id"), query.Get("q"), limit, filters)
	if err != nil {
		s.writeRuleRepositoryError(w, err, "rule_source_repository_index_search_failed", "rule_source_repository")
		return
	}
	httpjson.Write(w, http.StatusOK, index)
}

func (s *Server) handleRefreshRuleSourceRepositoryIndex(w http.ResponseWriter, r *http.Request) {
	repo, err := s.store.GetRuleSourceRepository(r.PathValue("id"))
	if err != nil {
		s.writeRuleRepositoryError(w, err, "rule_source_repository_index_refresh_failed", "rule_source_repository")
		return
	}
	index, err := s.browser.RefreshIndex(repo)
	if err != nil {
		s.recordOperationEvent(repository.OperationEvent{
			Severity:     "error",
			EventType:    "repository.refresh.failed",
			ResourceType: string(repository.KindRuleSourceRepo),
			ResourceID:   repo.ID,
			Message:      "Rule source repository index refresh failed",
			ErrorCode:    "rule_source_repository_index_refresh_failed",
			Context:      map[string]any{"error": err.Error()},
		})
		s.writeRuleRepositoryError(w, err, "rule_source_repository_index_refresh_failed", "rule_source_repository")
		return
	}
	index, err = s.store.UpsertRuleSourceIndex(index)
	if err != nil {
		s.recordOperationEvent(repository.OperationEvent{
			Severity:     "error",
			EventType:    "repository.refresh.failed",
			ResourceType: string(repository.KindRuleSourceRepo),
			ResourceID:   repo.ID,
			Message:      "Rule source repository index persistence failed",
			ErrorCode:    "rule_source_repository_index_refresh_failed",
			Context:      map[string]any{"error": err.Error()},
		})
		s.writeRuleRepositoryError(w, err, "rule_source_repository_index_refresh_failed", "rule_source_repository")
		return
	}
	s.recordOperationEvent(repository.OperationEvent{
		Severity:     "info",
		EventType:    "repository.refresh.succeeded",
		ResourceType: string(repository.KindRuleSourceRepo),
		ResourceID:   repo.ID,
		Message:      "Rule source repository index refreshed",
		Context:      map[string]any{"entries": len(index.Entries)},
	})
	httpjson.Write(w, http.StatusOK, index)
}

func (s *Server) handleRefreshRuleSourceSelectableFiles(w http.ResponseWriter, r *http.Request) {
	repo, err := s.store.GetRuleSourceRepository(r.PathValue("id"))
	if err != nil {
		s.writeRuleRepositoryError(w, err, "rule_source_repository_selectable_files_refresh_failed", "rule_source_repository")
		return
	}
	coreID := repository.Core(strings.TrimSpace(r.URL.Query().Get("core")))
	if coreID == "" {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_rule_source_repository_core", "core query parameter is required")
		return
	}
	files, err := s.browser.RefreshSelectableFiles(repo, coreID)
	if err != nil {
		s.recordOperationEvent(repository.OperationEvent{
			Severity:     "error",
			EventType:    "repository.refresh.failed",
			ResourceType: string(repository.KindRuleSourceRepo),
			ResourceID:   repo.ID,
			Core:         coreID,
			Message:      "Rule source selectable files refresh failed",
			ErrorCode:    "rule_source_repository_selectable_files_refresh_failed",
			Context:      map[string]any{"error": err.Error()},
		})
		s.writeRuleRepositoryError(w, err, "rule_source_repository_selectable_files_refresh_failed", "rule_source_repository")
		return
	}
	files, err = s.store.UpsertRuleSourceSelectableFiles(files)
	if err != nil {
		s.recordOperationEvent(repository.OperationEvent{
			Severity:     "error",
			EventType:    "repository.refresh.failed",
			ResourceType: string(repository.KindRuleSourceRepo),
			ResourceID:   repo.ID,
			Core:         coreID,
			Message:      "Rule source selectable files persistence failed",
			ErrorCode:    "rule_source_repository_selectable_files_refresh_failed",
			Context:      map[string]any{"error": err.Error()},
		})
		s.writeRuleRepositoryError(w, err, "rule_source_repository_selectable_files_refresh_failed", "rule_source_repository")
		return
	}
	s.recordOperationEvent(repository.OperationEvent{
		Severity:     "info",
		EventType:    "repository.refresh.succeeded",
		ResourceType: string(repository.KindRuleSourceRepo),
		ResourceID:   repo.ID,
		Core:         coreID,
		Message:      "Rule source selectable files refreshed",
		Context:      map[string]any{"files": len(files.Files)},
	})
	httpjson.Write(w, http.StatusOK, files)
}

func (s *Server) handleListSingBoxRuleSets(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListSingBoxRuleSets()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "sing_box_rule_sets_read_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, items)
}

func (s *Server) handleGetSingBoxRuleSet(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetSingBoxRuleSet(r.PathValue("id"))
	s.writeRepositoryResponse(w, item, err, "sing_box_rule_set")
}

func (s *Server) handleCreateSingBoxRuleSet(w http.ResponseWriter, r *http.Request) {
	var input repository.SingBoxRuleSetResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_sing_box_rule_set", "request body must be valid JSON")
		return
	}
	item, err := s.store.CreateSingBoxRuleSet(input)
	if err != nil {
		s.writeRuleRepositoryError(w, err, "sing_box_rule_set_create_failed", "sing_box_rule_set")
		return
	}
	httpjson.Write(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateSingBoxRuleSet(w http.ResponseWriter, r *http.Request) {
	var input repository.SingBoxRuleSetResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_sing_box_rule_set", "request body must be valid JSON")
		return
	}
	item, err := s.store.UpdateSingBoxRuleSet(r.PathValue("id"), input)
	if err != nil {
		s.writeRuleRepositoryError(w, err, "sing_box_rule_set_update_failed", "sing_box_rule_set")
		return
	}
	httpjson.Write(w, http.StatusOK, item)
}

func (s *Server) handleDeleteSingBoxRuleSet(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSingBoxRuleSet(r.PathValue("id")); err != nil {
		s.writeRuleRepositoryError(w, err, "sing_box_rule_set_delete_failed", "sing_box_rule_set")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListMihomoRuleProviders(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListMihomoRuleProviders()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "mihomo_rule_providers_read_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, items)
}

func (s *Server) handleGetMihomoRuleProvider(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetMihomoRuleProvider(r.PathValue("id"))
	s.writeRepositoryResponse(w, item, err, "mihomo_rule_provider")
}

func (s *Server) handleCreateMihomoRuleProvider(w http.ResponseWriter, r *http.Request) {
	var input repository.MihomoRuleProviderResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_mihomo_rule_provider", "request body must be valid JSON")
		return
	}
	item, err := s.store.CreateMihomoRuleProvider(input)
	if err != nil {
		s.writeRuleRepositoryError(w, err, "mihomo_rule_provider_create_failed", "mihomo_rule_provider")
		return
	}
	httpjson.Write(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateMihomoRuleProvider(w http.ResponseWriter, r *http.Request) {
	var input repository.MihomoRuleProviderResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_mihomo_rule_provider", "request body must be valid JSON")
		return
	}
	item, err := s.store.UpdateMihomoRuleProvider(r.PathValue("id"), input)
	if err != nil {
		s.writeRuleRepositoryError(w, err, "mihomo_rule_provider_update_failed", "mihomo_rule_provider")
		return
	}
	httpjson.Write(w, http.StatusOK, item)
}

func (s *Server) handleDeleteMihomoRuleProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteMihomoRuleProvider(r.PathValue("id")); err != nil {
		s.writeRuleRepositoryError(w, err, "mihomo_rule_provider_delete_failed", "mihomo_rule_provider")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListGroupSets(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListGroupSets()
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "group_sets_read_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, items)
}

func (s *Server) handleGetGroupSet(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetGroupSet(r.PathValue("id"))
	s.writeRepositoryResponse(w, item, err, "group_set")
}

func (s *Server) handleCreateGroupSet(w http.ResponseWriter, r *http.Request) {
	var input repository.GroupSetResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_group_set", "request body must be valid JSON")
		return
	}
	item, err := s.store.CreateGroupSet(input)
	if err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, "group_set_create_failed", err.Error())
		return
	}
	httpjson.Write(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateGroupSet(w http.ResponseWriter, r *http.Request) {
	var input repository.GroupSetResource
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_group_set", "request body must be valid JSON")
		return
	}
	item, err := s.store.UpdateGroupSet(r.PathValue("id"), input)
	s.writeRepositoryMutation(w, item, err, "group_set_update_failed", "group_set")
}

func (s *Server) handleDeleteGroupSet(w http.ResponseWriter, r *http.Request) {
	s.deleteRepositoryResource(w, s.store.DeleteGroupSet(r.PathValue("id")), "group_set_delete_failed", "group_set")
}

func (s *Server) handleImportClash(w http.ResponseWriter, r *http.Request) {
	var input importer.ClashImportInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_clash_import", "request body must be valid JSON")
		return
	}
	result, err := s.imports.ImportClash(input)
	s.writeImportResponse(w, result, err, "clash_import_failed")
}

func (s *Server) handleImportPlainNodes(w http.ResponseWriter, r *http.Request) {
	var input importer.PlainNodeImportInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_plain_node_import", "request body must be valid JSON")
		return
	}
	result, err := s.imports.ImportPlainNodes(input)
	s.writeImportResponse(w, result, err, "plain_node_import_failed")
}

func (s *Server) handleImportManualNode(w http.ResponseWriter, r *http.Request) {
	var input importer.ManualNodeImportInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_manual_node_import", "request body must be valid JSON")
		return
	}
	result, err := s.imports.CreateManualNode(input)
	s.writeImportResponse(w, result, err, "manual_node_import_failed")
}

func (s *Server) handleDeleteManualNode(w http.ResponseWriter, r *http.Request) {
	var input importer.ManualNodeDeleteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, "invalid_manual_node_delete", "request body must be valid JSON")
		return
	}
	result, err := s.imports.DeleteManualNode(input)
	s.writeImportResponse(w, result, err, "manual_node_delete_failed")
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"duration", time.Since(startedAt).String(),
		)
	})
}

func (s *Server) recordOperationEvent(event repository.OperationEvent) {
	if _, err := s.store.RecordOperationEvent(event); err != nil {
		s.logger.Warn("record operation event failed", "eventType", event.EventType, "error", err)
	}
}

func isJSONValidationError(err error) bool {
	var syntaxError *json.SyntaxError
	var typeError *json.UnmarshalTypeError
	return errors.As(err, &syntaxError) || errors.As(err, &typeError)
}

func (s *Server) writeRepositoryResponse(w http.ResponseWriter, item any, err error, resourceName string) {
	if err == nil {
		httpjson.Write(w, http.StatusOK, item)
		return
	}
	status := http.StatusInternalServerError
	code := resourceName + "_read_failed"
	if errors.Is(err, repository.ErrNotFound) {
		status = http.StatusNotFound
		code = resourceName + "_not_found"
	}
	httpjson.WriteError(w, status, code, err.Error())
}

func (s *Server) writeRepositoryMutation(w http.ResponseWriter, item any, err error, defaultCode string, resourceName string) {
	if err == nil {
		httpjson.Write(w, http.StatusOK, item)
		return
	}
	status := http.StatusInternalServerError
	code := defaultCode
	if errors.Is(err, repository.ErrNotFound) {
		status = http.StatusNotFound
		code = resourceName + "_not_found"
	} else if errors.Is(err, repository.ErrDuplicateNodeName) {
		status = http.StatusConflict
		code = resourceName + "_duplicate_node_name"
	}
	httpjson.WriteError(w, status, code, err.Error())
}

func (s *Server) deleteRepositoryResource(w http.ResponseWriter, err error, defaultCode string, resourceName string) {
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	status := http.StatusInternalServerError
	code := defaultCode
	if errors.Is(err, repository.ErrNotFound) {
		status = http.StatusNotFound
		code = resourceName + "_not_found"
	}
	httpjson.WriteError(w, status, code, err.Error())
}

func (s *Server) writeRuleRepositoryError(w http.ResponseWriter, err error, defaultCode string, resourceName string) {
	status := http.StatusInternalServerError
	code := defaultCode
	switch {
	case errors.Is(err, repository.ErrNotFound):
		status = http.StatusNotFound
		code = resourceName + "_not_found"
	case errors.Is(err, repository.ErrInvalidRuleSourceRepository),
		errors.Is(err, repository.ErrInvalidSingBoxRuleSet),
		errors.Is(err, repository.ErrInvalidMihomoRuleProvider):
		status = http.StatusBadRequest
		code = "invalid_" + resourceName
	case errors.Is(err, repository.ErrUnsupportedRepositoryCore), errors.Is(err, repository.ErrRuleSourceTreeLookup):
		status = http.StatusBadRequest
		code = resourceName + "_invalid_core"
	case errors.Is(err, repository.ErrBuiltInRepositoryReadOnly):
		status = http.StatusConflict
		code = resourceName + "_read_only"
	}
	httpjson.WriteError(w, status, code, err.Error())
}

func (s *Server) writeImportResponse(w http.ResponseWriter, result importer.Result, err error, defaultCode string) {
	if err == nil {
		event := repository.OperationEvent{
			Severity:  "info",
			EventType: "import.succeeded",
			Message:   "Repository import succeeded",
			Context: map[string]any{
				"warnings": len(result.Diagnostics.Warnings),
			},
		}
		if result.Subscription != nil {
			event.ResourceType = string(repository.KindSubscription)
			event.ResourceID = result.Subscription.ID
		} else if result.NodeSet != nil {
			event.ResourceType = string(repository.KindNodeSet)
			event.ResourceID = result.NodeSet.ID
		}
		s.recordOperationEvent(event)
		httpjson.Write(w, http.StatusCreated, result)
		return
	}
	status := http.StatusInternalServerError
	if errors.Is(err, importer.ErrInvalidImport) {
		status = http.StatusBadRequest
	} else if errors.Is(err, repository.ErrDuplicateNodeName) {
		status = http.StatusConflict
	}
	s.recordOperationEvent(repository.OperationEvent{
		Severity:  "error",
		EventType: "import.failed",
		Message:   "Repository import failed",
		ErrorCode: defaultCode,
		Context:   map[string]any{"error": err.Error()},
	})
	httpjson.WriteError(w, status, defaultCode, err.Error())
}
