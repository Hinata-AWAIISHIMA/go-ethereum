#!/bin/bash

if (( $EUID != 0 )); then
	echo "Please run as root"
	exit
fi

SRC_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

sudo systemctl stop geth && \
${SRC_DIR}/uninstall.sh && \
${SRC_DIR}/install.sh && \
systemctl start geth && \
journalctl -u geth -f
