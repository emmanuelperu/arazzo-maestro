// Workflow: authenticated-order-lookup
// Log in, list orders, read the first one
//
// k6 performance test generated from an Arazzo workflow.
// Base URL: override with `k6 run -e BASE_URL=<endpoint> <file>`.
//   default (OpenAPI servers): https://orders.example.com/api/v1
//
// Inputs: override each with `-e <name>=<value>`.
//   - username (string)
//   - password (string)

import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || "https://orders.example.com/api/v1";

const username = __ENV["username"] || '';
const password = __ENV["password"] || '';

export const options = {
  vus: 1,
  duration: "30s",
};

export default function () {
  // Step: login
  // Exchange the credentials for a bearer token.
  // requestBody content-type: application/json
  const loginBody = {
    "password": password,
    "username": username
  };
  const loginRes = http.request('POST', `${BASE_URL}/auth/login`, JSON.stringify(loginBody), { headers: { "Content-Type": "application/json" } });
  check(loginRes, {
    "login: $statusCode == 200": (r) => r.status === 200,
  });
  const login_token = loginRes.json("token");

  // Step: list-orders
  // Authenticated call, the token comes from the login step.
  const list_ordersRes = http.request('GET', `${BASE_URL}/orders?page=0`, null, { headers: { "Authorization": `Bearer ${login_token}` } });
  check(list_ordersRes, {
    "list-orders: $statusCode == 200": (r) => r.status === 200,
  });
  const list_orders_firstOrderId = list_ordersRes.json("items.0.id");

  // Step: get-order
  // The order id is self-provisioned from the listing step.
  const get_orderRes = http.request('GET', `${BASE_URL}/orders/${list_orders_firstOrderId}`, null, { headers: { "Authorization": `Bearer ${login_token}` } });
  check(get_orderRes, {
    "get-order: $statusCode == 200": (r) => r.status === 200,
  });
  const get_order_orderStatus = get_orderRes.json("status");
}
