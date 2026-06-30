#!/bin/bash
set -u
INTERFACE="$1"
CONDITION="$2"
QDISC_TYPE="$3"
shift 3
TC_ARGS=("$@")
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="${SCRIPT_DIR}/.."
RUN_GETH="${BASE_DIR}/run_geth.sh"
LOG_DIR="${BASE_DIR}/logs"
SOURCE_LOG="${LOG_DIR}/tester1.log"
SOURCE_PCAP="${BASE_DIR}/elcapture.pcap"
TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
RESULT_PREFIX="tester1_${INTERFACE}_${CONDITION}_${TIMESTAMP}"
RESULT_LOG="${LOG_DIR}/${RESULT_PREFIX}.log"
RESULT_PCAP="${LOG_DIR}/${RESULT_PREFIX}.pcap"
cleanup() {
  sudo tc qdisc del dev "${INTERFACE}" root 2>/dev/null || true
  [ -f "${SOURCE_LOG}" ] && mv "${SOURCE_LOG}" "${RESULT_LOG}"
  [ -f "${SOURCE_PCAP}" ] && mv "${SOURCE_PCAP}" "${RESULT_PCAP}"
}
trap cleanup EXIT
mkdir -p "${LOG_DIR}"
"${RUN_GETH}" &
GETH_PID=$!
until ip link show "${INTERFACE}" >/dev/null 2>&1; do sleep 1; done
sudo tc qdisc replace dev "${INTERFACE}" root "${QDISC_TYPE}" "${TC_ARGS[@]}"
wait "${GETH_PID}"
