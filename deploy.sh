#!/bin/bash

if (( $EUID != 0 )); then
	echo "Please run as root"
	exit
fi

SRC_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

echo "RUNNING systemctl stop geth" && \
systemctl stop geth && \
echo "RUNNING ${SRC_DIR}/uninstall.sh" && \
${SRC_DIR}/uninstall.sh && \
echo "RUNNING ${SRC_DIR}/install.sh" && \
${SRC_DIR}/install.sh && \
echo "RUNNING systemctl start geth" && \
systemctl start geth && \
echo "RUNNING journalctl -u geth -f" && \
journalctl -u geth -f
