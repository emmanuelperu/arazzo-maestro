// Workflow: happy-path-checkout
// Browse, add to cart and pay successfully
//
// k6 performance test generated from an Arazzo workflow.
// Base URL: override with `k6 run -e BASE_URL=<endpoint> <file>`.
//   default (OpenAPI servers): https://shop.example.com/api/v1
//
// Inputs: override each with `-e <name>=<value>`.
//   - productId (string)
//   - orderId (string)
//   - acceptLanguage (string)

import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || "https://shop.example.com/api/v1";

const productId = __ENV["productId"] || "prod-001";
const orderId = __ENV["orderId"] || "order-001";
const acceptLanguage = __ENV["acceptLanguage"] || "fr-FR";

export const options = {
  vus: 1,
  duration: "30s",
};

export default function () {
  // Step: list-catalog
  // Browse the product catalog
  const list_catalogRes = http.request('GET', `${BASE_URL}/products?page=0&size=10`, null, { headers: { "Accept-Language": acceptLanguage } });
  check(list_catalogRes, {
    "list-catalog: $statusCode == 200": (r) => r.status === 200,
  });
  const list_catalog_firstProductId = list_catalogRes.json("items.0.id");

  // Step: get-product
  // Fetch the selected product's full details
  const get_productRes = http.request('GET', `${BASE_URL}/products/${list_catalog_firstProductId}`, null, { headers: { "Accept-Language": acceptLanguage } });
  check(get_productRes, {
    "get-product: $statusCode == 200": (r) => r.status === 200,
  });
  const get_product_productName = get_productRes.json("name");

  // Step: add-to-cart
  // Add the product to the shopping cart
  // requestBody content-type: application/json
  const add_to_cartBody = {
    "productId": productId,
    "quantity": 2
  };
  const add_to_cartRes = http.request('POST', `${BASE_URL}/cart/items`, JSON.stringify(add_to_cartBody), { headers: { "Accept-Language": acceptLanguage } });
  // successCriteria (not translated): $response.body#/totalPrice > 0
  check(add_to_cartRes, {
    "add-to-cart: $statusCode == 201": (r) => r.status === 201,
  });
  const add_to_cart_cartTotal = add_to_cartRes.json("totalPrice");

  // Step: pay
  // Pay the order with a valid card
  // requestBody content-type: application/json
  const payBody = {
    "amount": 49.99,
    "cardHolder": "John Doe",
    "cardNumber": "4111111111111111",
    "cvv": "123",
    "expiryDate": "12/26",
    "method": "card"
  };
  const payRes = http.request('POST', `${BASE_URL}/orders/${orderId}/payment`, JSON.stringify(payBody), { headers: { "Accept-Language": acceptLanguage } });
  // successCriteria (not translated): $response.body#/status == "OK"
  check(payRes, {
    "pay: $statusCode == 200": (r) => r.status === 200,
  });
  const pay_transactionId = payRes.json("transactionId");
}
