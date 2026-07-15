package model

// DataPoint holds one second's worth of network throughput data captured from
// a poll. Fields are zero-valued when IsError is true.
type DataPoint struct {
	TimestampMs         int64
	DownloadBytesPerSec float64 // bytes/sec received on the monitored interface
	UploadBytesPerSec   float64 // bytes/sec transmitted on the monitored interface
	IsError             bool
	ErrorMessage        string

	// TopDownloadIP / TopUploadIP label the LAN-internal hosts that received
	// (download) and sent (upload) the most traffic during this second, when
	// the host's lanSubnet feature is enabled. The label is the device's DHCP
	// hostname when the router's lease table knows one, otherwise its IP.
	// Often the same host in both directions, but not necessarily. Empty when
	// the feature is disabled, the poll errored, or no LAN traffic was
	// observed in that direction.
	TopDownloadIP string
	// TopDownloadBytesPerSec is the byte rate received by TopDownloadIP
	// during this second.
	TopDownloadBytesPerSec float64
	TopUploadIP            string
	// TopUploadBytesPerSec is the byte rate sent by TopUploadIP during this
	// second.
	TopUploadBytesPerSec float64
}
