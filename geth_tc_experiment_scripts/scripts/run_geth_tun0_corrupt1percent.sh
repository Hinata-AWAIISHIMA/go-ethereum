#!/bin/bash
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"${SCRIPT_DIR}/run_geth_with_tc_common.sh" tun0 corrupt1percent netem corrupt 1%
