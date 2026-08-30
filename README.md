<p align="center">
  <img src="ui/src/assets/img/logo/casa-white.svg" width="96" height="96" alt="Recasa Logo" style="filter: drop-shadow(0 4px 12px rgba(37, 99, 235, 0.4));">
</p>

<h1 align="center">Recasa</h1>

<p align="center">
  <strong>A modern, self-hosted personal cloud OS and container platform.</strong><br>
  Desktop-class multitasking, Docker container studio, KVM virtual machines, file management, and a rich ZimaOS-inspired App Store — all in one unified, windowed web workspace.
</p>

<p align="center">
  <a href="https://github.com/F-e-n-y-x/recasa/releases"><img src="https://img.shields.io/github/v/release/F-e-n-y-x/recasa?color=2563eb&style=flat-square" alt="Release"></a>
  <a href="https://github.com/F-e-n-y-x/recasa/blob/master/LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-emerald.svg?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Platform-Debian%20%7C%20Ubuntu-orange?style=flat-square" alt="Platform">
  <img src="https://img.shields.io/badge/Architecture-amd64%20%7C%20arm64-blueviolet?style=flat-square" alt="Architecture">
</p>

---

## ⚡ Quick Install

Install Recasa on any clean **Debian 12+** or **Ubuntu 22.04+** system with a single command:

```bash
curl -fsSL https://raw.githubusercontent.com/F-e-n-y-x/recasa/master/installer/install.sh | sudo bash
```

### Automated / Unattended Installation

```bash
# Install minimal stack (without KVM Virtual Machine Manager)
curl -fsSL https://raw.githubusercontent.com/F-e-n-y-x/recasa/master/installer/install.sh | sudo bash -s -- --without-vm --yes

# Install full stack including KVM Virtual Machine Manager & noVNC
curl -fsSL https://raw.githubusercontent.com/F-e-n-y-x/recasa/master/installer/install.sh | sudo bash -s -- --with-vm --yes
```

#### Installer Flags & Options

| Flag | Description |
| :--- | :--- |
| `--with-vm` | Automatically installs QEMU/KVM packages and enables the VM Manager sidecar. |
| `--without-vm` | Skips QEMU/KVM packages for a lighter installation. |
| `-y`, `--yes` | Unattended installation (accepts all defaults without interactive prompts). |

Once installation finishes, open your browser and navigate to `http://<your-server-ip>` to access your desktop!

---

## ✨ Key Features

### 🪟 Desktop Multitasking & Window Management
- **Native Windowed Experience**: Run multiple applications side-by-side with complete window controls (minimize, maximize, drag, resize handles, and dock pinning).
- **Session Persistence**: Restores open windows, sizes, and desktop grid coordinates automatically across page reloads.
- **Glassmorphism & Personalization**: Modern frosted-glass themes, customizable backdrop blur intensity, custom wallpaper uploads, and login screen wallpaper synchronization.

### 🛍️ ZimaOS-Inspired Windowed App Store
- **Windowed Catalog Browser**: Responsive grid with categories, instant search over 400+ self-hosted applications, and source repository managers.
- **Discover & Spotlight Carousel**:
  - Hero carousel with animated app highlights, tags, and quick-action triggers.
  - Curated rows for **Media & Streaming**, **AI & LLMs**, **Developer & DevOps Tools**, and **Networking**.
- **16:9 Screenshots & Lightbox**: High-resolution screenshot galleries with interactive full-screen lightbox previews.
- **In-Window Detail Drawer**: In-depth app overview, memory requirements, developer info, architecture compatibility (`amd64`, `arm64`), and port specifications.

### 🐳 Container Studio & Bidirectional YAML Sync
- **Dedicated Container Studio**: Replaces legacy dialogs with a modern card-based container configuration workspace.
- **Card-Based Visual Editor**:
  - General & Image configuration with quick tag selectors (`:latest`, `:alpine`, `:stable`).
  - Web UI portal routing and scheme configuration (`http://` / `https://`).
  - Dynamic Port Mappings (Host ➔ Container, TCP/UDP).
  - Storage & Volume Mounts (Host Path ➔ Container Path, `rw`/`ro` modes).
  - Environment Variables (Monospace key-value pairs).
  - Advanced Options: Network drivers (`bridge`, `host`, custom networks), restart policies, memory limits, root privileged container toggles, and hardware device passthrough (`/dev/dri`).
- **Live Two-Way Compose YAML Sync**: Real-time bidirectional updates between the visual form and dark monospace Compose code editor.
- **Import & Export**: Convert `docker run` commands or drag-and-drop `.yaml` compose files directly into the studio, or export `.yaml` files with one click.

### 🚀 Real-time Download & Install Experience
- **Floating Global Install HUD**: Sleek glassmorphic download progress card floating above the desktop dock showing live pulling/extracting stages, progress bars, and success badges.
- **Desktop Grid Live Downloading Tile**: New container apps appear immediately on your desktop grid while downloading with animated SVG circular progress rings.
- **In-Store Live Progress**: Quick install buttons throughout the catalog show real-time percentage progress.

### 🖥️ KVM Virtual Machine Manager
- **QEMU / KVM Virtualization**: Create, configure, and manage full Linux and Windows virtual machines.
- **Integrated noVNC Console**: Web-based graphical and terminal access to VMs directly within a movable desktop window or standalone mode.
- **Hardware Sizing**: Adjust vCPUs, RAM, virtual disks, ISO boot media, and network interfaces.

### 📁 Files & Storage Hub
- **Modern File Browser**: Fast tree navigation, drag-and-drop transfers, batch operations, breadcrumbs, and search.
- **Built-in File Previews**: View images, video streaming, audio, markdown, syntax-highlighted source code, PDFs, and Office documents (`.docx`, `.xlsx`).
- **Drive & USB Notifications**: Native snackbar alerts for newly attached storage devices with one-click navigation to Files.

### ⚙️ System Metrics & Monitoring
- **Desktop Telemetry Widgets**: Translucent real-time CPU (per-core), RAM, GPU, Disk I/O, and Network traffic monitors.
- **In-Browser Terminal**: Full interactive SSH / terminal console in a resizable window.

---

## 🏗️ Architecture & Services

Recasa is engineered as a modular microservices architecture communicating via a unified message bus and reverse-proxy gateway:

```
┌─────────────────────────────────────────────────────────────┐
│                          Recasa UI                          │
│         (Vue 2.7 / Vue CLI / Buefy / MDI / Webpack 5)       │
└──────────────────────────────┬──────────────────────────────┘
                               │ HTTP / WebSocket (:80)
┌──────────────────────────────▼──────────────────────────────┐
│                    Recasa Gateway Proxy                     │
│                  (services/gateway - Go)                    │
└──────┬──────────┬──────────┬──────────┬──────────┬──────────┘
       │          │          │          │          │
┌──────▼──┐ ┌─────▼───┐ ┌────▼───┐ ┌────▼───┐ ┌────▼──────┐
│  Core   │ │  User   │ │  App   │ │ Local  │ │    VM     │
│ Daemon  │ │ Service │ │  Mgmt  │ │Storage │ │  Sidecar  │
│ (v1/v2) │ │ (Auth)  │ │(Docker)│ │(Disks) │ │ (KVM/VNC) │
└─────────┘ └─────────┘ └─────────┘ └─────────┘ └──────────┘
       ▲          ▲          ▲          ▲
       └──────────┴─────┬────┴──────────┘
                        │ Message Bus (Socket.IO)
               ┌────────▼────────┐
               │   Message Bus   │
               │ (services/msg)  │
               └─────────────────┘
```

| Service | Directory | Description |
| :--- | :--- | :--- |
| **Gateway** | `services/gateway` | High-performance reverse proxy routing UI static assets and API endpoints. |
| **Core Daemon** | `services/core` | System management daemon, hardware monitoring, notifications, and legacy API handlers. |
| **User Service** | `services/user` | User authentication, JWT sessions, profiles, and wallpaper preferences. |
| **App Management** | `services/app-management` | Docker container and Compose lifecycle orchestrator with 400+ app catalog indexer. |
| **Local Storage** | `services/local-storage` | Block storage detection, filesystem formatting, storage pool allocation, and mount management. |
| **Message Bus** | `services/message-bus` | Real-time event broker and WebSocket broadcasting daemon. |
| **VM Sidecar** | `services/vm-sidecar` | QEMU/KVM virtual machine provisioning and noVNC WebSocket bridge. |
| **Frontend UI** | `ui/` | Responsive windowed desktop Single Page Application. |

---

## 🛠️ Development & Building from Source

### Prerequisites
- **Go**: `1.21+` (or `1.23+`)
- **Node.js**: `18+` or `20+`
- **pnpm**: `9+`
- **Docker Engine**: `20.10+` with `docker compose`

### 1. Build the Frontend UI
```bash
cd ui
pnpm install
pnpm run build
```
Compiled production assets are output to `ui/build/sysroot/var/lib/recasa/www/`.

### 2. Build Backend Go Services
```bash
# Core Daemon
cd services/core && go build -o /usr/local/bin/recasa-core cmd/main.go

# Gateway
cd services/gateway && go build -o /usr/local/bin/recasa-gateway cmd/main.go

# User Service
cd services/user && go build -o /usr/local/bin/recasa-user cmd/main.go

# App Management
cd services/app-management && go build -o /usr/local/bin/recasa-app-management cmd/main.go

# Local Storage
cd services/local-storage && go build -o /usr/local/bin/recasa-local-storage cmd/main.go

# Message Bus
cd services/message-bus && go build -o /usr/local/bin/recasa-message-bus cmd/main.go

# VM Sidecar
cd services/vm-sidecar && go build -o /usr/local/bin/recasa-vm-sidecar cmd/main.go
```

### 3. Run Test Suites
```bash
# Frontend Unit Tests (Vitest)
cd ui && pnpm vitest run

# Backend Go Services Unit Tests
cd services/app-management && go test ./...
cd services/core && go test ./...
cd services/common && go test ./...
```

---

## 🤝 Contributing

Contributions, issues, and feature requests are welcome!
1. Fork the project repository.
2. Create your feature branch (`git checkout -b feature/AmazingFeature`).
3. Commit your changes (`git commit -m 'feat: add amazing feature'`).
4. Push to the branch (`git push origin feature/AmazingFeature`).
5. Open a Pull Request.

---

## 📄 License

Distributed under the **Apache 2.0 License**. See [`LICENSE`](LICENSE) for more information.
