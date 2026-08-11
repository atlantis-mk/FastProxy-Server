package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type FirewallType string

const (
	FirewallTypeNFTables FirewallType = "nftables"
	FirewallTypeIPTables FirewallType = "iptables"
)

type commandRunner interface {
	Run(ctx context.Context, name string, args []string, input []byte) ([]byte, error)
}

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, name string, args []string, input []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	return cmd.CombinedOutput()
}

type FirewallSnapshot struct {
	Types          []FirewallType
	nftRuleset     []byte
	iptablesRules  []byte
	ip6tablesRules []byte
	runner         commandRunner
}

func CaptureFirewallSnapshot(ctx context.Context) (*FirewallSnapshot, error) {
	return captureFirewallSnapshot(ctx, osCommandRunner{})
}

func captureFirewallSnapshot(ctx context.Context, runner commandRunner) (*FirewallSnapshot, error) {
	snapshot := &FirewallSnapshot{runner: runner}
	if ruleset, err := runFirewallCommand(ctx, runner, "nft", []string{"list", "ruleset"}, nil); err == nil {
		snapshot.Types = append(snapshot.Types, FirewallTypeNFTables)
		snapshot.nftRuleset = ruleset
	}
	if rules, err := runFirewallCommand(ctx, runner, "iptables-save", nil, nil); err == nil {
		snapshot.Types = append(snapshot.Types, FirewallTypeIPTables)
		snapshot.iptablesRules = rules
	}
	if rules, err := runFirewallCommand(ctx, runner, "ip6tables-save", nil, nil); err == nil {
		snapshot.ip6tablesRules = rules
		if !containsFirewallType(snapshot.Types, FirewallTypeIPTables) {
			snapshot.Types = append(snapshot.Types, FirewallTypeIPTables)
		}
	}
	return snapshot, nil
}

func (s *FirewallSnapshot) Restore(ctx context.Context) error {
	if s == nil {
		return CleanupRuntimeNetworkState(ctx)
	}
	runner := s.runner
	if runner == nil {
		runner = osCommandRunner{}
	}

	var restoreErr error
	if len(s.nftRuleset) > 0 {
		input := append([]byte("flush ruleset\n"), s.nftRuleset...)
		if _, err := runFirewallCommand(ctx, runner, "nft", []string{"-f", "-"}, input); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore nftables ruleset: %w", err))
		}
	}
	if len(s.iptablesRules) > 0 {
		if _, err := runFirewallCommand(ctx, runner, "iptables-restore", nil, s.iptablesRules); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore iptables ruleset: %w", err))
		}
	}
	if len(s.ip6tablesRules) > 0 {
		if _, err := runFirewallCommand(ctx, runner, "ip6tables-restore", nil, s.ip6tablesRules); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore ip6tables ruleset: %w", err))
		}
	}
	if err := cleanupRuntimeNetworkState(ctx, runner); err != nil {
		restoreErr = errors.Join(restoreErr, err)
	}
	return restoreErr
}

func CleanupRuntimeNetworkState(ctx context.Context) error {
	return cleanupRuntimeNetworkState(ctx, osCommandRunner{})
}

func cleanupRuntimeNetworkState(ctx context.Context, runner commandRunner) error {
	var cleanupErr error
	for _, table := range []string{"mihomo", "sing-box"} {
		_, _ = runFirewallCommand(ctx, runner, "nft", []string{"delete", "table", "inet", table}, nil)
	}
	if err := deleteRuntimeFW4Rules(ctx, runner); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	for _, pref := range []string{"1", "9000", "9001", "9002", "9010", "32768"} {
		for range 8 {
			if _, err := runFirewallCommand(ctx, runner, "ip", []string{"rule", "del", "pref", pref}, nil); err != nil {
				break
			}
		}
	}
	_, _ = runFirewallCommand(ctx, runner, "ip", []string{"route", "flush", "table", "2022"}, nil)
	for _, name := range []string{"utun", "utun100", "utun1010", "Meta"} {
		_, _ = runFirewallCommand(ctx, runner, "ip", []string{"link", "del", name}, nil)
	}
	_, _ = runFirewallCommand(ctx, runner, "/etc/init.d/dnsmasq", []string{"restart"}, nil)
	return cleanupErr
}

func deleteRuntimeFW4Rules(ctx context.Context, runner commandRunner) error {
	var cleanupErr error
	for _, chain := range []string{"input", "forward"} {
		output, err := runFirewallCommand(ctx, runner, "nft", []string{"-a", "list", "chain", "inet", "fw4", chain}, nil)
		if err != nil {
			continue
		}
		for _, handle := range runtimeRuleHandles(string(output)) {
			if _, err := runFirewallCommand(ctx, runner, "nft", []string{"delete", "rule", "inet", "fw4", chain, "handle", handle}, nil); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete fw4 %s rule handle %s: %w", chain, handle, err))
			}
		}
	}
	return cleanupErr
}

func runtimeRuleHandles(rules string) []string {
	handlePattern := regexp.MustCompile(`# handle ([0-9]+)`)
	var handles []string
	for _, line := range strings.Split(rules, "\n") {
		if !strings.Contains(line, "mihomo") && !strings.Contains(line, "sing-box") {
			continue
		}
		match := handlePattern.FindStringSubmatch(line)
		if len(match) == 2 {
			handles = append(handles, match[1])
		}
	}
	return handles
}

func runFirewallCommand(ctx context.Context, runner commandRunner, name string, args []string, input []byte) ([]byte, error) {
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := runner.Run(deadline, name, args, input)
	if err != nil {
		if len(output) == 0 {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func containsFirewallType(types []FirewallType, target FirewallType) bool {
	for _, firewallType := range types {
		if firewallType == target {
			return true
		}
	}
	return false
}
