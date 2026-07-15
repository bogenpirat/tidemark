<div align="center">

<img src="assets/tidemark.svg" alt="Tidemark logo" width="120" height="120" />

# Tidemark

**A lightweight, always-on-top realtime network throughput monitor for Windows.**

Tidemark polls a host once per second — via SNMP or SSH — and plots live
upload/download throughput on a smooth, scrolling graph — one compact window per device.

[![Build](https://github.com/bogenpirat/tidemark/actions/workflows/build.yml/badge.svg)](../../actions/workflows/build.yml)
[![Release](https://github.com/bogenpirat/tidemark/actions/workflows/release.yml/badge.svg)](../../actions/workflows/release.yml)
[![Latest release](https://img.shields.io/github/v/release/bogenpirat/tidemark?sort=semver)](../../releases/latest)
![Platform](https://img.shields.io/badge/platform-Windows-0A2540)
![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)

</div>

---

## ✨ Features

- **Live throughput graph** — up/download rates sampled every second on a scrolling chart.
- **SNMP and SSH** — poll any SNMP v1/v2c device, or any Linux host over SSH with key-file authentication.
- **Multiple hosts** — monitor several interfaces in one window, or run separate instances side by side.
- **Tiny & frameless** — a borderless window that remembers its size and position.
- **Dark & light themes** — toggle from the right-click context menu.
- **At-a-glance stats** — current, max and average rate per host.
- **Self-contained** — a single `tidemark.exe`, no installer, no runtime, no dependencies.

## 🚀 Quick start

### 1. Download

Grab the latest `tidemark-windows-amd64.zip` from the
[**Releases**](../../releases/latest) page and unzip it anywhere. You'll get:

```
tidemark.exe          # the application
example-config.json   # a template you can copy and edit
README.md
```

### 2. Create a config file

Tidemark is launched with a JSON config that tells it which host(s) to poll.
Copy the template and edit it:

```powershell
Copy-Item example-config.json my-router.json
notepad my-router.json
```

A minimal SNMP config only needs a host and a community string:

```json
{
  "hosts": [
    {
      "host": "192.168.1.1",
      "name": "Main Router",
      "community": "public",
      "downloadOID": "1.3.6.1.2.1.31.1.1.1.6.1",
      "uploadOID":   "1.3.6.1.2.1.31.1.1.1.10.1"
    }
  ]
}
```

> 💡 **Finding the right OIDs.** The defaults read the 64-bit `ifHCInOctets` /
> `ifHCOutOctets` counters for interface index **1**. The trailing `.1` is the
> interface index — change it (`.2`, `.3`, …) to monitor a different port. Use a
> tool like `snmpwalk` to discover which index maps to which interface on your device.

A minimal SSH config needs a private key file and the name of the network
interface to monitor on the remote Linux host:

```json
{
  "hosts": [
    {
      "host": "192.168.1.1",
      "name": "Main Router",
      "protocol": "ssh",
      "keyFile": "C:\\Users\\me\\.ssh\\id_ed25519",
      "interface": "pppoe-wan"
    }
  ]
}
```

### 3. Run it

Pass the config file as the only argument:

```powershell
.\tidemark.exe my-router.json
```

The window opens immediately and starts plotting. **Right-click** anywhere in the
window for the context menu (settings, theme toggle, exit). **Drag** the window to
move it — its position and size are saved back into the config file on exit.

### Monitoring several devices

You can list multiple interfaces in the `hosts` array of a single config, **or**
launch one instance per device with its own config file — handy for keeping each
graph in its own window:

```powershell
.\tidemark.exe router.json
.\tidemark.exe switch.json
.\tidemark.exe nas.json
```

## ⚙️ Configuration reference

The config file is a top-level object with optional window/theme settings plus a
`hosts` array. (A bare single-host object is also accepted for backwards compatibility.)

### Top-level options

| Field            | Type    | Default | Description                                      |
|------------------|---------|---------|--------------------------------------------------|
| `hosts`          | array   | —       | List of hosts to monitor (see below).            |
| `darkTheme`      | bool    | `true`  | Use the dark color scheme.                       |
| `windowWidthDp`  | number  | `1000`  | Window width (device-independent pixels).        |
| `windowHeightDp` | number  | auto    | Window height. Auto-sized to the number of hosts.|
| `windowX`        | number  | OS      | Saved top-left X position (physical pixels).     |
| `windowY`        | number  | OS      | Saved top-left Y position (physical pixels).     |

> Window geometry and theme are written back automatically when you move, resize,
> or close the window — you normally never set these by hand.

### Per-host options

| Field         | Type   | Required   | Default                          | Description                                            |
|---------------|--------|------------|----------------------------------|--------------------------------------------------------|
| `host`        | string | ✅         | —                                | IP address or hostname of the device.                  |
| `protocol`    | string |            | `"snmp2c"`                       | Polling protocol: `snmp1`, `snmp2c`, or `ssh`.          |
| `name`        | string |            | (the host address)               | Friendly label shown on the graph.                     |
| `port`        | number |            | `161` (SNMP) / `22` (SSH)        | SNMP UDP port or SSH TCP port.                          |
| `community`   | string | ✅ (SNMP)  | —                                | SNMP community string.                                  |
| `downloadOID` | string |            | `1.3.6.1.2.1.31.1.1.1.6.1`       | SNMP only. OID for the inbound (download) byte counter. |
| `uploadOID`   | string |            | `1.3.6.1.2.1.31.1.1.1.10.1`      | SNMP only. OID for the outbound (upload) byte counter.  |
| `username`    | string |            | `"root"`                         | SSH only. Login user on the remote host.                |
| `keyFile`     | string | ✅ (SSH)   | —                                | SSH only. Path to the private key file used to authenticate. |
| `interface`   | string | ✅ (SSH)   | —                                | SSH only. Network interface to monitor on the remote host (e.g. `pppoe-wan`, `eth0`). |
| `hostKey`     | string |            | (accept any)                     | SSH only. Expected SHA256 fingerprint of the server's host key. When unset, any key is accepted and the fingerprint is logged so you can pin it here. |
| `timeoutMs`   | number |            | `3000`                           | Per-poll timeout in milliseconds.                       |
| `retries`     | number |            | `1`                              | Retry count per poll.                                   |

### Monitoring over SSH

With `"protocol": "ssh"`, Tidemark works with **any Linux host** it can reach
over SSH. It opens one connection at startup, authenticates with the given
private key file, and keeps the connection alive. Once per second it reads the
kernel's interface byte counters:

```
/sys/class/net/<interface>/statistics/rx_bytes   → download
/sys/class/net/<interface>/statistics/tx_bytes   → upload
```

This is a plain file read of two kernel counters — it terminates immediately
and adds no measurable load on the remote machine. The per-second deltas are
graphed exactly like SNMP counter deltas, and a dropped connection shows up as
error samples on the graph until the host is reachable again (Tidemark
reconnects automatically).

**Host key pinning.** By default any host key is accepted, and the server's
SHA256 fingerprint is logged on connect. To protect against man-in-the-middle
attacks, pin the fingerprint in the host's `hostKey` field — from then on a
mismatching host key makes the connection fail. To fetch the fingerprint(s),
run Tidemark with the `-hostkey` switch — it connects to each ssh host in the
config (no authentication needed), prints the fingerprints, and exits without
opening a window:

```powershell
.\tidemark.exe -hostkey my-router.json
# Main Router (192.168.1.1:22): SHA256:NcW9jUnKvRk3…
```

## 🛠️ Building from source

Tidemark is a [Go](https://go.dev) project that builds with [Mage](https://magefile.org).

**Prerequisites:** Go 1.26+ and the build tools (one-off install):

```powershell
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
go install github.com/magefile/mage@latest
```

**Build targets** (run `mage -l` to list them):

| Command         | Output                                                        |
|-----------------|---------------------------------------------------------------|
| `mage` / `mage release` | Optimized, windowless `tidemark.exe` (production build).|
| `mage debug`    | Unoptimized build with a console attached for log output.     |
| `mage generate` | Regenerate the embedded icon + version resource only.         |
| `mage clean`    | Remove build artifacts.                                       |

```powershell
mage release
.\tidemark.exe example-config.json
```

## 📦 Releases

Pushing to the default branch automatically builds a release binary, tags the
commit, and publishes a GitHub Release with `tidemark-windows-amd64.zip` attached.
See [`.github/workflows/release.yml`](.github/workflows/release.yml).

## 📄 License

See the repository for license details.
