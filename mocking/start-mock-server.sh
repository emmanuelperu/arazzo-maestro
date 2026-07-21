#!/bin/bash
# Boots a local Microcks instance and imports every OpenAPI contract from
# examples/, so the generated e2e (Hurl) and perf (k6) tests can run
# against real mocks. See mocking/README.md for the conventions the
# contracts must follow (named examples, request/response name matching).
set -e

cd "$(dirname "$0")"

if ! command -v docker &> /dev/null; then
  echo "Error: docker is not installed or not in PATH."
  exit 1
fi
if ! docker compose version &> /dev/null; then
  echo "Error: the 'docker compose' plugin is not available."
  exit 1
fi

MICROCKS_API='http://localhost:8082/api'

docker compose down
docker compose up -d

until curl -s -o /dev/null "$MICROCKS_API/services"; do
  echo "Waiting for Microcks to be up..."
  sleep 5
done

# Import every OpenAPI contract next to the Arazzo examples. The upload
# API replaces a service on re-import, so the script is idempotent.
echo ""
echo "=== Importing OpenAPI contracts ==="
for spec in ../examples/*.yaml; do
  case "$spec" in
    *.arazzo.yaml) continue ;;
  esac
  printf '%-45s -> ' "$(basename "$spec")"
  curl -s -F "file=@$spec" -F "mainArtifact=true" "$MICROCKS_API/artifact/upload"
  echo ""
done

# ------------------------------------------------------------------
# Custom dispatcher: payment outcome routed on the card number.
#
# OpenAPI named examples give Microcks one response per dispatch
# criteria built from PATH/QUERY parameter examples; routing on the
# REQUEST BODY is a Microcks-side setting that cannot be expressed in
# the contract, so it is pushed here after the import. The card ending
# 1111 pays (response example "success"), the card ending 0002 is
# declined (response example "refused").
# ------------------------------------------------------------------
echo ""
echo "=== Configuring the payment JSON_BODY dispatcher (Shop API) ==="
shop_id=$(curl -s "$MICROCKS_API/services?page=0&size=20" | python3 -c "
import json, sys
services = json.load(sys.stdin)
print(next(s['id'] for s in services if s['name'] == 'Shop API'))
")
curl -s -X PUT "$MICROCKS_API/services/$shop_id/operation?operationName=POST%20/orders/%7BorderId%7D/payment" \
  -H 'Content-Type: application/json' \
  --data-raw '{"defaultDelay":0,"dispatcher":"JSON_BODY","dispatcherRules":"{\"exp\": \"/cardNumber\", \"operator\": \"equals\", \"cases\": {\"4111111111111111\": \"success\", \"4111111111110002\": \"refused\", \"default\": \"success\"}}","parameterConstraints":[]}' \
  > /dev/null
echo "JSON_BODY dispatcher pushed on POST /orders/{orderId}/payment."

echo ""
echo "=== Mock server ready ==="
echo "Microcks UI: http://localhost:8082"
echo ""
echo "Mock base URLs:"
echo "  Shop API:     http://localhost:8082/rest/Shop+API/1.0.0"
echo "  Orders API:   http://localhost:8082/rest/Orders+API/1.0.0"
echo "  Checkout API: http://localhost:8082/rest/Checkout+API+(branching+demo)/1.0.0"
echo ""
echo "Run the generated tests against the mocks (from the repo root):"
echo ""
echo "  ./bin/arazzo-maestro test run e2e examples/shop.arazzo.yaml \\"
echo "    --base-url 'http://localhost:8082/rest/Shop+API/1.0.0' \\"
echo "    --variable productId=p-001 --variable orderId=ord-1 --variable acceptLanguage=en"
echo ""
echo "  ./bin/arazzo-maestro test run e2e examples/auth.arazzo.yaml \\"
echo "    --base-url 'http://localhost:8082/rest/Orders+API/1.0.0' \\"
echo "    --variable username=demo --variable password=demo-password"
echo ""
echo "  ./bin/arazzo-maestro test run e2e examples/checkout-branching.arazzo.yaml \\"
echo "    --base-url 'http://localhost:8082/rest/Checkout+API+(branching+demo)/1.0.0' \\"
echo "    --variable orderId=ord-1 --variable acceptLanguage=en"
echo ""
echo "  k6 run --vus 1 --iterations 1 \\"
echo "    -e BASE_URL='http://localhost:8082/rest/Shop+API/1.0.0' \\"
echo "    -e productId=p-001 -e orderId=ord-1 -e acceptLanguage=en \\"
echo "    examples/generated/perf/k6/shop/happy-path-checkout.k6.js"
