#!/bin/bash
# Stops the local Microcks instance started by start-mock-server.sh.
set -e
cd "$(dirname "$0")"
docker compose down
