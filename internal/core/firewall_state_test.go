package core

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type firewallCommandCall struct {
	Name  string
	Args  []string
	Input string
}

type fakeFirewallRunner struct {
	outputs map[string][]byte
	calls   []firewallCommandCall
}

func (r *fakeFirewallRunner) Run(ctx context.Context, name string, args []string, input []byte) ([]byte, error) {
	r.calls = append(r.calls, firewallCommandCall{
		Name:  name,
		Args:  append([]string(nil), args...),
		Input: string(input),
	})
	if output, ok := r.outputs[commandKey(name, args)]; ok {
		return output, nil
	}
	return nil, errors.New("not found")
}

func TestCaptureFirewallSnapshotDetectsAvailableFirewallTypes(t *testing.T) {
	runner := &fakeFirewallRunner{outputs: map[string][]byte{
		commandKey("nft", []string{"list", "ruleset"}): []byte("table inet fw4 {}\n"),
		commandKey("iptables-save", nil):               []byte("*filter\nCOMMIT\n"),
	}}

	snapshot, err := captureFirewallSnapshot(context.Background(), runner)
	if err != nil {
		t.Fatalf("captureFirewallSnapshot() error = %v", err)
	}
	if !reflect.DeepEqual(snapshot.Types, []FirewallType{FirewallTypeNFTables, FirewallTypeIPTables}) {
		t.Fatalf("Types = %#v, want nftables and iptables", snapshot.Types)
	}
	if string(snapshot.nftRuleset) != "table inet fw4 {}\n" {
		t.Fatalf("nftRuleset = %q", snapshot.nftRuleset)
	}
	if string(snapshot.iptablesRules) != "*filter\nCOMMIT\n" {
		t.Fatalf("iptablesRules = %q", snapshot.iptablesRules)
	}
}

func TestFirewallSnapshotRestoreFlushesAndReplaysCapturedRulesets(t *testing.T) {
	runner := &fakeFirewallRunner{outputs: map[string][]byte{
		commandKey("nft", []string{"-f", "-"}):                                       nil,
		commandKey("iptables-restore", nil):                                          nil,
		commandKey("nft", []string{"-a", "list", "chain", "inet", "fw4", "input"}):   nil,
		commandKey("nft", []string{"-a", "list", "chain", "inet", "fw4", "forward"}): nil,
	}}
	snapshot := &FirewallSnapshot{
		nftRuleset:    []byte("table inet fw4 {}\n"),
		iptablesRules: []byte("*filter\nCOMMIT\n"),
		runner:        runner,
	}

	if err := snapshot.Restore(context.Background()); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	nftRestore := findCall(runner.calls, "nft", []string{"-f", "-"})
	if nftRestore == nil {
		t.Fatal("Restore() did not run nft restore")
	}
	if nftRestore.Input != "flush ruleset\ntable inet fw4 {}\n" {
		t.Fatalf("nft restore input = %q", nftRestore.Input)
	}
	iptablesRestore := findCall(runner.calls, "iptables-restore", nil)
	if iptablesRestore == nil || iptablesRestore.Input != "*filter\nCOMMIT\n" {
		t.Fatalf("iptables restore call = %#v", iptablesRestore)
	}
}

func TestCleanupRuntimeNetworkStateRemovesMihomoArtifacts(t *testing.T) {
	runner := &fakeFirewallRunner{outputs: map[string][]byte{
		commandKey("nft", []string{"delete", "table", "inet", "mihomo"}): nil,
		commandKey("nft", []string{"-a", "list", "chain", "inet", "fw4", "input"}): []byte(
			`iifname "utun100" counter accept comment "!mihomo: Accept traffic from tun" # handle 41` + "\n",
		),
		commandKey("nft", []string{"-a", "list", "chain", "inet", "fw4", "forward"}): []byte(
			`oifname "utun100" counter accept comment "!mihomo: Accept traffic from tun" # handle 42` + "\n",
		),
		commandKey("nft", []string{"delete", "rule", "inet", "fw4", "input", "handle", "41"}):   nil,
		commandKey("nft", []string{"delete", "rule", "inet", "fw4", "forward", "handle", "42"}): nil,
		commandKey("ip", []string{"route", "flush", "table", "2022"}):                           nil,
		commandKey("ip", []string{"link", "del", "utun"}):                                       nil,
		commandKey("ip", []string{"link", "del", "utun100"}):                                    nil,
		commandKey("ip", []string{"link", "del", "utun1010"}):                                   nil,
		commandKey("ip", []string{"link", "del", "Meta"}):                                       nil,
		commandKey("/etc/init.d/dnsmasq", []string{"restart"}):                                  nil,
	}}
	for _, pref := range []string{"9000", "9001", "9002", "9010"} {
		runner.outputs[commandKey("ip", []string{"rule", "del", "pref", pref})] = nil
	}

	if err := cleanupRuntimeNetworkState(context.Background(), runner); err != nil {
		t.Fatalf("cleanupRuntimeNetworkState() error = %v", err)
	}

	for _, expected := range []firewallCommandCall{
		{Name: "nft", Args: []string{"delete", "table", "inet", "mihomo"}},
		{Name: "nft", Args: []string{"delete", "rule", "inet", "fw4", "input", "handle", "41"}},
		{Name: "nft", Args: []string{"delete", "rule", "inet", "fw4", "forward", "handle", "42"}},
		{Name: "ip", Args: []string{"route", "flush", "table", "2022"}},
		{Name: "ip", Args: []string{"link", "del", "utun100"}},
		{Name: "/etc/init.d/dnsmasq", Args: []string{"restart"}},
	} {
		if findCall(runner.calls, expected.Name, expected.Args) == nil {
			t.Fatalf("missing cleanup call %s %v in %#v", expected.Name, expected.Args, runner.calls)
		}
	}
}

func commandKey(name string, args []string) string {
	return name + " " + strings.Join(args, "\x00")
}

func findCall(calls []firewallCommandCall, name string, args []string) *firewallCommandCall {
	for i := range calls {
		if calls[i].Name == name && reflect.DeepEqual(calls[i].Args, args) {
			return &calls[i]
		}
	}
	return nil
}
