// Package sshpoll polls a Linux host over SSH at 1-second intervals, reading
// kernel interface byte counters, and sends DataPoints to an output channel.
// It mirrors the structure and error-handling semantics of the SNMP service.
package sshpoll

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"tidemark/internal/config"
	"tidemark/internal/counter"
	"tidemark/internal/model"

	"golang.org/x/crypto/ssh"
)

// SshService polls a Linux host over SSH at 1-second intervals and sends
// DataPoints to an output channel.
type SshService struct {
	hostConfig *config.HostConfig
}

// NewService creates an SshService configured from the provided HostConfig.
func NewService(hostConfig *config.HostConfig) *SshService {
	return &SshService{hostConfig: hostConfig}
}

// Start begins the polling loop in the calling goroutine. It sends DataPoints
// to the out channel and returns when ctx is cancelled or a fatal error occurs.
// The out channel is not closed by this function; callers should not rely on it.
func (sshService *SshService) Start(ctx context.Context, out chan<- model.DataPoint) {
	sshConfig := sshService.hostConfig

	clientConfig, configError := buildClientConfig(sshConfig)
	if configError != nil {
		slog.Error("SSH key setup failed", "host", sshConfig.Host, "err", configError)
		return
	}

	address := net.JoinHostPort(sshConfig.Host, strconv.Itoa(int(sshConfig.Port)))

	// The optional top-talker feature is enabled by a valid lanSubnet CIDR.
	// LoadConfig already validated the syntax; a parse failure here just
	// disables the feature.
	var lanPrefix netip.Prefix
	talkerEnabled := false
	if sshConfig.LanSubnet != "" {
		parsedPrefix, prefixError := netip.ParsePrefix(sshConfig.LanSubnet)
		if prefixError != nil {
			slog.Warn("SSH lanSubnet invalid, top-talker disabled",
				"host", sshConfig.Host, "lanSubnet", sshConfig.LanSubnet, "err", prefixError)
		} else {
			lanPrefix = parsedPrefix
			talkerEnabled = true
		}
	}

	pollCommand := buildPollCommand(sshConfig.Interface, talkerEnabled)
	pollTimeout := time.Duration(sshConfig.TimeoutMs) * time.Millisecond

	sshClient, dialError := ssh.Dial("tcp", address, clientConfig)
	if dialError != nil {
		slog.Error("SSH connect failed", "host", sshConfig.Host, "err", dialError)
		return
	}
	defer func() {
		if sshClient != nil {
			sshClient.Close()
		}
	}()
	slog.Info("SSH session established", "host", sshConfig.Host, "port", sshConfig.Port,
		"user", sshConfig.Username,
		"interface", sshConfig.Interface)

	// Fetched once per (re)connect: devices renew their leases far less often
	// than we poll, so a per-connection snapshot is fresh enough.
	var leaseNames map[string]string
	if talkerEnabled {
		leaseNames = fetchLeaseNames(ctx, sshClient, pollTimeout, sshConfig.Host)
	}

	var previousDownloadBytes uint64
	var previousUploadBytes uint64
	var previousTalkerTotals map[string]talkerTotals
	baselineCaptured := false
	firstSuccessLogged := false

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("SSH service stopping", "host", sshConfig.Host)
			return

		case tickTime := <-ticker.C:
			// Re-dial if a previous poll failure tore down the connection. This
			// gives SSH the same "errors graph, then recovery" behavior that
			// connectionless SNMP gets for free.
			if sshClient == nil {
				reconnectedClient, redialError := ssh.Dial("tcp", address, clientConfig)
				if redialError != nil {
					slog.Warn("SSH reconnect failed", "host", sshConfig.Host, "err", redialError)
					if !baselineCaptured {
						continue
					}
					out <- model.DataPoint{
						TimestampMs:  tickTime.UnixMilli(),
						IsError:      true,
						ErrorMessage: classifySshError(redialError, sshConfig.Host),
					}
					continue
				}
				sshClient = reconnectedClient
				slog.Info("SSH session re-established", "host", sshConfig.Host)
				if talkerEnabled {
					leaseNames = fetchLeaseNames(ctx, sshClient, pollTimeout, sshConfig.Host)
				}
			}

			commandOutput, pollError := runCommandWithRetries(ctx, sshClient, pollCommand,
				pollTimeout, sshConfig.Retries)

			if pollError != nil {
				slog.Warn("SSH poll failed", "host", sshConfig.Host, "err", pollError)
				// A failed exec usually means the connection is unhealthy; tear it
				// down and re-dial on the next tick.
				sshClient.Close()
				sshClient = nil
				if !baselineCaptured {
					// Cannot emit a delta without a baseline.
					continue
				}
				out <- model.DataPoint{
					TimestampMs:  tickTime.UnixMilli(),
					IsError:      true,
					ErrorMessage: classifySshError(pollError, sshConfig.Host),
				}
				continue
			}

			counterSection, talkerSection := splitTalkerSection(commandOutput)

			currentDownloadBytes, currentUploadBytes, parseError := parseCounters(counterSection)
			if parseError != nil {
				slog.Warn("SSH counter parse error", "host", sshConfig.Host, "err", parseError)
				out <- model.DataPoint{
					TimestampMs:  tickTime.UnixMilli(),
					IsError:      true,
					ErrorMessage: "SSH value parse error",
				}
				continue
			}

			slog.Debug("SSH raw counters",
				"host", sshConfig.Host,
				"downloadBytes", currentDownloadBytes,
				"uploadBytes", currentUploadBytes)

			var currentTalkerTotals map[string]talkerTotals
			if talkerEnabled {
				currentTalkerTotals = parseTalkerTotals(talkerSection, lanPrefix)
			}

			if !baselineCaptured {
				previousDownloadBytes = currentDownloadBytes
				previousUploadBytes = currentUploadBytes
				previousTalkerTotals = currentTalkerTotals
				baselineCaptured = true
				slog.Info("SSH baseline captured", "host", sshConfig.Host,
					"downloadBytes", currentDownloadBytes, "uploadBytes", currentUploadBytes)
				continue
			}

			downloadBytesPerSec := counter.ComputeDelta(previousDownloadBytes, currentDownloadBytes)
			uploadBytesPerSec := counter.ComputeDelta(previousUploadBytes, currentUploadBytes)

			var talkerWinners topTalkers
			if talkerEnabled {
				talkerWinners = pickTopTalkers(previousTalkerTotals, currentTalkerTotals)
				previousTalkerTotals = currentTalkerTotals
			}

			previousDownloadBytes = currentDownloadBytes
			previousUploadBytes = currentUploadBytes

			slog.Debug("SSH poll result",
				"host", sshConfig.Host,
				"downloadBytesPerSec", downloadBytesPerSec,
				"uploadBytesPerSec", uploadBytesPerSec)

			if !firstSuccessLogged {
				slog.Info("SSH first successful poll", "host", sshConfig.Host,
					"downloadBytesPerSec", downloadBytesPerSec,
					"uploadBytesPerSec", uploadBytesPerSec)
				firstSuccessLogged = true
			}

			out <- model.DataPoint{
				TimestampMs:                  tickTime.UnixMilli(),
				DownloadBytesPerSec:          downloadBytesPerSec,
				UploadBytesPerSec:            uploadBytesPerSec,
				IsError:                      false,
				TopDownloadIP:          talkerLabel(leaseNames, talkerWinners.downloadIP),
				TopDownloadBytesPerSec: talkerWinners.downloadBytesPerSec,
				TopUploadIP:            talkerLabel(leaseNames, talkerWinners.uploadIP),
				TopUploadBytesPerSec:   talkerWinners.uploadBytesPerSec,
			}
		}
	}
}

// FetchHostKey dials the host just far enough to capture its host key during
// the SSH handshake and returns the key's SHA256 fingerprint. No
// authentication is attempted, so it works before any credentials are set up.
func FetchHostKey(hostConfig *config.HostConfig) (string, error) {
	address := net.JoinHostPort(hostConfig.Host, strconv.Itoa(int(hostConfig.Port)))
	var fingerprint string
	clientConfig := &ssh.ClientConfig{
		User: "tidemark-hostkey",
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			fingerprint = ssh.FingerprintSHA256(key)
			return nil
		},
		Timeout: time.Duration(hostConfig.TimeoutMs) * time.Millisecond,
	}
	sshClient, dialError := ssh.Dial("tcp", address, clientConfig)
	if sshClient != nil {
		sshClient.Close()
	}
	// The dial is expected to fail after key exchange (no auth methods are
	// offered); the handshake still delivered the host key.
	if fingerprint != "" {
		return fingerprint, nil
	}
	return "", dialError
}

// buildClientConfig loads the private key and assembles the ssh.ClientConfig,
// including the host key verification policy.
func buildClientConfig(hostConfig *config.HostConfig) (*ssh.ClientConfig, error) {
	keyBytes, readError := os.ReadFile(hostConfig.KeyFile)
	if readError != nil {
		return nil, fmt.Errorf("reading key file %q: %w", hostConfig.KeyFile, readError)
	}
	signer, parseError := ssh.ParsePrivateKey(keyBytes)
	if parseError != nil {
		return nil, fmt.Errorf("parsing key file %q: %w", hostConfig.KeyFile, parseError)
	}

	return &ssh.ClientConfig{
		User:            hostConfig.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: makeHostKeyCallback(hostConfig),
		Timeout:         time.Duration(hostConfig.TimeoutMs) * time.Millisecond,
	}, nil
}

// makeHostKeyCallback returns the host key verification policy: when a
// fingerprint is pinned in the config it must match; otherwise any key is
// accepted and its fingerprint is logged so the user can pin it.
func makeHostKeyCallback(hostConfig *config.HostConfig) ssh.HostKeyCallback {
	pinnedFingerprint := strings.TrimPrefix(strings.TrimSpace(hostConfig.HostKey), "SHA256:")
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		actualFingerprint := ssh.FingerprintSHA256(key)
		if pinnedFingerprint == "" {
			slog.Info("SSH host key accepted (no pinned fingerprint in config)",
				"host", hostConfig.Host, "fingerprint", actualFingerprint)
			return nil
		}
		if strings.TrimPrefix(actualFingerprint, "SHA256:") != pinnedFingerprint {
			return fmt.Errorf("host key mismatch for %s: got %s, pinned SHA256:%s",
				hostConfig.Host, actualFingerprint, pinnedFingerprint)
		}
		return nil
	}
}

// runCommandWithRetries executes command on the client, retrying up to
// retries extra times, mirroring gosnmp's per-poll retransmits.
func runCommandWithRetries(ctx context.Context, sshClient *ssh.Client, command string,
	timeout time.Duration, retries int) ([]byte, error) {
	var lastError error
	for attempt := 0; attempt <= retries; attempt++ {
		output, runError := runCommand(ctx, sshClient, command, timeout)
		if runError == nil {
			return output, nil
		}
		lastError = runError
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastError
}

// runCommand executes command in a fresh session on the (kept-alive) client,
// enforcing timeout as a deadline. Sessions are lightweight channels over the
// existing connection, so this does not re-handshake.
func runCommand(ctx context.Context, sshClient *ssh.Client, command string,
	timeout time.Duration) ([]byte, error) {
	session, sessionError := sshClient.NewSession()
	if sessionError != nil {
		return nil, fmt.Errorf("opening session: %w", sessionError)
	}
	defer session.Close()

	type commandResult struct {
		output []byte
		err    error
	}
	resultChannel := make(chan commandResult, 1)
	go func() {
		output, runError := session.Output(command)
		resultChannel <- commandResult{output: output, err: runError}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-resultChannel:
		return result.output, result.err
	case <-timer.C:
		// Closing the session unblocks the goroutine's session.Output call.
		session.Close()
		return nil, errPollTimeout
	case <-ctx.Done():
		session.Close()
		return nil, ctx.Err()
	}
}

// errPollTimeout marks a poll that exceeded the configured timeout.
var errPollTimeout = fmt.Errorf("poll timed out")

// talkerSeparator is the marker line the poll command emits between the
// interface counters and the per-IP conntrack totals.
const talkerSeparator = "==="

// talkerAwkProgram aggregates /proc/net/nf_conntrack on the remote host into
// one "ip uploadbytes downloadbytes" line per originating source IP. The
// first src= of an entry is the original-direction source (the pre-NAT LAN IP
// for outbound connections). The entry's first bytes= counter is the
// original direction (LAN host sending = upload), the second is the reply
// direction (LAN host receiving = download). Requires nf_conntrack_acct=1
// (OpenWrt default); without it no bytes= fields exist and the output is
// empty.
const talkerAwkProgram = `{ip="";u=0;d=0;n=0; for(i=1;i<=NF;i++){ if(ip=="" && index($i,"src=")==1) ip=substr($i,5); else if(index($i,"bytes=")==1){n++; if(n==1)u=substr($i,7)+0; else if(n==2)d=substr($i,7)+0} } if(ip!="" && n>0){up[ip]+=u; dn[ip]+=d}} END{for(k in up) print k, up[k], dn[k]}`

// buildPollCommand returns the remote command run once per second. The base
// command reads the interface byte counters; with the top-talker feature it
// additionally dumps per-IP conntrack byte totals after a separator line. The
// awk part is best-effort: if conntrack is unavailable the section is empty
// and only the talker info is lost, never the bandwidth sample.
func buildPollCommand(interfaceName string, talkerEnabled bool) string {
	counterCommand := fmt.Sprintf(
		"cat /sys/class/net/%s/statistics/rx_bytes /sys/class/net/%s/statistics/tx_bytes",
		interfaceName, interfaceName)
	if !talkerEnabled {
		return counterCommand
	}
	return fmt.Sprintf("%s && echo %s && { awk '%s' /proc/net/nf_conntrack 2>/dev/null || true; }",
		counterCommand, talkerSeparator, talkerAwkProgram)
}

// splitTalkerSection splits the poll output at the separator line into the
// counter part and the talker part. Without a separator (feature disabled, or
// the remote command was cut short) the whole output is the counter part.
func splitTalkerSection(commandOutput []byte) (counterSection []byte, talkerSection string) {
	outputText := string(commandOutput)
	separatorIndex := strings.Index(outputText, "\n"+talkerSeparator)
	if separatorIndex < 0 {
		return commandOutput, ""
	}
	counterPart := outputText[:separatorIndex]
	talkerPart := outputText[separatorIndex+len("\n"+talkerSeparator):]
	return []byte(counterPart), talkerPart
}

// leaseCommand dumps the dnsmasq DHCP lease table. /tmp/dhcp.leases is the
// OpenWrt (and general dnsmasq) default location; on hosts without it the
// command yields no output and talkers simply keep showing as bare IPs.
const leaseCommand = "cat /tmp/dhcp.leases 2>/dev/null || true"

// fetchLeaseNames reads the remote DHCP lease table and returns an
// ip -> hostname map for labeling top talkers. Best-effort: any failure
// returns an empty map and only the name labels are lost.
func fetchLeaseNames(ctx context.Context, sshClient *ssh.Client,
	timeout time.Duration, host string) map[string]string {
	leaseOutput, leaseError := runCommand(ctx, sshClient, leaseCommand, timeout)
	if leaseError != nil {
		slog.Warn("DHCP lease fetch failed, talkers will show as IPs",
			"host", host, "err", leaseError)
		return nil
	}
	leaseNames := parseLeaseNames(string(leaseOutput))
	slog.Info("DHCP lease names loaded", "host", host, "count", len(leaseNames))
	return leaseNames
}

// parseLeaseNames parses dnsmasq lease lines ("expiry mac ip hostname
// clientid") into an ip -> hostname map. Leases where the client sent no
// hostname (recorded as "*") and malformed lines are skipped.
func parseLeaseNames(leaseOutput string) map[string]string {
	leaseNames := make(map[string]string)
	for _, line := range strings.Split(leaseOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[3] == "*" {
			continue
		}
		if _, addressError := netip.ParseAddr(fields[2]); addressError != nil {
			continue
		}
		leaseNames[fields[2]] = fields[3]
	}
	return leaseNames
}

// talkerLabel returns the DHCP hostname for ip when one is known, otherwise
// the ip itself. An empty ip (no talker that second) stays empty.
func talkerLabel(leaseNames map[string]string, ip string) string {
	if name, isKnown := leaseNames[ip]; isKnown {
		return name
	}
	return ip
}

// talkerTotals holds one LAN IP's cumulative conntrack byte counters, split
// by direction from the LAN host's point of view.
type talkerTotals struct {
	uploadBytes   uint64 // original direction: bytes sent by the LAN host
	downloadBytes uint64 // reply direction: bytes received by the LAN host
}

// topTalkers identifies the per-direction winners of one second: the LAN IP
// that downloaded the most and the LAN IP that uploaded the most (they are
// often, but not necessarily, the same host).
type topTalkers struct {
	downloadIP          string
	downloadBytesPerSec float64
	uploadIP            string
	uploadBytesPerSec   float64
}

// parseTalkerTotals parses "ip uploadbytes downloadbytes" lines into a map,
// keeping only IPs inside lanPrefix. Malformed lines are skipped; a poll must
// never fail because of talker data.
func parseTalkerTotals(talkerSection string, lanPrefix netip.Prefix) map[string]talkerTotals {
	totals := make(map[string]talkerTotals)
	for _, line := range strings.Split(talkerSection, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		address, addressError := netip.ParseAddr(fields[0])
		if addressError != nil || !lanPrefix.Contains(address) {
			continue
		}
		uploadBytes, uploadError := strconv.ParseUint(fields[1], 10, 64)
		if uploadError != nil {
			continue
		}
		downloadBytes, downloadError := strconv.ParseUint(fields[2], 10, 64)
		if downloadError != nil {
			continue
		}
		entry := totals[fields[0]]
		entry.uploadBytes += uploadBytes
		entry.downloadBytes += downloadBytes
		totals[fields[0]] = entry
	}
	return totals
}

// talkerDelta returns the per-second byte delta for one cumulative counter.
// An IP absent from the previous map contributes its full current total (all
// of its flows started since the last poll). A shrinking total (flows
// expired) is clamped to zero.
func talkerDelta(previousBytes, currentBytes uint64, wasPresent bool) uint64 {
	switch {
	case !wasPresent:
		return currentBytes
	case currentBytes > previousBytes:
		return currentBytes - previousBytes
	}
	return 0
}

// pickTopTalkers returns, for each direction, the IP with the largest
// positive byte delta between the previous and current per-IP conntrack
// totals. A direction with no traffic gets an empty IP.
func pickTopTalkers(previousTotals, currentTotals map[string]talkerTotals) topTalkers {
	var winners topTalkers
	var topDownloadDelta, topUploadDelta uint64
	for ip, current := range currentTotals {
		previous, wasPresent := previousTotals[ip]
		downloadDelta := talkerDelta(previous.downloadBytes, current.downloadBytes, wasPresent)
		uploadDelta := talkerDelta(previous.uploadBytes, current.uploadBytes, wasPresent)
		if downloadDelta > topDownloadDelta {
			topDownloadDelta = downloadDelta
			winners.downloadIP = ip
		}
		if uploadDelta > topUploadDelta {
			topUploadDelta = uploadDelta
			winners.uploadIP = ip
		}
	}
	winners.downloadBytesPerSec = float64(topDownloadDelta)
	winners.uploadBytesPerSec = float64(topUploadDelta)
	return winners
}

// parseCounters extracts the two decimal counter lines (rx then tx) produced
// by the poll command.
func parseCounters(commandOutput []byte) (downloadBytes, uploadBytes uint64, err error) {
	lines := strings.Fields(strings.TrimSpace(string(commandOutput)))
	if len(lines) != 2 {
		return 0, 0, fmt.Errorf("expected 2 counter values, got %d in %q", len(lines), commandOutput)
	}
	downloadBytes, downloadError := strconv.ParseUint(lines[0], 10, 64)
	if downloadError != nil {
		return 0, 0, fmt.Errorf("parsing rx_bytes %q: %w", lines[0], downloadError)
	}
	uploadBytes, uploadError := strconv.ParseUint(lines[1], 10, 64)
	if uploadError != nil {
		return 0, 0, fmt.Errorf("parsing tx_bytes %q: %w", lines[1], uploadError)
	}
	return downloadBytes, uploadBytes, nil
}

// classifySshError returns a short human-readable description of the error.
func classifySshError(err error, host string) string {
	if err == errPollTimeout {
		return fmt.Sprintf("timeout polling %s", host)
	}
	if netErr, isNetworkError := err.(net.Error); isNetworkError && netErr.Timeout() {
		return fmt.Sprintf("timeout polling %s", host)
	}
	return fmt.Sprintf("network error: %s", err.Error())
}
