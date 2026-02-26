package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

func TestNoArgs(t *testing.T) {
	var buf bytes.Buffer
	code := run([]string{}, &buf)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
}

func TestWrongSubcommand(t *testing.T) {
	var buf bytes.Buffer
	code := run([]string{"wrong"}, &buf)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
}

func TestHappyFoo(t *testing.T) {
	var buf bytes.Buffer
	code := run([]string{"foo", "--enable", "--name", "test"}, &buf) // "test" is passed
	if code != 0 {
		t.Fatalf("expected exit 0 so success, got %d", code)
	}
}

// use the table-driven test
func TestHappyFooAndBar(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"foo", []string{"foo", "--enable", "--name", "test"}},
		{"bar", []string{"bar", "--level", "5", "extraThings"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			code := run(tt.args, &buf)
			if code != 0 {
				t.Fatalf("expected exit 0 so success, got %d", code)
			}
		})
	}
}

func TestViewWithID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok from " + r.URL.Path))
	}))
	// goleak catches if this is commented out!!
	defer ts.Close()
	fmt.Print(ts)
	var buf bytes.Buffer
	code := run([]string{"view", "--host", ts.URL, "--id", "1"}, &buf)
	fmt.Print(code)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(buf.String(), "snippet/view/1") {
		t.Fatalf("unexpected body: %s", buf.String())
	}
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
