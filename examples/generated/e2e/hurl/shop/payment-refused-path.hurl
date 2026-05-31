# Workflow: payment-refused-path
# Add to cart then receive a refused card payment
#
# Base URL (required): pass via `hurl --variable baseUrl=<endpoint>`
#   default (OpenAPI servers): https://shop.example.com/api/v1
#
# Inputs (pass via `hurl --variable name=value`):
#   - orderId (string)
#   - acceptLanguage (string)

# Step: add-to-cart
# Add a product to the cart
POST {{baseUrl}}/cart/items
Accept-Language: {{acceptLanguage}}
# requestBody content-type: application/json
```
{
  "productId": "prod-002",
  "quantity": 1
}
```

HTTP *
[Asserts]
# $statusCode == 201

# Step: pay-refused
# Pay with a card that triggers a bank refusal. The bank may
# also respond 5xx on transient errors, in that case we retry
# up to twice with 2 s back-off before accepting the failure.
POST {{baseUrl}}/orders/{{orderId}}/payment
Accept-Language: {{acceptLanguage}}
# requestBody content-type: application/json
```
{
  "amount": 49.99,
  "cardHolder": "Jane Doe",
  "cardNumber": "4111111111110002",
  "cvv": "456",
  "expiryDate": "06/25",
  "method": "card"
}
```

HTTP *
[Asserts]
# $statusCode == 200
# $response.body#/status == "REFUSED"
