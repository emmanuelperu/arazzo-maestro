# Workflow: checkout-with-branching
# Pay, then confirm on success or cancel on failure
#
# Base URL (required): pass via `hurl --variable baseUrl=<endpoint>`
#   default (OpenAPI servers): https://shop.example.com/api/v1
#
# Inputs (pass via `hurl --variable name=value`):
#   - orderId (string)
#   - acceptLanguage (string)

# Step: pay
# Charge the customer's card for the order.
POST {{baseUrl}}/orders/{{orderId}}/payment
Accept-Language: {{acceptLanguage}}
Content-Type: application/json
# requestBody content-type: application/json
```
{
  "amount": 49.99,
  "cardHolder": "John Doe",
  "cardNumber": "4111111111111111",
  "cvv": "123",
  "expiryDate": "12/26",
  "method": "card"
}
```

HTTP *
[Asserts]
# $statusCode == 200
# $response.body#/status == "OK"
[Captures]
pay_paymentStatus: jsonpath "$.status"

# Step: confirm-order
# Payment accepted, confirm the order and capture the receipt id.
POST {{baseUrl}}/orders/{{orderId}}/confirm

HTTP *
[Asserts]
# $statusCode == 200
[Captures]
confirm-order_confirmationId: jsonpath "$.confirmationId"

# Step: cancel-order
# Payment refused, cancel the order and release reserved stock.
POST {{baseUrl}}/orders/{{orderId}}/cancel

HTTP *
[Asserts]
# $statusCode == 200
[Captures]
cancel-order_cancellationReason: jsonpath "$.reason"
