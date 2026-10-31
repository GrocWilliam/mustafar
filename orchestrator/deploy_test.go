package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestStripTopDir(t *testing.T) {
	var archive bytes.Buffer
	zipped := gzip.NewWriter(&archive)
	in := tar.NewWriter(zipped)
	in.WriteHeader(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "sha"}})
	in.WriteHeader(&tar.Header{Name: "sandbox/", Typeflag: tar.TypeDir, Mode: 0o755})
	in.WriteHeader(&tar.Header{Name: "sandbox/Dockerfile", Mode: 0o644, Size: 5})
	in.Write([]byte("FROM "))
	in.WriteHeader(&tar.Header{Name: "sandbox/app/main.py", Mode: 0o644, Size: 2})
	in.Write([]byte("hi"))
	in.Close()
	zipped.Close()

	var out bytes.Buffer
	if err := stripTopDir(&archive, &out); err != nil {
		t.Fatal(err)
	}
	var names []string
	contents := map[string]string{}
	for r := tar.NewReader(&out); ; {
		header, err := r.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(r)
		names, contents[header.Name] = append(names, header.Name), string(data)
	}
	if want := []string{"Dockerfile", "app/main.py"}; !reflect.DeepEqual(names, want) || contents["app/main.py"] != "hi" {
		t.Fatalf("names = %q, contents = %q", names, contents)
	}
}

func TestWaitHealthy(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls++; calls < 2 {
			w.WriteHeader(http.StatusServiceUnavailable) // still starting
		}
	}))
	defer server.Close()
	if err := waitHealthy(server.URL, 10*time.Second); err != nil {
		t.Fatalf("healthy server: %v", err)
	}
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer broken.Close()
	if err := waitHealthy(broken.URL, 1500*time.Millisecond); err == nil {
		t.Fatal("broken server reported healthy")
	}
}
