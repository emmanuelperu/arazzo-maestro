// hurl-demo orchestrates a self-contained Hurl run against the example
// Arazzo workflows, producing a hurl HTML report under dist/hurl-report/.
//
// Pipeline:
//
//  1. start a local HTTP mock server bound to a random free port
//  2. copy the OpenAPI + Arazzo passed on the command line to a tmp dir,
//     rewriting the OpenAPI 'servers:' URL so generated requests hit
//     the mock
//  3. invoke 'arazzo-maestro test gen e2e' on the tmp Arazzo
//  4. run 'hurl --test --report-html dist/hurl-report' on the produced
//     .hurl files, with the workflow inputs passed as --variable
//
// Run via: 'make hurl-report' (which builds bin/arazzo-maestro first).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	reportDir         = "dist/hurl-report"
	originalServerURL = "https://shop.example.com/api/v1"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: hurl-demo <openapi.yaml> <arazzo.yaml>")
		os.Exit(2)
	}
	if _, err := exec.LookPath("hurl"); err != nil {
		exit("hurl binary not in PATH; install with: brew install hurl")
	}

	openapiSrc, arazzoSrc := os.Args[1], os.Args[2]

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		exit(err)
	}
	mockURL := "http://" + ln.Addr().String()
	fmt.Printf("→ mock listening on %s\n", mockURL)

	srv := &http.Server{Handler: http.HandlerFunc(handleMock)}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	dir, err := os.MkdirTemp("", "hurl-demo-*")
	if err != nil {
		exit(err)
	}
	defer os.RemoveAll(dir)

	openapi, err := os.ReadFile(openapiSrc)
	if err != nil {
		exit(err)
	}
	openapi = []byte(strings.ReplaceAll(string(openapi), originalServerURL, mockURL))
	if err := os.WriteFile(filepath.Join(dir, filepath.Base(openapiSrc)), openapi, 0o644); err != nil {
		exit(err)
	}
	arazzo, err := os.ReadFile(arazzoSrc)
	if err != nil {
		exit(err)
	}
	arazzoPath := filepath.Join(dir, filepath.Base(arazzoSrc))
	if err := os.WriteFile(arazzoPath, arazzo, 0o644); err != nil {
		exit(err)
	}

	genOut := filepath.Join(dir, "gen")
	mustRun("bin/arazzo-maestro", "test", "gen", "e2e", arazzoPath, "-o", genOut)

	var hurls []string
	_ = filepath.Walk(filepath.Join(genOut, "e2e", "hurl"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".hurl") {
			hurls = append(hurls, p)
		}
		return nil
	})
	if len(hurls) == 0 {
		exit("no .hurl files generated")
	}

	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		exit(err)
	}

	hurlArgs := []string{
		"--test",
		"--report-html", reportDir,
		"--variable", "productId=p-001",
		"--variable", "orderId=ord-1",
		"--variable", "acceptLanguage=en",
	}
	hurlArgs = append(hurlArgs, hurls...)
	hurlExit := runForExit("hurl", hurlArgs...)

	fmt.Printf("\n→ open %s/index.html\n", reportDir)
	if hurlExit != 0 {
		// Bubble hurl's status: the report is still produced and worth
		// opening, but CI should see the failure.
		os.Exit(hurlExit)
	}
}

// handleMock returns canned JSON responses keyed on the request path so
// the generated workflow can chain calls (capture firstProductId from
// /products, look it up via /products/{id}, ...). All endpoints return
// 200 with the minimal field set the shop workflow captures.
func handleMock(w http.ResponseWriter, r *http.Request) {
	write := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	p := r.URL.Path
	switch {
	case p == "/products":
		write(map[string]any{
			"items": []map[string]string{{"id": "p-001", "name": "Demo Widget"}},
		})
	case strings.HasPrefix(p, "/products/"):
		id := strings.TrimPrefix(p, "/products/")
		write(map[string]any{"id": id, "name": "Demo Widget", "price": 49.99})
	case p == "/cart/items":
		write(map[string]any{"totalPrice": 49.99})
	case strings.HasPrefix(p, "/orders/") && strings.HasSuffix(p, "/payment"):
		write(map[string]any{"transactionId": "tx-001", "status": "OK"})
	default:
		fmt.Fprintf(os.Stderr, "mock: unexpected request %s %s\n", r.Method, p)
		http.NotFound(w, r)
	}
}

func mustRun(name string, args ...string) {
	if code := runForExit(name, args...); code != 0 {
		exit(fmt.Sprintf("%s exited with %d", name, code))
	}
}

func runForExit(name string, args ...string) int {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
	return cmd.ProcessState.ExitCode()
}

func exit(v any) {
	fmt.Fprintln(os.Stderr, "error:", v)
	os.Exit(1)
}
