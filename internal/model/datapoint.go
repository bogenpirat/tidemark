package model

// DataPoint holds one second's worth of network throughput data captured from
// an SNMP poll. Fields are zero-valued when IsError is true.
type DataPoint struct {
	TimestampMs         int64
	DownloadBytesPerSec float64 // bytes/sec received on the monitored interface
	UploadBytesPerSec   float64 // bytes/sec transmitted on the monitored interface
	IsError             bool
	ErrorMessage        string
}
