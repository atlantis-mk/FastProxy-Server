package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

func TestHandleRuntimeControllerProxyForwardsWithConfiguredSecret(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	var receivedPath string
	var receivedQuery string
	var receivedAuthorization string
	var receivedBody string
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		receivedAuthorization = r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll() error = %v", err)
		}
		receivedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer controller.Close()

	config, err := store.GlobalConfig()
	if err != nil {
		t.Fatalf("GlobalConfig() error = %v", err)
	}
	config.Fields["externalController"] = controller.Listener.Addr().String()
	config.Fields["secret"] = "runtime-secret"
	if _, err := store.UpdateGlobalConfig(config); err != nil {
		t.Fatalf("UpdateGlobalConfig() error = %v", err)
	}

	server := &Server{store: store, logger: slog.Default()}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/runtime/controller/providers/proxies/main?force=true",
		bytes.NewBufferString(`{"refresh":true}`),
	)
	request.Header.Set("Authorization", "Bearer frontend-secret")
	response := httptest.NewRecorder()

	server.handleRuntimeControllerProxy(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, body = %s", response.Code, response.Body.String())
	}
	if receivedPath != "/providers/proxies/main" {
		t.Fatalf("received path = %q, want /providers/proxies/main", receivedPath)
	}
	if receivedQuery != "force=true" {
		t.Fatalf("received query = %q, want force=true", receivedQuery)
	}
	if receivedAuthorization != "Bearer runtime-secret" {
		t.Fatalf("received authorization = %q, want runtime secret", receivedAuthorization)
	}
	if receivedBody != `{"refresh":true}` {
		t.Fatalf("received body = %q, want request body", receivedBody)
	}
}

func TestHandleCheckNodeHealthRecordsSuccessAndFailure(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("Atoi(port) error = %v", err)
	}
	if err := store.UpsertNodeCache(repository.NodeCacheUpsert{
		SourceType: repository.NodeCacheSourceImport,
		SourceID:   "nodes",
		NodeSetID:  "nodes",
		Nodes: []repository.NormalizedNode{{
			Tag:        "reachable",
			Type:       "direct",
			Server:     "127.0.0.1",
			ServerPort: port,
		}, {
			Tag:  "broken",
			Type: "direct",
		}},
	}); err != nil {
		t.Fatalf("UpsertNodeCache() error = %v", err)
	}
	server := &Server{store: store, logger: slog.Default()}

	success := checkNodeHealthForTest(t, server, "reachable")
	if !success.Success || success.LatencyMS < 0 || success.NodeID == "" {
		t.Fatalf("success sample = %#v, want successful sample", success)
	}
	failure := checkNodeHealthForTest(t, server, "broken")
	if failure.Success || failure.ErrorSummary == "" || failure.NodeID == "" {
		t.Fatalf("failure sample = %#v, want failed sample with error summary", failure)
	}
}

func checkNodeHealthForTest(t *testing.T, server *Server, tag string) repository.HealthCheckSample {
	t.Helper()
	body, err := json.Marshal(nodeHealthCheckRequest{Tag: tag, TimeoutMS: 200})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/repository/nodes/health-check", bytes.NewReader(body))
	response := httptest.NewRecorder()
	server.handleCheckNodeHealth(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, body = %s", response.Code, response.Body.String())
	}
	var sample repository.HealthCheckSample
	if err := json.NewDecoder(response.Body).Decode(&sample); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	return sample
}

func TestUpdateGlobalInbounds(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	server := &Server{store: store, logger: slog.Default()}
	body := bytes.NewBufferString(`{"inbounds":[{"id":" mixed ","enabled":true,"tag":" mixed-in ","kind":"mixed","listen":{"address":" 0.0.0.0 ","port":7890}},{"id":"","enabled":true,"tag":"","kind":"tun","tun":{"interfaceName":" tun0 ","autoRoute":true}}]}`)
	request := httptest.NewRequest(http.MethodPut, "/api/repository/config/inbounds", body)
	response := httptest.NewRecorder()

	server.handleUpdateGlobalInbounds(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, body = %s", response.Code, response.Body.String())
	}
	var updated repository.GlobalConfig
	if err := json.NewDecoder(response.Body).Decode(&updated); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(updated.Inbounds) != 2 {
		t.Fatalf("len(inbounds) = %d, want 2", len(updated.Inbounds))
	}
	if updated.Inbounds[0].ID != "mixed" || updated.Inbounds[0].Listen.Address != "0.0.0.0" {
		t.Fatalf("first inbound was not normalized: %#v", updated.Inbounds[0])
	}
	if updated.Inbounds[1].ID == "" || updated.Inbounds[1].Tag != "tun-in" || updated.Inbounds[1].Tun.InterfaceName != "tun0" {
		t.Fatalf("second inbound defaults were not applied: %#v", updated.Inbounds[1])
	}

	inbounds, err := store.GlobalInbounds()
	if err != nil {
		t.Fatalf("GlobalInbounds() error = %v", err)
	}
	if len(inbounds) != 2 || inbounds[0].Listen.Port != 7890 {
		t.Fatalf("persisted inbounds = %#v, want saved managed inbounds", inbounds)
	}
}

func TestUpdateGlobalConfigPersistsAllConfigSections(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	server := &Server{store: store, logger: slog.Default()}
	body := bytes.NewBufferString(`{
		"fields":{"allowLan":true,"bindAddress":"*","networkStrategy":"fallback"},
		"dnsServers":[{"id":" local ","name":" local ","role":" default ","protocol":" udp ","address":" 223.5.5.5 ","port":" 53 "}],
		"dnsRules":[{"id":" cn ","matcher":" geosite ","value":" cn ","serverName":" default-1 "}],
		"inbounds":[{"id":"mixed","enabled":true,"tag":"mixed-in","kind":"mixed","listen":{"address":"0.0.0.0","port":7890}}]
	}`)
	request := httptest.NewRequest(http.MethodPut, "/api/repository/config", body)
	response := httptest.NewRecorder()

	server.handleUpdateGlobalConfig(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, body = %s", response.Code, response.Body.String())
	}
	var updated repository.GlobalConfig
	if err := json.NewDecoder(response.Body).Decode(&updated); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if updated.Fields["networkStrategy"] != "fallback" {
		t.Fatalf("fields = %#v, want saved network config", updated.Fields)
	}
	if len(updated.DNSServers) != 1 || updated.DNSServers[0].Name != "" || updated.DNSServers[0].Address != "223.5.5.5" {
		t.Fatalf("dnsServers = %#v, want normalized server", updated.DNSServers)
	}
	if len(updated.DNSRules) != 1 || updated.DNSRules[0].Matcher != "geosite" || updated.DNSRules[0].ServerName != "default-1" {
		t.Fatalf("dnsRules = %#v, want normalized rule", updated.DNSRules)
	}
	if len(updated.Inbounds) != 1 || updated.Inbounds[0].Tag != "mixed-in" {
		t.Fatalf("inbounds = %#v, want saved inbound", updated.Inbounds)
	}

	readRequest := httptest.NewRequest(http.MethodGet, "/api/repository/config", nil)
	readResponse := httptest.NewRecorder()
	server.handleGetGlobalConfig(readResponse, readRequest)
	if readResponse.Code != http.StatusOK {
		t.Fatalf("read response status = %d, body = %s", readResponse.Code, readResponse.Body.String())
	}
	var persisted repository.GlobalConfig
	if err := json.NewDecoder(readResponse.Body).Decode(&persisted); err != nil {
		t.Fatalf("Decode(read) error = %v", err)
	}
	if persisted.Fields["bindAddress"] != "*" || len(persisted.DNSServers) != 1 || len(persisted.DNSRules) != 1 || len(persisted.Inbounds) != 1 {
		t.Fatalf("persisted config = %#v, want all sections", persisted)
	}
}

func TestGetGlobalInboundsReturnsDefaultInbounds(t *testing.T) {
	store, err := repository.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	server := &Server{store: store, logger: slog.Default()}
	request := httptest.NewRequest(http.MethodGet, "/api/repository/config/inbounds", nil)
	response := httptest.NewRecorder()

	server.handleGetGlobalInbounds(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, body = %s", response.Code, response.Body.String())
	}
	var inbounds []repository.ManagedInbound
	if err := json.NewDecoder(response.Body).Decode(&inbounds); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(inbounds) != 6 {
		t.Fatalf("len(inbounds) = %d, want default inbounds", len(inbounds))
	}
	if inbounds[0].Kind != "http" || inbounds[0].Listen.Port != 7890 {
		t.Fatalf("inbounds[0] = %#v, want default HTTP inbound", inbounds[0])
	}
	if inbounds[5].Kind != "tun" || len(inbounds[5].Tun.DNSHijack) != 1 {
		t.Fatalf("inbounds[5] = %#v, want default Tun inbound", inbounds[5])
	}
}
