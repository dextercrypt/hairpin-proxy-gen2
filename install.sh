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
# Step 2 — Mode selection
# ---------------------------------------------------------------------------
echo -e "  ${BOLD_YELLOW}❯ Step 2 — Select Mode${RESET}"
echo ""
echo -e "  ${DIM}Choose which resources hairpin-proxy-gen2 should watch:${RESET}"
echo ""
echo -e "  ${BOLD_WHITE}  1)${RESET} ${CYAN}gateway${RESET}  ${DIM}— Gateway API only  (HTTPRoute, GRPCRoute, TLSRoute, Gateway listeners)${RESET}"
echo -e "  ${BOLD_WHITE}  2)${RESET} ${CYAN}ingress${RESET}  ${DIM}— Ingress only       (networking.k8s.io/v1 Ingress resources)${RESET}"
echo -e "  ${BOLD_WHITE}  3)${RESET} ${CYAN}both${RESET}     ${DIM}— Dual-stack         (all resources, routed to correct backend by source)${RESET}"
echo ""
echo -e -n "  ${BOLD_WHITE}Select mode${RESET} ${DIM}[1/2/3, default: 3]${RESET}${BOLD_WHITE}: ${RESET}"
read -r MODE_INPUT

case "$MODE_INPUT" in
  1) MODE="gateway" ;;
  2) MODE="ingress" ;;
  *)  MODE="both" ;;
esac

echo -e "\n  ${GREEN}✔${RESET}  Mode: ${CYAN}${MODE}${RESET}"

confirm "Mode set to ${CYAN}${MODE}${RESET}${BOLD_WHITE} — proceed to target configuration?"

# ---------------------------------------------------------------------------
# Step 3 — Target server(s) based on mode
# ---------------------------------------------------------------------------
GATEWAY_TARGET=""
INGRESS_TARGET=""

DEFAULT_GATEWAY_TARGET="envoy-gateway.envoy-gateway-system.svc.cluster.local"
DEFAULT_INGRESS_TARGET="ingress-nginx-controller.ingress-nginx.svc.cluster.local"

if [[ "$MODE" == "gateway" || "$MODE" == "both" ]]; then
  echo -e "  ${BOLD_YELLOW}❯ Step 3a — Gateway API Target Server${RESET}"
  echo ""
  echo -e "  ${DIM}Where HAProxy forwards Gateway API traffic (HTTPRoute, GRPCRoute, etc.)${RESET}"
  echo ""
  echo -e "  ${DIM}Examples:${RESET}"
  echo -e "  ${DIM}    Envoy Gateway  →  ${CYAN}envoy-gateway.envoy-gateway-system.svc.cluster.local${RESET}"
  echo -e "  ${DIM}    Istio          →  ${CYAN}istio-ingressgateway.istio-system.svc.cluster.local${RESET}"
  echo -e "  ${DIM}    Cilium         →  ${CYAN}cilium-gateway.kube-system.svc.cluster.local${RESET}"
  echo ""
  echo -e -n "  ${BOLD_WHITE}Gateway API Target${RESET} ${DIM}[default: ${CYAN}${DEFAULT_GATEWAY_TARGET}${RESET}${DIM}]${RESET}${BOLD_WHITE}: ${RESET}"
  read -r GATEWAY_INPUT
  GATEWAY_TARGET="${GATEWAY_INPUT:-$DEFAULT_GATEWAY_TARGET}"
  echo -e "\n  ${GREEN}✔${RESET}  Gateway target: ${CYAN}${GATEWAY_TARGET}${RESET}"
  echo ""
fi

if [[ "$MODE" == "ingress" || "$MODE" == "both" ]]; then
  echo -e "  ${BOLD_YELLOW}❯ Step 3b — Ingress Target Server${RESET}"
  echo ""
  echo -e "  ${DIM}Where HAProxy forwards Ingress traffic.${RESET}"
  echo ""
  echo -e "  ${DIM}Examples:${RESET}"
  echo -e "  ${DIM}    ingress-nginx  →  ${CYAN}ingress-nginx-controller.ingress-nginx.svc.cluster.local${RESET}"
  echo -e "  ${DIM}    Traefik        →  ${CYAN}traefik.traefik.svc.cluster.local${RESET}"
  echo -e "  ${DIM}    Kong           →  ${CYAN}kong-proxy.kong.svc.cluster.local${RESET}"
  echo ""
  echo -e -n "  ${BOLD_WHITE}Ingress Target${RESET} ${DIM}[default: ${CYAN}${DEFAULT_INGRESS_TARGET}${RESET}${DIM}]${RESET}${BOLD_WHITE}: ${RESET}"
  read -r INGRESS_INPUT
  INGRESS_TARGET="${INGRESS_INPUT:-$DEFAULT_INGRESS_TARGET}"
  echo -e "\n  ${GREEN}✔${RESET}  Ingress target: ${CYAN}${INGRESS_TARGET}${RESET}"
  echo ""
fi

confirm "Targets configured — proceed to download?"

# ---------------------------------------------------------------------------
# Step 4 — Download the correct manifest
# ---------------------------------------------------------------------------
BASE_URL="https://raw.githubusercontent.com/dextercrypt/hairpin-proxy-gen2/main"
MANIFEST_URL="${BASE_URL}/install-${MODE}.yaml"
TMP_FILE="$(mktemp /tmp/hairpin-proxy-gen2-XXXXXX.yaml)"

echo -e "  ${BOLD_YELLOW}❯ Step 4 — Downloading manifest${RESET}"
echo ""

curl -fsSL "$MANIFEST_URL" -o "$TMP_FILE" &
spinner $! "Pulling install-${MODE}.yaml from GitHub..."

# Patch targets
if [[ -n "$GATEWAY_TARGET" ]]; then
  sed -i.bak "s|envoy-gateway.envoy-gateway-system.svc.cluster.local|${GATEWAY_TARGET}|g" "$TMP_FILE"
fi
if [[ -n "$INGRESS_TARGET" ]]; then
  sed -i.bak "s|ingress-nginx-controller.ingress-nginx.svc.cluster.local|${INGRESS_TARGET}|g" "$TMP_FILE"
fi
rm -f "${TMP_FILE}.bak"

confirm "Manifest downloaded and patched — proceed to review summary?"

# ---------------------------------------------------------------------------
# Step 5 — Summary
# ---------------------------------------------------------------------------
echo -e "  ${BOLD_YELLOW}❯ Step 5 — Summary${RESET}"
echo ""
echo -e "  ${DIM}  Namespace   :${RESET}  ${WHITE}hairpin-proxy-gen2${RESET}"
echo -e "  ${DIM}  Mode        :${RESET}  ${CYAN}${MODE}${RESET}"
echo -e "  ${DIM}  Controller  :${RESET}  ${WHITE}dextercrypt/hairpin-proxy-gen2-controller:v0.0.1${RESET}"

if [[ "$MODE" == "gateway" || "$MODE" == "both" ]]; then
  echo -e "  ${DIM}  HAProxy (Gateway API) :${RESET}  ${WHITE}dextercrypt/hairpin-proxy-gen2-haproxy:v0.0.1${RESET}"
  echo -e "  ${DIM}  Gateway target        :${RESET}  ${CYAN}${GATEWAY_TARGET}${RESET}"
fi
if [[ "$MODE" == "ingress" || "$MODE" == "both" ]]; then
  echo -e "  ${DIM}  HAProxy (Ingress)     :${RESET}  ${WHITE}dextercrypt/hairpin-proxy-gen2-haproxy:v0.0.1${RESET}"
  echo -e "  ${DIM}  Ingress target        :${RESET}  ${CYAN}${INGRESS_TARGET}${RESET}"
fi
echo ""

confirm "Everything looks good — apply to cluster now?"

# ---------------------------------------------------------------------------
# Step 6 — Apply
# ---------------------------------------------------------------------------
echo -e "  ${BOLD_YELLOW}❯ Step 6 — Applying to cluster${RESET}"
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
