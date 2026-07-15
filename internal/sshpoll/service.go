// Package sshpoll polls a Linux host over SSH at 1-second intervals, reading
// kernel interface byte counters, and sends DataPoints to an output channel.
// It mirrors the structure and error-handling semantics of the SNMP service.
package sshpoll

import (
	"context"
	"fmt"
	"log/slog"
	"net"
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
	pollCommand := fmt.Sprintf(
		"cat /sys/class/net/%s/statistics/rx_bytes /sys/class/net/%s/statistics/tx_bytes",
		sshConfig.Interface, sshConfig.Interface)
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

	var previousDownloadBytes uint64
	var previousUploadBytes uint64
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

			currentDownloadBytes, currentUploadBytes, parseError := parseCounters(commandOutput)
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

			if !baselineCaptured {
				previousDownloadBytes = currentDownloadBytes
				previousUploadBytes = currentUploadBytes
				baselineCaptured = true
				slog.Info("SSH baseline captured", "host", sshConfig.Host,
					"downloadBytes", currentDownloadBytes, "uploadBytes", currentUploadBytes)
				continue
			}

			downloadBytesPerSec := counter.ComputeDelta(previousDownloadBytes, currentDownloadBytes)
			uploadBytesPerSec := counter.ComputeDelta(previousUploadBytes, currentUploadBytes)

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
				TimestampMs:         tickTime.UnixMilli(),
				DownloadBytesPerSec: downloadBytesPerSec,
				UploadBytesPerSec:   uploadBytesPerSec,
				IsError:             false,
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
