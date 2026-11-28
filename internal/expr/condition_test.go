package expr

import "testing"

func TestParseCondition(t *testing.T) {
	tests := []struct {
		in   string
		want Condition
		ok   bool
	}{
		{"$statusCode == 200", Condition{Subject: SubjectStatus, Op: "==", Kind: LiteralNumber, Value: "200"}, true},
		{"  $statusCode>=500 ", Condition{Subject: SubjectStatus, Op: ">=", Kind: LiteralNumber, Value: "500"}, true},
		{"$response.body#/totalPrice > 0", Condition{Subject: SubjectBody, Pointer: "totalPrice", Op: ">", Kind: LiteralNumber, Value: "0"}, true},
		{"$response.body#/price <= -1.5e2", Condition{Subject: SubjectBody, Pointer: "price", Op: "<=", Kind: LiteralNumber, Value: "-1.5e2"}, true},
		{"$response.body#/status == 'OK'", Condition{Subject: SubjectBody, Pointer: "status", Op: "==", Kind: LiteralString, Value: "OK"}, true},
		{`$response.body#/status != "REFUSED"`, Condition{Subject: SubjectBody, Pointer: "status", Op: "!=", Kind: LiteralString, Value: "REFUSED"}, true},
		{"$response.body#/note == 'it''s done'", Condition{Subject: SubjectBody, Pointer: "note", Op: "==", Kind: LiteralString, Value: "it's done"}, true},
		{"$response.body#/a == ''", Condition{Subject: SubjectBody, Pointer: "a", Op: "==", Kind: LiteralString, Value: ""}, true},
		{"$response.body#/paid == true", Condition{Subject: SubjectBody, Pointer: "paid", Op: "==", Kind: LiteralBool, Value: "true"}, true},
		{"$response.body != null", Condition{Subject: SubjectBody, Op: "!=", Kind: LiteralNull, Value: "null"}, true},
		{"$response.header.Content-Type == 'application/json'", Condition{Subject: SubjectHeader, HeaderName: "Content-Type", Op: "==", Kind: LiteralString, Value: "application/json"}, true},

		{"$statusCode == 200 && $response.body != null", Condition{}, false},
		{"$response.body#/a == 'x' || $statusCode == 1", Condition{}, false},
		{"$response.body#/a == 'x' && $response.body#/b == 'y'", Condition{}, false},
		{"!($statusCode == 200)", Condition{}, false},
		{"200 == $statusCode", Condition{}, false},
		{"$statusCode == 'OK'", Condition{}, false},
		{"$response.body#/id == $inputs.id", Condition{}, false},
		{`$response.body#/a == "x"y"`, Condition{}, false},
		{"$response.body#/a == OK", Condition{}, false},
		{"$response.body#/a == 01", Condition{}, false},
		{"$method == 'GET'", Condition{}, false},
		{"$response.header. == 'x'", Condition{}, false},
		{"$statusCode", Condition{}, false},
		{"", Condition{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := ParseCondition(tt.in)
			if ok != tt.ok || got != tt.want {
				t.Errorf("ParseCondition(%q) = %+v, %v; want %+v, %v", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}
