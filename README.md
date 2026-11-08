# myflvx（FLVX 增强版）

基于 [FLVX](https://github.com/Sagit-chu/flvx) `2.2.0-alpha4` 界面的增强版：保留 alpha4 的 UI，后端与节点端同步上游最新版本的修复，并加入「订阅」与「部署节点」功能。

## 和原版的区别

- **界面**：沿用 `2.2.0-alpha4` 的界面（不使用之后的液态玻璃新界面）。
- **修复**：`go-backend`、`go-gost`（节点端）同步到上游最新代码（3.0.1-rc7 之后），包含 UDP/KCP、流量统计、运行时恢复、认证与 WebSocket 加固、SSRF、依赖漏洞等修复；与新界面无关的前端修复也已移植。
- **不包含**：上游的商业授权（keygen 校验、品牌设置付费锁定），品牌设置与 alpha4 一样免费可用。
- **新增：代理节点**（所有用户可用，各自独立）：手动添加、粘贴分享链接、导入机场订阅（链接 / base64 / Clash YAML / 订阅地址）、标签 / 来源 / 隧道筛选、批量操作、复制分享链接。每个用户只能看到和使用自己的节点。
- **新增：隧道中转**：在节点上选择隧道，面板以当前用户身份自动创建转发（目标为节点地址），并按转发入口生成中转节点；普通用户只能选择自己有权限的隧道，转发计入自己的配额。转发端口或源节点变更后中转节点自动跟随；也可以关联自己已有的转发。
- **新增：订阅管理 / 生成订阅**（所有用户可用，各自独立）：上传 Clash/mihomo 配置、从订阅地址导入，或用自己的节点生成订阅（内置极简 / 常用分流 / 全局模板，可视化代理组编辑与自定义规则）。订阅只属于创建者，不能分配或共享。
- **新增：订阅下载**：在面板内（携带登录凭据）下载自己的 Clash/mihomo YAML、Base64 文件或复制分享链接，**不生成公网订阅链接**，配置不会暴露在公网。
- **新增：部署节点**（仅管理员）：选择一台或多台 FLVX 节点，由面板通过节点端执行 [mihomo_script](https://github.com/mozziexwz/mihomo_script)，一键部署 **Shadowsocks 2022 / mieru / Sudoku**，不需要登录服务器。端口、密码、密钥由面板生成；可查看实时日志、刷新状态、卸载，部署参数可复制到其他节点；完成后自动生成代理节点。需要节点端为 myflvx 2.2.2 或更高版本（可在「节点」页面升级），服务器需为带 systemd 的 Debian / Ubuntu / RHEL 系。

## 备份恢复兼容

- **原版 → 增强版**：原版（2.2.0-alpha4 ~ 3.x）导出的备份可以直接导入。
  - alpha4 及更早版本的备份里密码是 MD5：上游新版导入时会把这些用户禁用并重置密码，增强版改为保留原密码，用户首次登录时自动升级为 bcrypt。
- **增强版 → 原版**：导出时选择格式：
  - **兼容原版 FLVX 3.x**：结构与上游一致，不含增强版数据；
  - **兼容原版 FLVX 2.2.0-alpha4**：同时去掉 alpha4 不认识的字段（alpha4 的导入会拒绝未知字段）。
- 增强版数据（代理节点、订阅、部署记录）是单独的备份类型，只有勾选时才会写入备份文件；备份里保留每条数据的所属用户，2.2.1 的备份（没有所属用户）导入后归第一个管理员。

## 部署流程
---
### Docker Compose部署
#### 快速部署（安装最新版）
面板端：
```bash
curl -L https://raw.githubusercontent.com/mozziexwz/myflvx/main/panel_install.sh -o panel_install.sh && chmod +x panel_install.sh && ./panel_install.sh
```
节点端：
```bash
curl -L https://raw.githubusercontent.com/mozziexwz/myflvx/main/install.sh -o install.sh && chmod +x install.sh && ./install.sh
```

> 镜像与节点端二进制由本仓库的 GitHub Actions 在推送版本 tag（如 `2.2.1`）时构建，发布到 `ghcr.io/mozziexwz/flux-panel-backend`、`ghcr.io/mozziexwz/vite-frontend` 和本仓库的 Releases。首次使用前需要先发布一个版本，并在 GitHub 的 Packages 设置中把镜像设为 Public（或在服务器上 `docker login ghcr.io`）。

#### 安装特定版本
从 [Releases](https://github.com/mozziexwz/myflvx/releases) 页面复制对应版本的安装命令，脚本会自动安装该版本而非最新版。

面板端（以 2.2.1 为例）：
```bash
curl -L https://github.com/mozziexwz/myflvx/releases/download/2.2.1/panel_install.sh -o panel_install.sh && chmod +x panel_install.sh && ./panel_install.sh
```
节点端（以 2.2.1 为例）：
```bash
curl -L https://github.com/mozziexwz/myflvx/releases/download/2.2.1/install.sh -o install.sh && chmod +x install.sh && ./install.sh
```

#### PostgreSQL 部署（Docker Compose）

安装脚本会根据环境自动下载对应的 Compose 配置并保存为 `docker-compose.yml`。默认仍使用 SQLite，切换到 PostgreSQL 只需要配置环境变量。

1) 在 `docker-compose` 同目录创建或修改 `.env`：

```bash
JWT_SECRET=replace_with_your_secret
BACKEND_PORT=6365
FRONTEND_PORT=6366

DB_TYPE=postgres
DATABASE_URL=postgres://flux_panel:replace_with_strong_password@postgres:5432/flux_panel?sslmode=disable

POSTGRES_DB=flux_panel
POSTGRES_USER=flux_panel
POSTGRES_PASSWORD=replace_with_strong_password
```

> 📌 使用安装脚本部署时，`POSTGRES_PASSWORD` 会自动随机生成并写入 `.env`。

2) 启动服务：

```bash
docker compose up -d
```

3) 如果你想继续使用 SQLite，保留 `DB_TYPE=sqlite`（或不设置 `DB_TYPE`）即可。

#### 从 SQLite 迁移到 PostgreSQL

如果你是通过 `panel_install.sh` 安装面板，推荐直接使用脚本菜单一键迁移：

```bash
./panel_install.sh
# 选择 4. 迁移到 PostgreSQL
```

脚本会自动完成 SQLite 备份、PostgreSQL 启动、`pgloader` 导入、`.env` 中 `DB_TYPE`/`DATABASE_URL` 更新，并重启服务。

如果你希望手动迁移，以下示例基于 Docker Volume `sqlite_data`（项目默认配置）与 `pgloader`：

1) 停止服务并备份 SQLite 数据：

```bash
docker compose down
docker run --rm -v sqlite_data:/data -v "$(pwd)":/backup alpine sh -c "cp /data/gost.db /backup/gost.db.bak"
```

2) 仅启动 PostgreSQL：

```bash
docker compose up -d postgres
```

3) 使用 `pgloader` 迁移：

```bash
source .env
docker run --rm --network gost-network -v sqlite_data:/sqlite dimitri/pgloader:latest pgloader /sqlite/gost.db "postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}"
```

4) 切换后端到 PostgreSQL 并启动：

```bash
source .env
export DB_TYPE=postgres
export DATABASE_URL="postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable"
docker compose up -d
```

5) 迁移完成后，登录面板检查用户、隧道、转发、节点数据是否正确。

#### 默认管理员账号

- **账号**: admin_user
- **密码**: admin_user

> ⚠️ 首次登录后请立即修改默认密码！

---
## 致谢与第三方组件

- [FLVX](https://github.com/Sagit-chu/flvx)（GPLv3）：本项目的上游，面板、后端与节点端均来自 FLVX。
- [妙妙屋 miaomiaowu](https://github.com/iluobei/miaomiaowu)（MIT）：节点管理、订阅管理、生成订阅、订阅链接的功能设计；分享链接解析使用其开源模块 [proxyparser](https://github.com/MMWOrg/mmwX-plugins)（MIT）。
- [mihomo_script](https://github.com/mozziexwz/mihomo_script)：部署节点使用的一键脚本（Shadowsocks 2022 / mieru / Sudoku）。
- [mihomo](https://github.com/MetaCubeX/mihomo)：节点服务端与客户端内核。

本项目沿用 FLVX 的许可证（GPLv3，原 flux-panel 部分为 Apache 2.0），见 `LICENSE`、`LICENSE-APACHE` 与 `NOTICE`。

## Original Project
- **Name**: flux-panel
- **Source**: https://github.com/bqlpfy/flux-panel
- **License**: Apache License 2.0

## Modifications
This fork (FLVX) is no longer a light patch on top of the upstream project. It has been deeply reworked, with both backend and frontend rebuilt around a Go-based architecture.

### 1. Backend (Rewritten)
- **Removed**: The original `springboot-backend/` (Java/Spring Boot) implementation.
- **Added**: A fully rewritten `go-backend/` service (Go), including updated data and API handling for panel management.

### 2. Frontend (Reworked)
- **Reworked**: `vite-frontend/` has been substantially rebuilt to match the new backend contract and current UI layer architecture.
- **Updated**: Dashboard pages/components and interaction flows for the current React/Vite stack.

### 3. Forwarding Stack (Modified)
- **Modified**: `go-gost/` forwarding agent wrapper.
- **Modified**: `go-gost/x/` local fork of `github.com/go-gost/x`.

### 4. Mobile Clients (Removed)
- **Removed**: `android-app/` source code.
- **Removed**: `ios-app/` source code.

### 5. Deployment & Project Infrastructure
- **Updated**: Docker deployment templates and installer output flow (IPv4/IPv6 compose variants).
- **Updated**: Release installation scripts (`install.sh`, `panel_install.sh`) and supporting automation.
- **Added/Updated**: Project-level engineering documentation (for example `AGENTS.md`).

---


## 免责声明

本项目仅供个人学习与研究使用，基于开源项目进行二次开发。  

使用本项目所带来的任何风险均由使用者自行承担，包括但不限于：  

- 配置不当或使用错误导致的服务异常或不可用；  
- 使用本项目引发的网络攻击、封禁、滥用等行为；  
- 服务器因使用本项目被入侵、渗透、滥用导致的数据泄露、资源消耗或损失；  
- 因违反当地法律法规所产生的任何法律责任。  

本项目为开源的流量转发工具，仅限合法、合规用途。  
使用者必须确保其使用行为符合所在国家或地区的法律法规。  

**作者不对因使用本项目导致的任何法律责任、经济损失或其他后果承担责任。**  
**禁止将本项目用于任何违法或未经授权的行为，包括但不限于网络攻击、数据窃取、非法访问等。**  

如不同意上述条款，请立即停止使用本项目。  

作者对因使用本项目所造成的任何直接或间接损失概不负责，亦不提供任何形式的担保、承诺或技术支持。  


请务必在合法、合规、安全的前提下使用本项目。

---
## ⭐ 支持原作者（FLVX）：喝杯咖啡！（USDT）

| 网络       | 地址                                                                 |
|------------|----------------------------------------------------------------------|
| BNB(BEP20) | `0xa608708fdc6279a2433fd4b82f0b72b8cbe97ed5`                          |
| TRC20      | `TM8VYdU3s3gSX5PC8swjAJrAzZFCHKqG2k`                                  |
| Aptos      | `0x49427bfcba1006a346447430689b2307ac156316bb34850d1d3029ff9d118da5`  |
| polygon    |  `0xa608708fdc6279a2433fd4b82f0b72b8cbe97ed5`    |
