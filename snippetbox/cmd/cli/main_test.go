package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

func TestNoArgs(t *testing.T) {
	var buf bytes.Buffer
	code := run([]string{}, &buf, nil)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
}

func TestWrongSubcommand(t *testing.T) {
	var buf bytes.Buffer
	code := run([]string{"wrong"}, &buf, nil)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
}

func TestHappyFoo(t *testing.T) {
	var buf bytes.Buffer
	code := run([]string{"foo", "--enable", "--name", "test"}, &buf, nil) // "test" is passed
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
			// we dont make http calls so just doing nil to appease run()
			code := run(tt.args, &buf, nil)
			if code != 0 {
				t.Fatalf("expected exit 0 so success, got %d", code)
			}
		})
	}
}

func TestViewWithID(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok from " + r.URL.Path))
	}))
	// goleak catches if this is commented out!!
	defer ts.Close()
	// fmt.Print(ts)
	var buf bytes.Buffer
	code := run([]string{"view", "--host", ts.URL, "--id", "1"}, &buf, ts.Client())
	// fmt.Print(code)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(buf.String(), "snippet/view/1") {
		t.Fatalf("unexpected body: %s", buf.String())
	}
}

func TestCreateSnippet(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// verify it's a POST with JSON
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		var input struct {
			Title   string `json:"title"`
			Content string `json:"content"`
			Expires int    `json:"expires"`
		}
		err := json.NewDecoder(r.Body).Decode(&input)
		if err != nil {
			w.WriteHeader(400)
			w.Write([]byte("bad json"))
			return
		}
		w.WriteHeader(201)
		w.Write([]byte("created: " + input.Title))
	}))
	defer ts.Close()

	var buf bytes.Buffer
	code := run([]string{"create", "--host", ts.URL, "--title", "Wasabi", "--content",
		"w", "--expires", "7"}, &buf, ts.Client())
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(buf.String(), "created: Wasabi") {
		t.Fatalf("unexpected body: %s", buf.String())
	}
}

// this should fail for this commit until i fix other stuff...
func TestViewTLS(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok from " + r.URL.Path))
	}))
	defer ts.Close()

	var buf bytes.Buffer
	code := run([]string{"view", "--host", ts.URL, "--id", "1"}, &buf, ts.Client())
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; output: %s", code, buf.String())
	}
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
