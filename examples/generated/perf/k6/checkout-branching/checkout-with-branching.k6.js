// Workflow: checkout-with-branching
// Pay, then confirm on success or cancel on failure
//
// k6 performance test generated from an Arazzo workflow.
// Base URL: override with `k6 run -e BASE_URL=<endpoint> <file>`.
//   default (OpenAPI servers): https://shop.example.com/api/v1
//
// Inputs: override each with `-e <name>=<value>`.
//   - orderId (string)
//   - acceptLanguage (string)

import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || "https://shop.example.com/api/v1";

const orderId = __ENV["orderId"] || "order-001";
const acceptLanguage = __ENV["acceptLanguage"] || "fr-FR";

export const options = {
  vus: 1,
  duration: "30s",
};

export default function () {
  // Step: pay
  // Charge the customer's card for the order.
  // requestBody content-type: application/json
  const payBody = {
    "amount": 49.99,
    "cardHolder": "John Doe",
    "cardNumber": "4111111111111111",
    "cvv": "123",
    "expiryDate": "12/26",
    "method": "card"
  };
  const payRes = http.request('POST', `${BASE_URL}/orders/${orderId}/payment`, JSON.stringify(payBody), { headers: { "Content-Type": "application/json", "Accept-Language": acceptLanguage } });
  // successCriteria (not translated): $response.body#/status == "OK"
  check(payRes, {
    "pay: $statusCode == 200": (r) => r.status === 200,
  });
  const pay_paymentStatus = payRes.json("status");

  // Step: confirm-order
  // Payment accepted, confirm the order and capture the receipt id.
  const confirm_orderRes = http.request('POST', `${BASE_URL}/orders/${orderId}/confirm`, null, { headers: {} });
  check(confirm_orderRes, {
    "confirm-order: $statusCode == 200": (r) => r.status === 200,
  });
  const confirm_order_confirmationId = confirm_orderRes.json("confirmationId");

  // Step: cancel-order
  // Payment refused, cancel the order and release reserved stock.
  const cancel_orderRes = http.request('POST', `${BASE_URL}/orders/${orderId}/cancel`, null, { headers: {} });
  check(cancel_orderRes, {
    "cancel-order: $statusCode == 200": (r) => r.status === 200,
  });
  const cancel_order_cancellationReason = cancel_orderRes.json("reason");
}
