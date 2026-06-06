package snmp

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"time"

	"tidemark/internal/config"
	"tidemark/internal/model"

	gosnmp "github.com/gosnmp/gosnmp"
)

const (
	maxUint64AsFloat = float64(math.MaxUint64)
	// counter64WrapThreshold detects wrap-around: if the delta is more than half
	// of 2^64, assume the counter wrapped and compute the wrapped delta instead.
	counter64WrapThreshold = float64(1 << 63)
)

// SnmpService polls an SNMP v2c host at 1-second intervals and sends
// DataPoints to an output channel.
type SnmpService struct {
	appConfig *config.AppConfig
}

// NewService creates an SnmpService configured from the provided AppConfig.
func NewService(appConfig *config.AppConfig) *SnmpService {
	return &SnmpService{appConfig: appConfig}
}

// Start begins the polling loop in the calling goroutine. It sends DataPoints
// to the out channel and returns when ctx is cancelled or a fatal error occurs.
// The out channel is not closed by this function; callers should not rely on it.
func (snmpService *SnmpService) Start(ctx context.Context, out chan<- model.DataPoint) {
	snmpConfig := snmpService.appConfig

	snmpSession := &gosnmp.GoSNMP{
		Target:             snmpConfig.Host,
		Port:               snmpConfig.Port,
		Community:          snmpConfig.Community,
		Version:            gosnmp.Version2c,
		Timeout:            time.Duration(snmpConfig.TimeoutMs) * time.Millisecond,
		Retries:            snmpConfig.Retries,
		MaxOids:            gosnmp.MaxOids,
		ExponentialTimeout: false,
	}

	if connectError := snmpSession.Connect(); connectError != nil {
		slog.Error("SNMP connect failed", "host", snmpConfig.Host, "err", connectError)
		return
	}
	defer snmpSession.Conn.Close()
	slog.Info("SNMP session established", "host", snmpConfig.Host, "port", snmpConfig.Port,
		"community", snmpConfig.Community,
		"downloadOID", snmpConfig.DownloadOID,
		"uploadOID", snmpConfig.UploadOID)

	downloadOID := snmpConfig.DownloadOID
	uploadOID := snmpConfig.UploadOID
	oidList := []string{downloadOID, uploadOID}

	var previousDownloadOctets uint64
	var previousUploadOctets uint64
	baselineCaptured := false
	firstSuccessLogged := false

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("SNMP service stopping", "host", snmpConfig.Host)
			return

		case tickTime := <-ticker.C:
			snmpResult, getError := snmpSession.Get(oidList)

			if getError != nil {
				errorMessage := classifySnmpError(getError, snmpConfig.Host)
				slog.Warn("SNMP poll failed", "host", snmpConfig.Host, "err", getError)
				if !baselineCaptured {
					// Cannot emit a delta without a baseline.
					continue
				}
				out <- model.DataPoint{
					TimestampMs:  tickTime.UnixMilli(),
					IsError:      true,
					ErrorMessage: errorMessage,
				}
				continue
			}

			if len(snmpResult.Variables) < 2 {
				slog.Warn("SNMP response has unexpected variable count",
					"host", snmpConfig.Host, "count", len(snmpResult.Variables))
				out <- model.DataPoint{
					TimestampMs:  tickTime.UnixMilli(),
					IsError:      true,
					ErrorMessage: "unexpected SNMP response: too few variables",
				}
				continue
			}

			currentDownloadOctets, parseDownloadError := extractUint64(snmpResult.Variables[0])
			currentUploadOctets, parseUploadError := extractUint64(snmpResult.Variables[1])

			if parseDownloadError != nil || parseUploadError != nil {
				slog.Warn("SNMP value parse error", "host", snmpConfig.Host,
					"downloadErr", parseDownloadError, "uploadErr", parseUploadError)
				out <- model.DataPoint{
					TimestampMs:  tickTime.UnixMilli(),
					IsError:      true,
					ErrorMessage: "SNMP value type mismatch",
				}
				continue
			}

			slog.Debug("SNMP raw counters",
				"host", snmpConfig.Host,
				"downloadOctets", currentDownloadOctets,
				"uploadOctets", currentUploadOctets)

			if !baselineCaptured {
				previousDownloadOctets = currentDownloadOctets
				previousUploadOctets = currentUploadOctets
				baselineCaptured = true
				slog.Info("SNMP baseline captured", "host", snmpConfig.Host,
					"downloadOctets", currentDownloadOctets, "uploadOctets", currentUploadOctets)
				continue
			}

			downloadBytesPerSec := computeCounterDelta(previousDownloadOctets, currentDownloadOctets)
			uploadBytesPerSec := computeCounterDelta(previousUploadOctets, currentUploadOctets)

			previousDownloadOctets = currentDownloadOctets
			previousUploadOctets = currentUploadOctets

			slog.Debug("SNMP poll result",
				"host", snmpConfig.Host,
				"downloadBytesPerSec", downloadBytesPerSec,
				"uploadBytesPerSec", uploadBytesPerSec)

			if !firstSuccessLogged {
				slog.Info("SNMP first successful poll", "host", snmpConfig.Host,
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

// computeCounterDelta returns the increase in a 64-bit counter, handling
// wrap-around by assuming a forward-only counter.
func computeCounterDelta(previousValue, currentValue uint64) float64 {
	if currentValue >= previousValue {
		return float64(currentValue - previousValue)
	}
	// Counter wrapped around 2^64.
	slog.Warn("64-bit SNMP counter wrap-around detected",
		"previous", previousValue, "current", currentValue)
	wrappedDelta := float64(math.MaxUint64-previousValue) + float64(currentValue) + 1
	return wrappedDelta
}

// extractUint64 pulls a Counter64 or Counter32 value from an SNMP variable.
func extractUint64(variable gosnmp.SnmpPDU) (uint64, error) {
	switch variable.Type {
	case gosnmp.Counter64:
		rawValue, isCorrectType := variable.Value.(uint64)
		if !isCorrectType {
			return 0, fmt.Errorf("Counter64 value is not uint64 for OID %s", variable.Name)
		}
		return rawValue, nil
	case gosnmp.Counter32, gosnmp.Gauge32:
		rawValue, isCorrectType := variable.Value.(uint)
		if !isCorrectType {
			return 0, fmt.Errorf("Counter32/Gauge32 value is not uint for OID %s", variable.Name)
		}
		return uint64(rawValue), nil
	default:
		return 0, fmt.Errorf("unexpected SNMP type %v for OID %s", variable.Type, variable.Name)
	}
}

// classifySnmpError returns a short human-readable description of the error.
func classifySnmpError(err error, host string) string {
	if netErr, isNetworkError := err.(net.Error); isNetworkError && netErr.Timeout() {
		return fmt.Sprintf("timeout polling %s", host)
	}
	return fmt.Sprintf("network error: %s", err.Error())
}
