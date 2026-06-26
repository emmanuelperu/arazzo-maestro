# Workflow: authenticated-order-lookup
# Log in, list orders, read the first one
#
# Base URL (required): pass via `hurl --variable baseUrl=<endpoint>`
#   default (OpenAPI servers): https://orders.example.com/api/v1
#
# Inputs (pass via `hurl --variable name=value`):
#   - username (string)
#   - password (string)

# Step: login
# Exchange the credentials for a bearer token.
POST {{baseUrl}}/auth/login
Content-Type: application/json
# requestBody content-type: application/json
```
{
  "password": "{{password}}",
  "username": "{{username}}"
}
```

HTTP *
[Asserts]
# $statusCode == 200
[Captures]
login_token: jsonpath "$.token"

# Step: list-orders
# Authenticated call, the token comes from the login step.
GET {{baseUrl}}/orders
Authorization: Bearer {{login_token}}
[QueryStringParams]
page: 0

HTTP *
[Asserts]
# $statusCode == 200
[Captures]
list-orders_firstOrderId: jsonpath "$.items[0].id"

# Step: get-order
# The order id is self-provisioned from the listing step.
GET {{baseUrl}}/orders/{{list-orders_firstOrderId}}
Authorization: Bearer {{login_token}}

HTTP *
[Asserts]
# $statusCode == 200
[Captures]
get-order_orderStatus: jsonpath "$.status"
