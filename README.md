<p align="center">
  <img src="ui/src/assets/img/logo/casa-white.svg" width="96" height="96" alt="NivaroOS Logo" style="filter: drop-shadow(0 4px 12px rgba(37, 99, 235, 0.4));">
</p>

<h1 align="center">NivaroOS v1.0</h1>

<p align="center">
  <strong>A modern, self-hosted personal cloud OS, container platform, and companion mobile ecosystem.</strong><br>
  Desktop-class windowed multitasking, Docker container studio, KVM virtual machines, unified file management with companion device integration, and a rich ZimaOS-inspired App Store — seamlessly bridged with a native mobile companion app.
</p>

<p align="center">
  <a href="https://github.com/F-e-n-y-x/NivaroOS/releases/tag/v1.0.0"><img src="https://img.shields.io/badge/Release-v1.0.0-2563eb?style=flat-square" alt="Version 1.0.0"></a>
  <a href="https://github.com/F-e-n-y-x/NivaroOS/blob/master/LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-emerald.svg?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Platform-Debian%20%7C%20Ubuntu%20%7C%20Android-orange?style=flat-square" alt="Platform">
  <img src="https://img.shields.io/badge/Architecture-amd64%20%7C%20arm64-blueviolet?style=flat-square" alt="Architecture">
</p>

---

## ⚡ Quick Install (Host OS)

Install NivaroOS on any clean **Debian 12+** or **Ubuntu 22.04+** system with a single command:

```bash
curl -fsSL https://raw.githubusercontent.com/F-e-n-y-x/NivaroOS/master/installer/install.sh | sudo bash
```

#### Installer Flags & Options

| Flag | Description |
| :--- | :--- |
| `--with-vm` | Automatically installs QEMU/KVM packages and enables the VM Manager sidecar. |
| `--without-vm` | Skips QEMU/KVM packages for a lighter installation. |
| `-y`, `--yes` | Unattended installation (accepts all defaults without interactive prompts). |

Once installation finishes, open your browser and navigate to `http://<your-server-ip>` to access your desktop!

---

## 📱 Mobile Companion App

NivaroOS includes a native Flutter mobile app for Android and iOS that brings your entire personal cloud and server infrastructure into your pocket.

- **Download APK**: Grab the latest release APK from the [Releases](https://github.com/F-e-n-y-x/NivaroOS/releases/tag/v1.0.0) page (`NivaroOS.apk`).
- **Source Code**: Available in the [`mobile/`](mobile/) directory.

---

## 🐳 Run with Docker (Try Without Installing)

Want to test NivaroOS without modifying your host system? You can run NivaroOS instantly in a Docker container:

```bash
docker run -d \
  --name nivaroos \
  --restart unless-stopped \
  --privileged \
  -p 80:80 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v nivaroos_data:/DATA \
  -v nivaroos_config:/etc/nivaroos \
  ghcr.io/f-e-n-y-x/nivaroos:latest
```

---

## 🌟 Comprehensive Feature Set

### 🖥️ WebUI Desktop Experience

#### 1. Windowed Desktop & Multitasking
- **Native Windowed Environment**: Run multiple applications side-by-side with full window management controls (minimize, maximize, drag, resize handles, snap, and dock pinning).
- **Session Persistence**: Automatically saves and restores open window states, dimensions, and desktop positions across browser reloads.
- **Modern Glassmorphic UI**: Translucent frosted-glass headers, customizable backdrop blur, custom wallpaper uploads, and synchronized lock/login screen backgrounds.
- **Top Bar & Notification Center**: Quick access to system status, companion devices, running tasks, notifications, and power controls.

#### 2. ZimaOS-Inspired Windowed App Store
- **Extensive Catalog**: Browse, search, and install from an expansive catalog of 400+ containerized self-hosted applications with instant category filtering.
- **Discover & Spotlight Carousel**: Animated hero banner showcasing trending apps, categorized curated rows for Media, AI/LLMs, Home Automation, and Developer Tools.
- **App Details & Screenshots**: Interactive 16:9 lightbox screenshot viewer, system memory requirements, developer links, architecture tags (`amd64`, `arm64`), and mapped port listings.
- **Custom App Repositories**: Easily add and manage custom third-party community app store sources.

#### 3. Container Studio & Bidirectional Compose Sync
- **Card-Based Visual Editor**: Intuitive forms to configure container images, web UI ports, dynamic host/container port mappings (TCP/UDP), volume mounts (`rw`/`ro`), environment variables, restart policies, resource limits (CPU/RAM), and hardware device passthrough (NVIDIA GPU, `/dev/dri`).
- **Live 2-Way Compose YAML Sync**: Real-time bidirectional synchronization between visual configuration cards and the monospace Docker Compose YAML editor. Changes made in the form immediately update the YAML, and direct edits in YAML instantly update the form.
- **Import & Export**: Drag-and-drop Docker Compose files, convert `docker run` commands with one click, or export configurations as standard Compose files.

#### 4. Real-time Download & Install HUD
- **Floating Global Install Dock**: Sleek floating card displaying live pulling and extracting stages, dynamic progress bars, and completion badges.
- **Desktop Grid Live Installing Tiles**: Newly added applications appear immediately on your desktop grid with animated SVG progress rings while downloading.

#### 5. KVM Virtual Machine Manager
- **Full QEMU / KVM Virtualization**: Provision, launch, and manage full Linux and Windows virtual machines.
- **Integrated Web Console (noVNC)**: High-performance graphical and terminal access to VMs directly within a movable desktop window or in standalone full-screen mode.
- **Flexible Hardware Sizing**: Configure vCPUs, RAM allocation, virtual disks, ISO boot installation media, and network interfaces.

#### 6. Files & Storage Hub
- **Modern File Browser**: Fast hierarchical tree navigation, drag-and-drop transfers, chunked multi-file upload tray, batch actions, and breadcrumb trails.
- **Multi-Format Previews**:
  - **Video Player**: Integrated Artplayer with seekable HTTP Range (`206 Partial Content`) streaming for smooth scrubbing.
  - **Audio Player**: Clean playback controls and playlist support.
  - **PDF Viewer**: Document viewer with page navigation and zoom controls.
  - **Office Documents**: Inline rendering of `.docx`, `.xlsx`, and `.pptx` documents.
  - **Code & Markdown**: Syntax highlighting for dozens of programming languages and rich Markdown previews.
  - **Image Viewer**: High-resolution viewer with pan, zoom, and thumbnail caching.

#### 7. Companion Device Storage Integration
- **Direct Phone Storage Access**: Browse your companion mobile device storage (`/storage/emulated/0`) directly from the WebUI Files app under `/DATA/Companion/<Device_Name>`.
- **Inline Streaming & Previews**: Stream phone videos, listen to music, view high-res photos, and inspect PDFs stored on your mobile phone directly in your desktop browser without downloading the full files first.
- **Two-Way File Transfers**: Download files from your server to your phone or upload documents from your browser directly into phone storage.
- **Telemetry & Monitoring**: Companion Devices section in Settings reports live phone battery level, charging state, available phone storage, Wi-Fi SSID, and connection status.

#### 8. System Telemetry & Monitoring
- **Desktop Hardware Widgets**: Live telemetry meters for CPU usage (per-core breakdowns), memory consumption, disk I/O, and network throughput.
- **NVIDIA GPU Monitor**: Dedicated widget displaying GPU core utilization, VRAM usage, temperature, power draw, and active GPU processes.
- **In-Browser Terminal**: Fully featured SSH terminal console running in a movable desktop window.
- **System Package Updater**: Integrated updates panel for upgrading underlying Debian/Ubuntu packages with a single click.

---

### 📱 Mobile Companion App (`nivaroos_mobile`)

#### 1. Instant Discovery & Dashboard
- **mDNS Network Auto-Discovery**: Automatically discovers NivaroOS servers on your local Wi-Fi network without manually typing IP addresses.
- **Secure Token Authentication**: Seamless JWT authentication with auto-refresh and secure persistent storage.
- **Real-Time Server Metrics**: Live dashboard displaying server CPU load, RAM allocation, storage pool capacity, temperature, and uptime.

#### 2. Unified Mobile File Manager
- **Cross-Storage Browsing**: Seamlessly browse server storage drives, internal pools, external USB drives, and local phone storage (`/storage/emulated/0`) within one cohesive app.
- **In-App Media Viewers**:
  - Fullscreen video player with swipe gesture controls for brightness, volume, and playback speed.
  - Audio player with scrubber and background playback.
  - PDF reader with smooth zooming and page navigation.
  - Markdown viewer and source code syntax viewer.
- **Fast File Operations**: Rename, move, delete, upload to server, and download to phone.

#### 3. Embedded Companion Storage Server & Reverse Tunnel
- **Local HTTP & WebSocket Server**: Built-in HTTP server running on port 8765 exposing whole phone storage to NivaroOS.
- **HTTP Range Streaming**: Supports `Range: bytes=...` headers (`206 Partial Content`) for instant video scrubbing and document streaming.
- **Persistent Reverse Tunnel**: Connects a persistent WebSocket tunnel back to the NivaroOS core server, allowing remote file access and streaming even behind NAT or without port forwarding.
- **Unattended Standby Background Service**: Persistent Android foreground service with WakeLock to keep storage accessible 24/7 even when the screen is off or device is in deep standby.

#### 4. Remote VM Console & Trackpad
- **Remote KVM Display**: Connect to virtual machines running on your NivaroOS server with a fast RFB/VNC client.
- **Intuitive Trackpad Mode**: Control the remote mouse cursor using your phone's screen as a precision trackpad with tap-to-click, left/right mouse buttons, and two-finger scrolling.
- **Virtual On-Screen Keyboard**: Full input support including modifier keys (`Ctrl`, `Alt`, `Shift`, `Esc`, `Tab`, `Del`, `Super/Win`) and Function keys (`F1`–`F12`).

#### 5. Mobile App Store & Docker Management
- **Catalog Browsing**: Browse the 400+ NivaroOS app store directly on mobile.
- **Container Lifecycle Controls**: Start, stop, restart, pause, and inspect running Docker containers from anywhere.

#### 6. Speed & Link Diagnostics
- **LAN Link Benchmark**: Measures real-time direct transfer speed between your mobile phone and the NivaroOS server.
- **Internet Speed Test**: Benchmarks your server's connection to the internet (download, upload, and latency) with smooth animated speedometer gauges.

---

## 🏗️ Architecture & Services

NivaroOS is built on a modular microservices architecture communicating over an event-driven message bus and unified gateway:

```
┌─────────────────────────────────────────────────────────────┐
│                          NivaroOS UI                          │
│         (Vue 2.7 / Vue CLI / Buefy / MDI / Webpack 5)       │
└──────────────────────────────┬──────────────────────────────┘
                               │ HTTP / WebSocket (:80)
┌──────────────────────────────▼──────────────────────────────┐
│                    NivaroOS Gateway Proxy                     │
│                  (services/gateway - Go)                    │
└──────┬──────────┬──────────┬──────────┬──────────┬──────┬───┘
       │          │          │          │          │      │
┌──────▼──┐ ┌─────▼───┐ ┌────▼───┐ ┌────▼───┐ ┌────▼──┐ ┌─▼──────┐
│  Core   │ │  User   │ │  App   │ │ Local  │ │  VM   │ │  GPU   │
│ Daemon  │ │ Service │ │  Mgmt  │ │Storage │ │Sidecar│ │Sidecar │
│ (v1/v2) │ │ (Auth)  │ │(Docker)│ │(Disks) │ │(KVM)  │ │(nvidia)│
└────┬────┘ └─────────┘ └─────────┘ └─────────┘ └──────┘ └────────┘
     │ (WS Tunnel / LAN Proxy)
┌────▼────────────────────────┐
│   Mobile Companion Device   │
│  (Flutter / HTTP & WS / BG) │
└─────────────────────────────┘
```

| Service | Directory | Description |
| :--- | :--- | :--- |
| **Gateway** | `services/gateway` | High-performance reverse proxy routing UI static assets, WebSocket tunnels, and API endpoints. |
| **Core Daemon** | `services/core` | System management daemon, companion device proxying, hardware monitoring, and notifications. |
| **User Service** | `services/user` | User authentication, JWT sessions, user profiles, and desktop preferences. |
| **App Management** | `services/app-management` | Docker container and Compose lifecycle orchestrator with 400+ app catalog indexer. |
| **Local Storage** | `services/local-storage` | Block storage detection, filesystem formatting, storage pool allocation, and mount management. |
| **Message Bus** | `services/message-bus` | Real-time event broker and WebSocket broadcasting daemon. |
| **VM Sidecar** | `services/vm-sidecar` | QEMU/KVM virtual machine provisioning and noVNC WebSocket bridge. |
| **GPU Sidecar** | `services/gpu-sidecar` | NVIDIA GPU metrics (utilization, memory, temperature, processes) for the desktop telemetry widget. |
| **CLI** | `cli/` | `nivaroos-cli` — standalone admin command-line tool for managing services and add-ons. |
| **Frontend UI** | `ui/` | Responsive windowed desktop Single Page Application. |
| **Mobile App** | `mobile/` | Native Flutter companion app for Android and iOS. |

---

## 🖥️ Command-Line Interface

Every installation includes `nivaroos-cli`, an administrative CLI independent of the web UI:

```bash
nivaroos-cli --help
```

Toggle optional add-ons like the VM Manager at any time:

```bash
nivaroos-cli vm enable    # builds and starts the VM Manager service
nivaroos-cli vm disable   # stops VM Manager (VM disks under /DATA/VMs remain safe)
```

---

## 🛠️ Building from Source

### Prerequisites
- **Go**: `1.23.4+`
- **Node.js**: `18+` or `20+`
- **pnpm**: `9+`
- **Flutter**: `3.22+` (for mobile app)
- **Docker Engine**: `20.10+` with `docker compose`

### 1. Build the Frontend UI
```bash
cd ui
pnpm install
pnpm run build
```
Compiled production assets are output to `ui/build/sysroot/var/lib/nivaroos/www/`.

### 2. Build Backend Go Services & CLI
```bash
# Core Daemon
cd services/core && go build -o /usr/local/bin/nivaroos .

# Gateway
cd services/gateway && go build -o /usr/local/bin/nivaroos-gateway .

# User Service
cd services/user && go build -o /usr/local/bin/nivaroos-user .

# App Management
cd services/app-management && go build -o /usr/local/bin/nivaroos-app-management .

# Local Storage
cd services/local-storage && go build -o /usr/local/bin/nivaroos-local-storage .

# Message Bus
cd services/message-bus && go build -o /usr/local/bin/nivaroos-message-bus .

# GPU Sidecar
cd services/gpu-sidecar && go build -o /usr/local/bin/nivaroos-gpu-sidecar .

# VM Sidecar (optional)
cd services/vm-sidecar && go build -o /usr/local/bin/nivaroos-vm-sidecar .

# CLI
cd cli && go build -o /usr/local/bin/nivaroos-cli .
```

### 3. Build the Mobile App
```bash
cd mobile
flutter pub get
flutter build apk --release
```
The output APK will be generated at `mobile/build/app/outputs/flutter-apk/app-release.apk`.

---

## 👥 Authors & Contributors

- **Ayush** ([@F-e-n-y-x](https://github.com/F-e-n-y-x)) — Project Creator
- **NivaroOS Community Contributors**

---

## 🙏 Acknowledgments

NivaroOS is a fork of [CasaOS](https://github.com/IceWhaleTech/CasaOS), originally created by [IceWhaleTech](https://github.com/IceWhaleTech). Thank you to the original CasaOS team and community for the foundation this project was built on.

---

## 📄 License

Distributed under the **Apache 2.0 License**. See [`LICENSE`](LICENSE) for more information.
