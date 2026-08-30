# Recasa

<p align="center">
  <strong>A modern, self-hosted personal cloud OS and application platform.</strong><br>
  Desktop-class multitasking, Docker container orchestration, KVM virtual machines, file management, and a rich ZimaOS-inspired App Store — all served in one unified web workspace.
</p>

---

## ✨ Key Features

### 🪟 Desktop Multitasking & Window Management
- **Native Windowed Experience**: Run multiple applications side-by-side with full window controls (minimize, maximize, drag, resize handles, and taskbar docking).
- **Session Persistence**: Restores open windows, sizes, and screen positions seamlessly across reloads.
- **Glassmorphic Theme**: Modern translucent blur effects, custom wallpaper styling, and desktop background customization synchronized with the login screen.

### 🛍️ ZimaOS-Inspired Windowed App Store
- **Modern Desktop App Store**: Runs inside a dedicated resizable window with responsive breakpoints.
- **Discover & Curated Collections**:
  - Hero spotlight carousel with animated app highlights and preview cards.
  - Curated category rows for **Media & Entertainment**, **AI & Next-Gen LLMs**, **Developer & DevOps**, **Networking & Privacy**, and **Productivity & Cloud**.
- **Screenshot Previews & Lightbox**: 16:9 high-resolution card banners and an interactive in-window screenshot lightbox.
- **In-Window Detail Drawer**: Specifications, minimum RAM requirements, mapped ports, volumes, and architecture compatibility (`amd64`, `arm64`).
- **Custom Compose Deployer**: Integrated Docker Compose YAML editor for 1-click deployments of custom container stacks.
- **Store Sources Manager**: Add community repositories and custom app store sources.

### 🖥️ Virtual Machine Manager
- **KVM / QEMU Virtualization**: Create, edit, and run full Linux/Windows virtual machines.
- **Built-in noVNC Console**: Integrated web VNC console for direct graphical and terminal access to VMs in windowed or standalone view.
- **Resource Allocation**: Configure vCPUs, RAM, virtual disks, network bridges, and boot ISOs.

### 📁 Files & Storage Hub
- **File Management**: Tree navigation, drag-and-drop uploads, batch downloads, search, and granular permissions.
- **Built-in File Viewers**: Preview images, videos, audio, code files with syntax highlighting, PDFs, and Office documents (`.docx`, `.xlsx`).
- **Storage Pools & Network Shares**: Manage local disks, storage pools, and mount external SMB/NFS storage.

### ⚙️ System Settings & User Management
- **Appearance Customization**: Custom wallpaper uploads, login screen background sync, glassmorphism blur intensity, and dock customization.
- **System Metrics**: Real-time CPU, RAM, disk, and network I/O dashboards.
- **Terminal & Logs**: In-browser interactive SSH/terminal and live container logging.

---

## 🏗️ Architecture & Services

Recasa is structured into modular, decoupled backend microservices communicating over a message bus and unified gateway:

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
| **Gateway** | `services/gateway` | High-performance reverse proxy routing UI static assets and API requests. |
| **Core** | `services/core` | Core platform daemon, hardware monitoring, notifications, and legacy API handlers. |
| **User Service** | `services/user` | Authentication, JWT sessions, user profiles, and public wallpaper endpoints. |
| **App Management** | `services/app-management` | Docker container and Docker Compose lifecycle, 400+ app store catalog indexing. |
| **Local Storage** | `services/local-storage` | Disk detection, partition formatting, storage pool allocation, and mount management. |
| **Message Bus** | `services/message-bus` | Real-time event broadcasting and WebSocket messaging bus. |
| **VM Sidecar** | `services/vm-sidecar` | QEMU/KVM virtual machine provisioning and noVNC WebSocket proxy. |
| **Frontend UI** | `ui/` | Desktop-class responsive Vue.js Single Page Application. |

---

## 🚀 Building & Developing

### Prerequisites
- **Go**: `1.21+`
- **Node.js**: `18+` or `20+`
- **pnpm**: `8+` or `9+`
- **Docker Engine**: `20.10+` and `docker compose`

### 1. Build Frontend UI
```bash
cd ui
pnpm install
pnpm run build
```
The compiled assets will be placed into `ui/build/sysroot/var/lib/recasa/www/`.

### 2. Build Backend Services
```bash
# Core
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

### 3. Run Tests
```bash
# Frontend Unit Tests (Vitest)
cd ui && pnpm vitest run

# Backend Go Services Unit Tests
cd services/app-management && go test ./...
cd services/core && go test ./...
cd services/common && go test ./...
```

---

## 📄 License

This project is licensed under the Apache License 2.0.
