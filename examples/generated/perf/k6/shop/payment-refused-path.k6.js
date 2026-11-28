// Workflow: payment-refused-path
// Add to cart then receive a refused card payment
//
// k6 performance test generated from an Arazzo workflow.
// Base URL: override with `k6 run -e BASE_URL=<endpoint> <file>`.
//   default (OpenAPI servers): https://shop.example.com/api/v1
//
// Success criteria: 3 of 3 checked
//
// Inputs: override each with `-e <name>=<value>`.
//   - orderId (string)
//   - acceptLanguage (string)

import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || "https://shop.example.com/api/v1";

const orderId = __ENV["orderId"] || "order-001";
const acceptLanguage = __ENV["acceptLanguage"] || "fr-FR";

// Success-criteria helpers: Arazzo compares strings case-insensitively
// and coerces numeric strings for ordering operators.
function bodyAt(r, path) { try { return path === "" ? r.json() : r.json(path); } catch (e) { return undefined; } }
function headerOf(r, name) { const k = Object.keys(r.headers).find((h) => h.toLowerCase() === name.toLowerCase()); return k === undefined ? undefined : r.headers[k]; }
function sameText(v, s) { return typeof v === "string" && v.toLowerCase() === s.toLowerCase(); }
function asNumber(v) { return typeof v === "number" ? v : typeof v === "string" && v.trim() !== "" ? Number(v) : NaN; }

export const options = {
  vus: 1,
  duration: "30s",
};

export default function () {
  // Step: add-to-cart
  // Add a product to the cart
  // requestBody content-type: application/json
  const add_to_cartBody = {
    "productId": "prod-002",
    "quantity": 1
  };
  const add_to_cartRes = http.request('POST', `${BASE_URL}/cart/items`, JSON.stringify(add_to_cartBody), { headers: { "Content-Type": "application/json", "Accept-Language": acceptLanguage } });
  check(add_to_cartRes, {
    "add-to-cart: $statusCode == 201": (r) => r.status === 201,
  });

  // Step: pay-refused
  // Pay with a card that triggers a bank refusal. The bank may
  // also respond 5xx on transient errors, in that case we retry
  // up to twice with 2 s back-off before accepting the failure.
  // requestBody content-type: application/json
  const pay_refusedBody = {
    "amount": 49.99,
    "cardHolder": "Jane Doe",
    "cardNumber": "4111111111110002",
    "cvv": "456",
    "expiryDate": "06/25",
    "method": "card"
  };
  const pay_refusedRes = http.request('POST', `${BASE_URL}/orders/${orderId}/payment`, JSON.stringify(pay_refusedBody), { headers: { "Content-Type": "application/json", "Accept-Language": acceptLanguage } });
  check(pay_refusedRes, {
    "pay-refused: $statusCode == 200": (r) => r.status === 200,
    "pay-refused: $response.body#/status == 'REFUSED'": (r) => sameText(bodyAt(r, "status"), "REFUSED"),
  });
}
