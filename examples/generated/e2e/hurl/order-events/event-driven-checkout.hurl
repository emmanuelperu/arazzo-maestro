# Workflow: event-driven-checkout
# Place an order, pay by message, confirm over HTTP
#
# Base URL (required): pass via `hurl --variable baseUrl=<endpoint>`
#   default (OpenAPI servers): https://orders.example.com/api/v1
#
# Inputs (pass via `hurl --variable name=value`):
#   - correlationId (string)
#   - productId (integer)

# Step: placeOrder
# Create the order; it waits for payment.
POST {{baseUrl}}/orders
X-Correlation-Id: {{correlationId}}
Content-Type: application/json
# requestBody content-type: application/json
```
{
  "productId": {{productId}},
  "quantity": 1
}
```

HTTP *
[Asserts]
# $statusCode == 202
[Captures]
placeOrder_orderId: jsonpath "$.id"
placeOrder_total: jsonpath "$.total"

# Step: requestPayment
# Ask the payment service to charge the order.
# not supported: AsyncAPI send step; no message sent or received, no request generated

# Step: awaitPaymentConfirmed
# Wait for the confirmation of this checkout's payment.
# not supported: AsyncAPI receive step; no message sent or received, no request generated
# warning: outputs paymentId are not captured; later references to them stay unresolved

# Step: getOrder
# Read the order back once the payment is captured.
GET {{baseUrl}}/orders/{{placeOrder_orderId}}

HTTP *
[Asserts]
# $statusCode == 200
# $response.body#/status == 'paid'
[Captures]
getOrder_status: jsonpath "$.status"
