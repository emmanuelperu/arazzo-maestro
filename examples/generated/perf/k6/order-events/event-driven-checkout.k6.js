// Workflow: event-driven-checkout
// Place an order, pay by message, confirm over HTTP
//
// k6 performance test generated from an Arazzo workflow.
// Base URL: override with `k6 run -e BASE_URL=<endpoint> <file>`.
//   default (OpenAPI servers): https://orders.example.com/api/v1
//
// Success criteria: 3 of 3 checked
//
// Inputs: override each with `-e <name>=<value>`.
//   - correlationId (string)
//   - productId (integer)

import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || "https://orders.example.com/api/v1";

const correlationId = __ENV["correlationId"] || '';
const productId = __ENV["productId"] || 42;

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
  // Step: placeOrder
  // Create the order; it waits for payment.
  // requestBody content-type: application/json
  const placeOrderBody = {
    "productId": productId,
    "quantity": 1
  };
  const placeOrderRes = http.request('POST', `${BASE_URL}/orders`, JSON.stringify(placeOrderBody), { headers: { "Content-Type": "application/json", "X-Correlation-Id": correlationId } });
  check(placeOrderRes, {
    "placeOrder: $statusCode == 202": (r) => r.status === 202,
  });
  const placeOrder_orderId = placeOrderRes.json("id");
  const placeOrder_total = placeOrderRes.json("total");

  // Step: requestPayment
  // Ask the payment service to charge the order.
  // not supported: AsyncAPI send step; no message sent or received, no request generated

  // Step: awaitPaymentConfirmed
  // Wait for the confirmation of this checkout's payment.
  // not supported: AsyncAPI receive step; no message sent or received, no request generated
  // warning: outputs paymentId are not captured; later references to them stay unresolved

  // Step: getOrder
  // Read the order back once the payment is captured.
  const getOrderRes = http.request('GET', `${BASE_URL}/orders/${placeOrder_orderId}`, null, { headers: {} });
  check(getOrderRes, {
    "getOrder: $statusCode == 200": (r) => r.status === 200,
    "getOrder: $response.body#/status == 'paid'": (r) => sameText(bodyAt(r, "status"), "paid"),
  });
  const getOrder_status = getOrderRes.json("status");
}
