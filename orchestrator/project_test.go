package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { time.Sleep(200 * time.Millisecond) }))
	defer slow.Close()
	a := api{name: "gitea", base: slow.URL, http: &http.Client{Timeout: 20 * time.Millisecond}}
	err := a.call("GET", "/", nil, nil)
	if !isTimeout(err) || !isTimeout(fmt.Errorf("opening the pull request: %w", err)) {
		t.Fatalf("a client timeout, bare or wrapped, is a timeout: %v", err)
	}
	if isTimeout(&statusError{500, "gitea GET /: 500"}) || isTimeout(fmt.Errorf("CI failed")) {
		t.Fatal("an answer or another error is not a timeout")
	}
}

func TestRequeueStopsAfterMax(t *testing.T) {
	var o project
	for i := 1; i <= maxRequeues; i++ {
		if !o.requeue(7) {
			t.Fatalf("timeout %d should requeue", i)
		}
	}
	if o.requeue(7) {
		t.Fatal("one timeout too many should block")
	}
	if !o.requeue(8) {
		t.Fatal("counts are per US")
	}
}
