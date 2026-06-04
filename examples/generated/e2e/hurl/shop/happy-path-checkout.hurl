# Workflow: happy-path-checkout
# Browse, add to cart and pay successfully
#
# Base URL (required): pass via `hurl --variable baseUrl=<endpoint>`
#   default (OpenAPI servers): https://shop.example.com/api/v1
#
# Inputs (pass via `hurl --variable name=value`):
#   - productId (string)
#   - orderId (string)
#   - acceptLanguage (string)

# Step: list-catalog
# Browse the product catalog
GET {{baseUrl}}/products
Accept-Language: {{acceptLanguage}}
[QueryStringParams]
page: 0
size: 10

HTTP *
[Asserts]
# $statusCode == 200
[Captures]
list-catalog_firstProductId: jsonpath "$.items[0].id"

# Step: get-product
# Fetch the selected product's full details
GET {{baseUrl}}/products/{{list-catalog_firstProductId}}
Accept-Language: {{acceptLanguage}}

HTTP *
[Asserts]
# $statusCode == 200
[Captures]
get-product_productName: jsonpath "$.name"

# Step: add-to-cart
# Add the product to the shopping cart
POST {{baseUrl}}/cart/items
Accept-Language: {{acceptLanguage}}
# requestBody content-type: application/json
```
{
  "productId": "{{productId}}",
  "quantity": 2
}
```

HTTP *
[Asserts]
# $statusCode == 201
# $response.body#/totalPrice > 0
[Captures]
add-to-cart_cartTotal: jsonpath "$.totalPrice"

# Step: pay
# Pay the order with a valid card
POST {{baseUrl}}/orders/{{orderId}}/payment
Accept-Language: {{acceptLanguage}}
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
pay_transactionId: jsonpath "$.transactionId"
