#!/bin/bash
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"${SCRIPT_DIR}/run_geth_with_tc_common.sh" tun0 reorder20percent_delay50ms netem delay 50ms reorder 20%
