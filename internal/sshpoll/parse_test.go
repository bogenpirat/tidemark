package sshpoll

import (
	"net/netip"
	"strings"
	"testing"
)

func mustPrefix(t *testing.T, cidr string) netip.Prefix {
	t.Helper()
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		t.Fatalf("parsing prefix %q: %v", cidr, err)
	}
	return prefix
}

func TestBuildPollCommand(t *testing.T) {
	plain := buildPollCommand("eth0", false)
	want := "cat /sys/class/net/eth0/statistics/rx_bytes /sys/class/net/eth0/statistics/tx_bytes"
	if plain != want {
		t.Errorf("plain command = %q, want %q", plain, want)
	}

	extended := buildPollCommand("pppoe-wan", true)
	if len(extended) <= len(plain) {
		t.Errorf("extended command should append the talker section, got %q", extended)
	}
	for _, fragment := range []string{"pppoe-wan", "echo ===", "awk", "/proc/net/nf_conntrack", "|| true"} {
		if !strings.Contains(extended, fragment) {
			t.Errorf("extended command missing %q: %q", fragment, extended)
		}
	}
}

func TestSplitTalkerSection(t *testing.T) {
	counterPart, talkerPart := splitTalkerSection([]byte("123\n456\n===\n192.168.1.10 999 111\n"))
	if got := string(counterPart); got != "123\n456" {
		t.Errorf("counter part = %q", got)
	}
	if talkerPart != "\n192.168.1.10 999 111\n" {
		t.Errorf("talker part = %q", talkerPart)
	}

	// No separator (feature disabled): everything is the counter part.
	counterPart, talkerPart = splitTalkerSection([]byte("123\n456\n"))
	if got := string(counterPart); got != "123\n456\n" {
		t.Errorf("counter part without separator = %q", got)
	}
	if talkerPart != "" {
		t.Errorf("talker part without separator = %q", talkerPart)
	}

	// Separator present but conntrack unavailable: empty talker section.
	counterPart, talkerPart = splitTalkerSection([]byte("123\n456\n===\n"))
	if got := string(counterPart); got != "123\n456" {
		t.Errorf("counter part with empty talkers = %q", got)
	}
	if fields := parseTalkerTotals(talkerPart, mustPrefix(t, "192.168.1.0/24")); len(fields) != 0 {
		t.Errorf("expected no talkers, got %v", fields)
	}
}

func TestParseTalkerTotals(t *testing.T) {
	lanPrefix := mustPrefix(t, "192.168.1.0/24")
	section := "\n" +
		"192.168.1.10 12345 200\n" + // in subnet
		"192.168.1.20 999 88\n" + // in subnet
		"192.168.2.30 500 1\n" + // outside subnet
		"8.8.8.8 100 1\n" + // outside subnet (WAN peer)
		"2003:e5:1234::1 700 1\n" + // IPv6, not in v4 prefix
		"not-an-ip 100 1\n" + // malformed address
		"192.168.1.40 notanumber 1\n" + // malformed upload total
		"192.168.1.41 1 notanumber\n" + // malformed download total
		"192.168.1.50 100\n" + // missing download column
		"\n"
	totals := parseTalkerTotals(section, lanPrefix)
	if len(totals) != 2 {
		t.Fatalf("expected 2 talkers, got %v", totals)
	}
	if got := totals["192.168.1.10"]; got.uploadBytes != 12345 || got.downloadBytes != 200 {
		t.Errorf("talker .10 = %+v, want up=12345 down=200", got)
	}
	if got := totals["192.168.1.20"]; got.uploadBytes != 999 || got.downloadBytes != 88 {
		t.Errorf("talker .20 = %+v, want up=999 down=88", got)
	}
}

func TestParseTalkerTotalsIPv6Subnet(t *testing.T) {
	totals := parseTalkerTotals("2003:e5:1234::1 700 30\n192.168.1.10 100 1\n", mustPrefix(t, "2003:e5:1234::/48"))
	if len(totals) != 1 || totals["2003:e5:1234::1"].uploadBytes != 700 || totals["2003:e5:1234::1"].downloadBytes != 30 {
		t.Errorf("IPv6 subnet totals = %v", totals)
	}
}

func TestPickTopTalkers(t *testing.T) {
	previous := map[string]talkerTotals{
		"192.168.1.10": {uploadBytes: 1000, downloadBytes: 50000},
		"192.168.1.20": {uploadBytes: 5000, downloadBytes: 1000},
		"192.168.1.30": {uploadBytes: 9000, downloadBytes: 9000},
	}
	current := map[string]talkerTotals{
		"192.168.1.10": {uploadBytes: 1300, downloadBytes: 50100}, // up +300, down +100
		"192.168.1.20": {uploadBytes: 5100, downloadBytes: 1900},  // up +100, down +900
		"192.168.1.30": {uploadBytes: 8000, downloadBytes: 8000},  // shrank -> clamped to 0
	}
	winners := pickTopTalkers(previous, current)
	if winners.uploadIP != "192.168.1.10" || winners.uploadBytesPerSec != 300 {
		t.Errorf("top uploader = %s (%v), want 192.168.1.10 (300)",
			winners.uploadIP, winners.uploadBytesPerSec)
	}
	if winners.downloadIP != "192.168.1.20" || winners.downloadBytesPerSec != 900 {
		t.Errorf("top downloader = %s (%v), want 192.168.1.20 (900)",
			winners.downloadIP, winners.downloadBytesPerSec)
	}

	// A new IP contributes its full totals and can win both directions.
	current["192.168.1.40"] = talkerTotals{uploadBytes: 2000, downloadBytes: 3000}
	winners = pickTopTalkers(previous, current)
	if winners.uploadIP != "192.168.1.40" || winners.uploadBytesPerSec != 2000 {
		t.Errorf("new-IP top uploader = %s (%v), want 192.168.1.40 (2000)",
			winners.uploadIP, winners.uploadBytesPerSec)
	}
	if winners.downloadIP != "192.168.1.40" || winners.downloadBytesPerSec != 3000 {
		t.Errorf("new-IP top downloader = %s (%v), want 192.168.1.40 (3000)",
			winners.downloadIP, winners.downloadBytesPerSec)
	}

	// One direction idle: only the other gets a winner.
	winners = pickTopTalkers(
		map[string]talkerTotals{"192.168.1.10": {uploadBytes: 100, downloadBytes: 100}},
		map[string]talkerTotals{"192.168.1.10": {uploadBytes: 100, downloadBytes: 400}},
	)
	if winners.uploadIP != "" || winners.uploadBytesPerSec != 0 {
		t.Errorf("idle-upload winner = %q (%v), want empty",
			winners.uploadIP, winners.uploadBytesPerSec)
	}
	if winners.downloadIP != "192.168.1.10" || winners.downloadBytesPerSec != 300 {
		t.Errorf("download winner = %s (%v), want 192.168.1.10 (300)",
			winners.downloadIP, winners.downloadBytesPerSec)
	}

	// Nil/empty maps must not panic.
	if winners := pickTopTalkers(nil, nil); winners.downloadIP != "" || winners.uploadIP != "" {
		t.Errorf("nil maps winners = %+v, want empty", winners)
	}
}

func TestParseLeaseNames(t *testing.T) {
	leaseOutput := "1752600000 aa:bb:cc:dd:ee:01 192.168.1.10 laptop-anna 01:aa:bb:cc:dd:ee:01\n" +
		"1752600000 aa:bb:cc:dd:ee:02 192.168.1.20 * 01:aa:bb:cc:dd:ee:02\n" + // no hostname sent
		"1752600000 aa:bb:cc:dd:ee:03 not-an-ip badhost 01:aa:bb:cc:dd:ee:03\n" + // malformed ip
		"garbage line\n" + // too few fields
		"1752600000 aa:bb:cc:dd:ee:04 2003:e5:1234::4 nas *\n" + // IPv6 lease (odhcpd-style entry)
		"\n"
	leaseNames := parseLeaseNames(leaseOutput)
	if len(leaseNames) != 2 {
		t.Fatalf("expected 2 lease names, got %v", leaseNames)
	}
	if leaseNames["192.168.1.10"] != "laptop-anna" {
		t.Errorf("lease for .10 = %q, want laptop-anna", leaseNames["192.168.1.10"])
	}
	if leaseNames["2003:e5:1234::4"] != "nas" {
		t.Errorf("lease for IPv6 host = %q, want nas", leaseNames["2003:e5:1234::4"])
	}
}

func TestTalkerLabel(t *testing.T) {
	leaseNames := map[string]string{"192.168.1.10": "laptop-anna"}
	if got := talkerLabel(leaseNames, "192.168.1.10"); got != "laptop-anna" {
		t.Errorf("known ip label = %q, want laptop-anna", got)
	}
	if got := talkerLabel(leaseNames, "192.168.1.99"); got != "192.168.1.99" {
		t.Errorf("unknown ip label = %q, want the ip back", got)
	}
	if got := talkerLabel(leaseNames, ""); got != "" {
		t.Errorf("empty ip label = %q, want empty", got)
	}
	if got := talkerLabel(nil, "192.168.1.10"); got != "192.168.1.10" {
		t.Errorf("nil map label = %q, want the ip back", got)
	}
}

// TestParseCountersWithTalkerOutput ensures the existing counter parser still
// works on the first section of the combined command output.
func TestParseCountersWithTalkerOutput(t *testing.T) {
	output := []byte("123456789\n987654321\n===\n192.168.1.10 555 777\n")
	counterSection, talkerSection := splitTalkerSection(output)
	download, upload, err := parseCounters(counterSection)
	if err != nil {
		t.Fatalf("parseCounters: %v", err)
	}
	if download != 123456789 || upload != 987654321 {
		t.Errorf("counters = %d/%d", download, upload)
	}
	totals := parseTalkerTotals(talkerSection, mustPrefix(t, "192.168.1.0/24"))
	if got := totals["192.168.1.10"]; got.uploadBytes != 555 || got.downloadBytes != 777 {
		t.Errorf("talker totals = %v", totals)
	}
}
