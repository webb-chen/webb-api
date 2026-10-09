#!/usr/bin/env bash
#
# webb-api 一键部署脚本（多发行版通用版）
#
# 支持: RHEL / Rocky / AlmaLinux / CentOS Stream / Fedora / Ubuntu / Debian /
#       openSUSE / Arch / Manjaro / Kali / Linux Mint / Zorin / Deepin /
#       UOS / 银河麒麟 / 龙蜥 Anolis / openEuler / Alpine (容器内)
#
# 用法:
#   1. 把压缩包上传到服务器并解压
#   2. sudo bash deploy.sh
#
set -eu

# 注意：这里不用 pipefail。
# generate_password 用 `tr ... | head -c 32`，head 读够就退出会让 tr 收到 SIGPIPE，
# pipefail 会把整条管道判定失败 + set -e 直接让脚本静默退出。
# 用 set -eu（不带 pipefail）规避该陷阱。

# ─────────────────────────────────────────────
# 可配置变量（按需修改）
# ─────────────────────────────────────────────
INSTALL_DIR="/opt/webb-api"
WEBB_PORT="3000"
DB_PORT="5433"
REDIS_PORT="6380"
TZ_VALUE="Asia/Shanghai"

# ─────────────────────────────────────────────
# 颜色 & 样式
# ─────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

HAS_COLOR=1
[[ -t 1 ]] || HAS_COLOR=0
color() { if [[ $HAS_COLOR -eq 1 ]]; then printf "%b" "$1"; fi; }

# ─────────────────────────────────────────────
# 启动画面
# ─────────────────────────────────────────────
print_banner() {
  local w=50
  echo ""
  echo -e "${BOLD}${CYAN}"
  echo "  +================================================+  "
  echo "  |                                                |  "
  echo "  |   [##] [##] [ ]  [##] [##] [##]   [##]      |  "
  echo "  |  [ ]  [##] [ ] [ ]   [ ] [ ]  [##] [ ]       |  "
  echo "  |  [##] [##] [##] [##] [##] [##] [##] [##]     |  "
  echo "  |                                                |  "
  echo "  |            W E B B - A P I                    |  "
  echo "  |          One-Click Deploy Script              |  "
  echo "  |     Docker + PostgreSQL + Redis               |  "
  echo "  |                                                |  "
  echo "  |  Multi-Distro Compatible                     |  "
  echo "  |  RHEL · Rocky · AlmaLinux · Fedora           |  "
  echo "  |  Ubuntu · Debian · openSUSE · Arch           |  "
  echo "  |  openEuler · Anolis · 麒麟 · UOS             |  "
  echo "  +================================================+  "
  echo -e "${NC}"
}

# ─────────────────────────────────────────────
# 日志函数
# ─────────────────────────────────────────────
log()    { echo -e "${GREEN}[OK]${NC} $*"; }
warn()   { echo -e "${YELLOW}[!!]${NC} $*"; }
err()    { echo -e "${RED}[ERR]${NC} $*"; }
step()   { echo -e "\n${BOLD}${BLUE}━━━  $(color "${CYAN}$*")${NC}  ${DIM}────────────────────────────────────${NC}"; }

# ─────────────────────────────────────────────
# 进度条函数
# ─────────────────────────────────────────────
# progress_step "步骤描述" 总步数 当前步数
# 输出带彩色条的进度行
progress_step() {
  local label="$1" total="$2" done="$3"
  local bar_width=30
  local filled=$(( done * bar_width / total ))
  local empty=$(( bar_width - filled ))
  local bar=""
  for ((i=0;i<filled;i++)); do bar+="█"; done
  for ((i=0;i<empty;i++)); do bar+="░"; done
  local pct=$(( done * 100 / total ))
  echo -ne "  ${BOLD}[${CYAN}#${done}/${total}${NC}]${BOLD} "
  echo -ne "${GREEN}${bar}${NC} "
  echo -e "${DIM}${label}${NC}  ${YELLOW}${pct}%${NC}"
}

# 总进度追踪
TOTAL_STEPS=6
CURRENT_STEP=0
next_step() {
  CURRENT_STEP=$((CURRENT_STEP + 1))
}

# ─────────────────────────────────────────────
# 1. root 检查
# ─────────────────────────────────────────────
check_root() {
  if [[ $EUID -ne 0 ]]; then
    err "请以 root 或 sudo 运行: sudo bash $(basename "$0")"
    exit 1
  fi
}

# ─────────────────────────────────────────────
# 2. 识别系统 & 包管理器
# ─────────────────────────────────────────────
PKG_MGR=""
DISTRO="Unknown"

detect_system() {
  # 优先读 /etc/os-release（所有现代发行版都有）
  if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    source /etc/os-release
    DISTRO="${PRETTY_NAME:-${ID:-unknown}}"
  elif [[ -f /etc/redhat-release ]]; then
    DISTRO="$(cat /etc/redhat-release)"
  elif [[ -f /etc/alpine-release ]]; then
    DISTRO="Alpine Linux $(cat /etc/alpine-release)"
  elif [[ -f /etc/debian_version ]]; then
    DISTRO="Debian $(cat /etc/debian_version)"
  fi

  # 检测包管理器
  if command -v dnf &>/dev/null; then
    PKG_MGR="dnf"
  elif command -v yum &>/dev/null; then
    PKG_MGR="yum"
  elif command -v apt-get &>/dev/null; then
    PKG_MGR="apt"
  elif command -v pacman &>/dev/null; then
    PKG_MGR="pacman"
  elif command -v zypper &>/dev/null; then
    PKG_MGR="zypper"
  elif command -v apk &>/dev/null; then
    PKG_MGR="apk"
  elif command -v microdnf &>/dev/null; then
    PKG_MGR="microdnf"
  fi

  # 识别特定发行版（用于 Docker 安装源）
  if [[ -n ${ID:-} ]]; then
    case "$ID" in
      centos|rhel|rocky|almalinux|anolis|opensuse*|fedora|euler*|kylin*|uos*|neokylin*)
        PKG_MGR="dnf" 2>/dev/null || PKG_MGR="yum"
        ;;
      arch|manjaro|endeavouros)
        PKG_MGR="pacman"
        ;;
      *opensuse*)
        PKG_MGR="zypper"
        ;;
      alpine)
        PKG_MGR="apk"
        ;;
      debian|ubuntu|linuxmint|zorin|deepin|kali|tails)
        # tails 基于 Debian，Kali 基于 Debian，Linux Mint/Zorin/Deepin 基于 Ubuntu
        PKG_MGR="apt"
        ;;
    esac
  fi
}

# ─────────────────────────────────────────────
# 3. 安装 Docker（按发行版）
# ─────────────────────────────────────────────
install_docker() {
  # 已有 Docker 就跳过
  if command -v docker &>/dev/null && docker --version &>/dev/null; then
    log "Docker 已安装: $(docker --version)"
    return
  fi

  case "$PKG_MGR" in
    dnf|yum)
      # RHEL 9 / Rocky 9 / Alma / Fedora / 龙蜥 / openEuler / 麒麟 / UOS
      if ! yum list dnf-utils &>/dev/null 2>&1; then
        yum install -y dnf-utils
      fi
      yum config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo 2>/dev/null || \
      yum config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
      yum install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
      ;;
    apt)
      # Debian / Ubuntu / Kali / Linux Mint / Zorin / Deepin
      apt-get update -qq
      apt-get install -y -qq ca-certificates curl
      # 安装 Docker GPG 公钥（新系统用 keyrings，旧系统回退 apt-key）
      local gpg_key="/etc/apt/keyrings/docker.gpg"
      if [[ -d /etc/apt/keyrings ]] || mkdir -p /etc/apt/keyrings 2>/dev/null; then
        curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o "$gpg_key"
      else
        curl -fsSL https://download.docker.com/linux/ubuntu/gpg | apt-key add -
      fi
      # 获取发行版代号
      local codename
      codename=$(lsb_release -cs 2>/dev/null || grep -oP '(?<=VERSION_CODENAME=)\S+' /etc/os-release 2>/dev/null || tr -d '"' < /etc/debian_version)
      # 写入 Docker 源（keyrings 方式 + apt-key 方式兜底）
      if [[ -f "$gpg_key" ]]; then
        echo "deb [arch=$(dpkg --print-architecture) signed-by=$gpg_key] https://download.docker.com/linux/${ID} ${codename} stable" \
          > /etc/apt/sources.list.d/docker.list
      else
        echo "deb https://download.docker.com/linux/${ID} ${codename} stable" \
          > /etc/apt/sources.list.d/docker.list
      fi
      apt-get update -qq
      apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
      ;;
    pacman)
      # Arch / Manjaro
      pacman -S --noconfirm docker docker-compose
      ;;
    zypper)
      # openSUSE
      zypper --non-interactive install docker docker-compose
      ;;
    apk)
      # Alpine
      apk add --no-cache docker docker-cli docker-compose
      ;;
    microdnf)
      # Fedora Atomic / UBI
      microdnf install -y dnf-utils
      yum config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
      microdnf install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
      ;;
    *)
      err "无法识别包管理器，请手动安装 Docker: https://docs.docker.com/engine/install/"
      exit 1
      ;;
  esac

  systemctl enable docker 2>/dev/null || true
  systemctl start docker 2>/dev/null || true

  if ! docker --version &>/dev/null; then
    err "Docker 安装后仍不可用，请检查: systemctl status docker"
    exit 1
  fi
  log "Docker 安装完成: $(docker --version)"
}

# ─────────────────────────────────────────────
# 4. 检查 Docker Compose
# ─────────────────────────────────────────────
ensure_compose() {
  if docker compose version &>/dev/null; then
    log "Docker Compose: $(docker compose version | head -1)"
    return
  fi
  warn "Docker Compose 插件缺失，尝试安装..."
  case "$PKG_MGR" in
    dnf|yum)  yum install -y docker-compose-plugin ;;
    apt)      apt-get install -y -qq docker-compose-plugin ;;
    pacman)   pacman -S --noconfirm docker-compose ;;
    zypper)   zypper --non-interactive install docker-compose ;;
    apk)      apk add docker-compose ;;
    *)        ;;
  esac
  log "Docker Compose: $(docker compose version 2>/dev/null || echo '已安装')"
}

# ─────────────────────────────────────────────
# 5. 定位项目源目录 & 准备安装目录
# ─────────────────────────────────────────────
# 正常用法：先解压部署包再运行脚本，脚本所在目录即项目目录。
# 容错用法：用户没解压就运行脚本（目录里只有 deploy.sh + tar 包），自动解压。
resolve_source() {
  SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
  SOURCE_DIR="$SCRIPT_DIR"

  if [[ ! -f "$SOURCE_DIR/docker-compose.yml" ]]; then
    local tarball="" f
    for f in "$SOURCE_DIR"/webb-api-deploy-*.tar.gz; do
      if [[ -f "$f" ]]; then tarball="$f"; break; fi
    done
    if [[ -n "$tarball" ]]; then
      warn "当前目录没有项目文件，自动解压部署包: $(basename "$tarball")"
      tar xzf "$tarball" -C "$SOURCE_DIR"
      # 解压目录名可能带日期后缀：webb-api-deploy 或 webb-api-deploy-YYYYMMDD
      local d
      for d in "$SOURCE_DIR"/webb-api-deploy*/; do
        if [[ -f "${d}docker-compose.yml" ]]; then
          SOURCE_DIR="${d%/}"
          break
        fi
      done
      log "部署包解压完成"
    fi
  fi

  if [[ ! -f "$SOURCE_DIR/docker-compose.yml" ]]; then
    err "当前目录缺少项目文件（docker-compose.yml）"
    err "请先解压部署包，进入解压目录后再运行:"
    err "  tar xzf webb-api-deploy-*.tar.gz && cd webb-api-deploy && sudo bash deploy.sh"
    exit 1
  fi
}

prepare_dirs() {
  if [[ -d "$INSTALL_DIR" ]]; then
    warn "安装目录 $INSTALL_DIR 已存在，覆盖项目文件（保留 data/ 和 logs/）"
    # 全树清理：删除除 data/ 和 logs/ 之外的所有条目，避免旧文件残留
    find "$INSTALL_DIR" -mindepth 1 -maxdepth 1 \
      ! -name 'data' ! -name 'logs' -exec rm -rf {} +
  else
    mkdir -p "$INSTALL_DIR"
  fi

  # 复制文件（源目录 == 安装目录时跳过，避免自拷贝）
  if [[ "$(cd "$SOURCE_DIR" 2>/dev/null && pwd)" == "$(cd "$INSTALL_DIR" 2>/dev/null && pwd)" ]]; then
    warn "项目文件已在 $INSTALL_DIR 内，跳过文件复制"
  else
    log "正在复制项目文件到 $INSTALL_DIR ..."
    cp -r "$SOURCE_DIR"/. "$INSTALL_DIR"/
  fi

  cd "$INSTALL_DIR"
  mkdir -p data logs

  # 复制结果校验：关键文件必须就位，避免后续步骤因缺文件而失败
  if [[ ! -f docker-compose.yml || ! -f Dockerfile ]]; then
    err "复制后未找到 docker-compose.yml / Dockerfile，部署中止"
    exit 1
  fi
  log "目录准备完成"
}

# ─────────────────────────────────────────────
# 6. 生成随机密码 & 更新 compose
# ─────────────────────────────────────────────
generate_password() {
  # 脚本头部已去掉 pipefail，此处 head -c 32 提前退出触发的 SIGPIPE 不再被视为错误
  tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 32
}

setup_secrets() {
  # 幂等设计：不依赖 .env.deployed 早退。
  # 若 compose 里仍是默认弱密码(123456)，就替换为随机密码；
  # 若已有随机密码则保留；确保 SESSION_SECRET 一定存在。

  local need_replace=0
  if grep -q "123456" docker-compose.yml; then
    need_replace=1
  fi

  if [[ $need_replace -eq 1 ]]; then
    local PG_PASSWORD REDIS_PASSWORD
    PG_PASSWORD=$(generate_password)
    REDIS_PASSWORD=$(generate_password)

    sed -i "s|postgresql://root:123456@webbapi-postgres:5432/webb-api|postgresql://root:${PG_PASSWORD}@webbapi-postgres:5432/webb-api|" docker-compose.yml
    sed -i "s|redis://:123456@webbapi-redis:6379|redis://:${REDIS_PASSWORD}@webbapi-redis:6379|" docker-compose.yml
    # 兼容 ""," " / "," 两种逗号-引号写法（逗号后可有空格或无空格）
    sed -i "s|\"--requirepass\",[[:space:]]*\"123456\"|\"--requirepass\", \"${REDIS_PASSWORD}\"|" docker-compose.yml
    sed -i "s|POSTGRES_PASSWORD:[[:space:]]*123456|POSTGRES_PASSWORD: ${PG_PASSWORD}|" docker-compose.yml
    log "已替换默认密码为随机密码"

    # 替换结果校验：四处生效点必须全部替换成功，否则立即中止
    # （曾因 requirepass 行 sed 模式与实际空格不匹配，导致 Redis 密码两边不一致 → WRONGPASS）
    # 注意：先剔除以 # 开头的注释行，避免误报被注释掉的示例配置（如 LOG_SQL_DSN/mysql/clickhouse）
    local active_hits
    active_hits=$(grep -Ev '^[[:space:]]*#' docker-compose.yml | grep -E 'redis://:123456@|--requirepass",?[[:space:]]*"123456"|POSTGRES_PASSWORD:[[:space:]]*123456|postgresql://root:123456@' || true)
    if [[ -n "$active_hits" ]]; then
      err "docker-compose.yml 中仍残留默认密码，替换未完全生效："
      echo "$active_hits" | sed 's/^/    /'
      exit 1
    fi
  else
    log "检测到 compose 已使用随机密码，跳过替换"
  fi

  # 确保 SESSION_SECRET 存在（放在 NODE_NAME 行后）
  if ! grep -q "SESSION_SECRET=" docker-compose.yml; then
    local SESSION_SECRET
    SESSION_SECRET=$(generate_password)
    sed -i "/NODE_NAME=/a\\      - SESSION_SECRET=${SESSION_SECRET}  # 自动生成" docker-compose.yml
    log "已生成并插入 SESSION_SECRET"
  else
    log "检测到 compose 已含 SESSION_SECRET，跳过"
  fi

  touch .env.deployed
}

# ─────────────────────────────────────────────
# 7. 构建 & 启动
# ─────────────────────────────────────────────
stop_old_containers() {
  if docker compose down 2>/dev/null; then
    log "已停止旧容器"
  fi
}

build_and_start() {
  # 构建：输出完整日志到文件，失败时打印尾部并立即中止（绝不能再误报成功）
  log "正在构建 Docker 镜像（首次约 5-10 分钟），日志写入 build.log ..."
  if ! docker compose build > build.log 2>&1; then
    err "Docker 镜像构建失败，最后 40 行日志："
    tail -40 build.log | sed 's/^/    /'
    err "完整日志: $INSTALL_DIR/build.log"
    exit 1
  fi
  log "镜像构建完成"

  # 启动：同样校验退出码
  log "正在启动容器..."
  if ! docker compose up -d > up.log 2>&1; then
    err "容器启动失败，最后 40 行日志："
    tail -40 up.log | sed 's/^/    /'
    err "完整日志: $INSTALL_DIR/up.log"
    exit 1
  fi

  echo ""
  docker compose ps
  echo ""
}

# ─────────────────────────────────────────────
# 8. 等待就绪
# ─────────────────────────────────────────────
wait_ready() {
  log "等待服务启动..."
  local MAX_WAIT=180 WAITED=0

  while [[ $WAITED -lt $MAX_WAIT ]]; do
    local code
    code=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:${WEBB_PORT}/api/status" 2>/dev/null || true)
    if [[ "$code" == "200" ]]; then
      log "服务已就绪！"
      return
    fi
    sleep 3
    WAITED=$((WAITED + 3))
    # 行内进度条
    local bar_width=40
    local pct=$(( WAITED * 100 / MAX_WAIT ))
    local filled=$(( WAITED * bar_width / MAX_WAIT ))
    local empty=$(( bar_width - filled ))
    local bar=""
    for ((i=0;i<filled;i++)); do bar+="█"; done
    for ((i=0;i<empty;i++)); do bar+="░"; done
    echo -ne "${DIM}  等待中 ${bar} ${WAITED}s/${MAX_WAIT}s (${pct}%)${NC}\r"
  done
  echo ""
  err "服务未在 ${MAX_WAIT}s 内就绪，部署未成功。排查步骤："
  err "  1) cd $INSTALL_DIR && docker compose logs --tail=50 webb-api"
  err "  2) 修复后执行: cd $INSTALL_DIR && docker compose restart webb-api"
  exit 1
}

# ─────────────────────────────────────────────
# 9. 输出结果
# ─────────────────────────────────────────────
print_result() {
  local HOST_IP
  HOST_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "127.0.0.1")

  echo ""
  echo -e "${BOLD}${GREEN}"
  echo "  ╔══════════════════════════════════════════════╗"
  echo "  ║           🚀  部 署 成 功 ！                 ║"
  echo "  ╚══════════════════════════════════════════════╝"
  echo -e "${NC}"
  echo ""
  echo -e "${BOLD}  访问地址${NC}    http://${HOST_IP}:${WEBB_PORT}"
  echo -e "${BOLD}  授权管理${NC}    http://${HOST_IP}:${WEBB_PORT}/license"
  echo -e "${BOLD}  数据库端口${NC}  ${DB_PORT} (PostgreSQL)"
  echo -e "${BOLD}  Redis 端口${NC}  ${REDIS_PORT}"
  echo -e "${BOLD}  安装目录${NC}  ${INSTALL_DIR}"
  echo ""
  echo -e "${BOLD}  常用运维命令${NC}:"
  echo -e "    ${DIM}查看日志${NC}   cd $INSTALL_DIR && docker compose logs -f"
  echo -e "    ${DIM}重启服务${NC}   cd $INSTALL_DIR && docker compose restart"
  echo -e "    ${DIM}停止服务${NC}   cd $INSTALL_DIR && docker compose down"
  echo -e "    ${DIM}升级部署${NC}   重新上传压缩包后 sudo bash deploy.sh"
  echo ""
  echo -e "${DIM}  上传 .lic 授权文件后 60s 内生效，无需重启${NC}"
  echo ""
}

# ─────────────────────────────────────────────
# 主流程
# ─────────────────────────────────────────────
main() {
  # 任何未预期失败都打印出来，避免静默退出
  trap 'echo -e "\n${RED}[ERR]${NC} 脚本在第 $LINENO 行附近失败 (exit code: $?)，上方输出请保留以便排查" >&2' ERR

  print_banner
  check_root

  # Step 1/6: 识别系统
  step "识别系统与包管理器"
  detect_system
  next_step
  progress_step "系统识别" "$TOTAL_STEPS" "$CURRENT_STEP"
  echo -e "    ${DIM}发行版: ${BOLD}${DISTRO}${NC}"
  echo -e "    ${DIM}包管理器: ${BOLD}${PKG_MGR}${NC}"
  if [[ -z "$PKG_MGR" ]]; then
    err "无法识别包管理器，请手动安装 Docker 后重新运行"
    exit 1
  fi

  # Step 2/6: 安装 Docker
  step "安装 Docker"
  install_docker
  ensure_compose
  next_step
  progress_step "Docker 安装" "$TOTAL_STEPS" "$CURRENT_STEP"

  # Step 3/6: 准备目录
  step "准备安装目录"
  resolve_source
  prepare_dirs
  next_step
  progress_step "目录准备" "$TOTAL_STEPS" "$CURRENT_STEP"

  # Step 4/6: 生成密码
  step "生成随机密码"
  setup_secrets
  next_step
  progress_step "密码生成" "$TOTAL_STEPS" "$CURRENT_STEP"

  # Step 5/6: 构建 & 启动
  step "构建并启动服务"
  stop_old_containers
  build_and_start
  next_step
  progress_step "构建启动" "$TOTAL_STEPS" "$CURRENT_STEP"

  # Step 6/6: 等待就绪
  step "等待服务就绪"
  wait_ready
  next_step
  progress_step "服务就绪" "$TOTAL_STEPS" "$CURRENT_STEP"

  echo ""
  # 全部完成，打印进度条满格
  progress_step "部署完成 ✓" "$TOTAL_STEPS" "$TOTAL_STEPS"
  print_result
  trap - ERR
}

main "$@"
