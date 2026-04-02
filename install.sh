#!/usr/bin/env bash
set -euo pipefail

# ---------------------------------------------------------------------------
# Colors & styles
# ---------------------------------------------------------------------------
RESET="\033[0m"
BOLD="\033[1m"
DIM="\033[2m"
RED="\033[0;31m"
GREEN="\033[0;32m"
YELLOW="\033[0;33m"
CYAN="\033[0;36m"
MAGENTA="\033[0;35m"
WHITE="\033[0;37m"
BOLD_CYAN="\033[1;36m"
BOLD_GREEN="\033[1;32m"
BOLD_YELLOW="\033[1;33m"
BOLD_RED="\033[1;31m"
BOLD_WHITE="\033[1;37m"

# ---------------------------------------------------------------------------
# Banner
# ---------------------------------------------------------------------------
clear
echo ""
echo -e "${BOLD_CYAN}"
echo "  ██╗  ██╗ █████╗ ██╗██████╗ ██████╗ ██╗███╗   ██╗"
echo "  ██║  ██║██╔══██╗██║██╔══██╗██╔══██╗██║████╗  ██║"
echo "  ███████║███████║██║██████╔╝██████╔╝██║██╔██╗ ██║"
echo "  ██╔══██║██╔══██║██║██╔══██╗██╔═══╝ ██║██║╚██╗██║"
echo "  ██║  ██║██║  ██║██║██║  ██║██║     ██║██║ ╚████║"
echo "  ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝╚═╝  ╚═╝╚═╝     ╚═╝╚═╝  ╚═══╝"
echo -e "${RESET}"
echo -e "${BOLD_CYAN}  ██████╗ ██████╗  ██████╗ ██╗  ██╗██╗   ██╗     ██████╗ ███████╗███╗  ██╗██████╗ ${RESET}"
echo -e "${BOLD_CYAN}  ██╔══██╗██╔══██╗██╔═══██╗╚██╗██╔╝╚██╗ ██╔╝    ██╔════╝ ██╔════╝████╗ ██║╚════██╗${RESET}"
echo -e "${BOLD_CYAN}  ██████╔╝██████╔╝██║   ██║ ╚███╔╝  ╚████╔╝     ██║  ███╗█████╗  ██╔██╗██║ █████╔╝${RESET}"
echo -e "${BOLD_CYAN}  ██╔═══╝ ██╔══██╗██║   ██║ ██╔██╗   ╚██╔╝      ██║   ██║██╔══╝  ██║╚████║██╔═══╝ ${RESET}"
echo -e "${BOLD_CYAN}  ██║     ██║  ██║╚██████╔╝██╔╝ ██╗   ██║       ╚██████╔╝███████╗██║ ╚███║███████╗${RESET}"
echo -e "${BOLD_CYAN}  ╚═╝     ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═╝   ╚═╝        ╚═════╝ ╚══════╝╚═╝  ╚══╝╚══════╝${RESET}"
echo ""
echo -e "  ${DIM}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
echo -e "  ${BOLD_WHITE}  Kubernetes Hairpin Proxy — Generation 2${RESET}"
echo -e "  ${DIM}  Ingress + Gateway API • CoreDNS Rewriting • Cert-Manager Ready${RESET}"
echo -e "  ${DIM}  by ${RESET}${MAGENTA}@dextercrypt${RESET}${DIM}  •  ${RESET}${CYAN}https://github.com/dextercrypt${RESET}"
echo -e "  ${DIM}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
echo ""

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
spinner() {
  local pid=$1
  local msg=$2
  local frames=("⠋" "⠙" "⠹" "⠸" "⠼" "⠴" "⠦" "⠧" "⠇" "⠏")
  local i=0
  while kill -0 "$pid" 2>/dev/null; do
    printf "\r  ${CYAN}${frames[$i]}${RESET}  ${WHITE}%s${RESET}" "$msg"
    i=$(( (i+1) % ${#frames[@]} ))
    sleep 0.08
  done
  printf "\r  ${BOLD_GREEN}✔${RESET}  ${WHITE}%s${RESET}\n" "$msg"
}

confirm() {
  local msg=$1
  echo ""
  echo -e -n "  ${BOLD_YELLOW}?${RESET}  ${BOLD_WHITE}${msg}${RESET} ${DIM}[y/N]${RESET}: "
  read -r REPLY
  echo ""
  if [[ ! "$REPLY" =~ ^[Yy]$ ]]; then
    echo -e "  ${BOLD_RED}✘  Aborted.${RESET}"
    echo ""
    exit 0
  fi
}

# ---------------------------------------------------------------------------
# Step 1 — Preflight checks
# ---------------------------------------------------------------------------
echo -e "  ${BOLD_YELLOW}❯ Step 1 — Preflight checks${RESET}"
echo ""

check_cmd() {
  if command -v "$1" &>/dev/null; then
    echo -e "  ${GREEN}✔${RESET}  ${WHITE}$1${RESET} found"
  else
    echo -e "  ${RED}✘${RESET}  ${WHITE}$1${RESET} not found — please install it first"
    exit 1
  fi
}

check_cmd kubectl
check_cmd curl

confirm "Preflight looks good — proceed to configuration?"

# ---------------------------------------------------------------------------
# Step 2 — TARGET_SERVER prompt
# ---------------------------------------------------------------------------
DEFAULT_TARGET="envoy-gateway.envoy-gateway-system.svc.cluster.local"

echo -e "  ${BOLD_YELLOW}❯ Step 2 — Target Server Configuration${RESET}"
echo ""
echo -e "  ${DIM}This is where HAProxy will forward all hairpin traffic.${RESET}"
echo -e "  ${DIM}It should be the in-cluster FQDN of your ingress controller or API Gateway.${RESET}"
echo ""
echo -e "  ${DIM}Examples:${RESET}"
echo -e "  ${DIM}    Envoy Gateway  →  ${CYAN}envoy-gateway.envoy-gateway-system.svc.cluster.local${RESET}"
echo -e "  ${DIM}    ingress-nginx  →  ${CYAN}ingress-nginx-controller.ingress-nginx.svc.cluster.local${RESET}"
echo -e "  ${DIM}    Traefik        →  ${CYAN}traefik.traefik.svc.cluster.local${RESET}"
echo -e "  ${DIM}    Kong           →  ${CYAN}kong-proxy.kong.svc.cluster.local${RESET}"
echo ""
echo -e -n "  ${BOLD_WHITE}Target Server${RESET} ${DIM}[default: ${CYAN}${DEFAULT_TARGET}${RESET}${DIM}]${RESET}${BOLD_WHITE}: ${RESET}"
read -r TARGET_INPUT

if [[ -z "$TARGET_INPUT" ]]; then
  TARGET_SERVER="$DEFAULT_TARGET"
  echo -e "\n  ${DIM}No input — using default:${RESET} ${CYAN}${TARGET_SERVER}${RESET}"
else
  TARGET_SERVER="$TARGET_INPUT"
  echo -e "\n  ${GREEN}✔${RESET}  Using: ${CYAN}${TARGET_SERVER}${RESET}"
fi

confirm "Target set to ${CYAN}${TARGET_SERVER}${RESET}${BOLD_WHITE} — proceed to download?"

# ---------------------------------------------------------------------------
# Step 3 — Download install.yaml
# ---------------------------------------------------------------------------
INSTALL_YAML_URL="https://raw.githubusercontent.com/dextercrypt/hairpin-proxy-gen2/main/install.yaml"
TMP_FILE="$(mktemp /tmp/hairpin-proxy-gen2-XXXXXX.yaml)"

echo -e "  ${BOLD_YELLOW}❯ Step 3 — Downloading manifest${RESET}"
echo ""

curl -fsSL "$INSTALL_YAML_URL" -o "$TMP_FILE" &
spinner $! "Pulling install.yaml from GitHub..."

sed -i.bak "s|envoy-gateway.envoy-gateway-system.svc.cluster.local|${TARGET_SERVER}|g" "$TMP_FILE"
rm -f "${TMP_FILE}.bak"

confirm "Manifest downloaded and patched — proceed to review summary?"

# ---------------------------------------------------------------------------
# Step 4 — Summary
# ---------------------------------------------------------------------------
echo -e "  ${BOLD_YELLOW}❯ Step 4 — Summary${RESET}"
echo ""
echo -e "  ${DIM}  Namespace   :${RESET}  ${WHITE}hairpin-proxy-gen2${RESET}"
echo -e "  ${DIM}  HAProxy     :${RESET}  ${WHITE}dextercrypt/hairpin-proxy-gen2-haproxy:v0.0.1${RESET}"
echo -e "  ${DIM}  Controller  :${RESET}  ${WHITE}dextercrypt/hairpin-proxy-gen2-controller:v0.0.1${RESET}"
echo -e "  ${DIM}  Target      :${RESET}  ${CYAN}${TARGET_SERVER}${RESET}"
echo ""

confirm "Everything looks good — apply to cluster now?"

# ---------------------------------------------------------------------------
# Step 5 — Apply
# ---------------------------------------------------------------------------
echo -e "  ${BOLD_YELLOW}❯ Step 5 — Applying to cluster${RESET}"
echo ""

kubectl apply -f "$TMP_FILE" &
spinner $! "Applying manifest to Kubernetes..."

rm -f "$TMP_FILE"

echo ""
echo -e "  ${DIM}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
echo -e "  ${BOLD_GREEN}  ✔  hairpin-proxy-gen2 installed successfully!${RESET}"
echo -e "  ${DIM}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
echo ""
echo -e "  ${DIM}  Check status:${RESET}"
echo -e "  ${CYAN}  kubectl get all -n hairpin-proxy-gen2${RESET}"
echo ""
