package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "snippetbox.paulkarayan.com/cmd/proto"
)

// minimal gRPC server for CLI integration tests
type testGRPCServer struct {
	pb.UnimplementedSnippetBoxServer
}

func (s *testGRPCServer) Home(_ context.Context, _ *pb.HomeRequest) (*pb.HomeResponse, error) {
	return &pb.HomeResponse{Message: "Hello from Snippetbox"}, nil
}

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

// func TestViewTLS(t *testing.T) {
// 	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		auth := r.Header.Get("Authorization")
// 		if auth != "Bearer test-token" {
// 			http.Error(w, "unauthorized", 401)
// 			return
// 		}
// 		w.Write([]byte("ok from " + r.URL.Path))
// 	}))
// 	defer ts.Close()
//
// 	var buf bytes.Buffer
// 	code := run([]string{"view", "--host", ts.URL, "--id", "1", "--token", "test-token"}, &buf, ts.Client())
// 	if code != 0 {
// 		t.Fatalf("expected exit 0, got %d; output: %s", code, buf.String())
// 	}
// }

func TestHomeTLS(t *testing.T) {
	// generate CA
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caCertDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	caCert, _ := x509.ParseCertificate(caCertDER)
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	// generate server cert
	srvKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srvTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	srvCertDER, _ := x509.CreateCertificate(rand.Reader, srvTmpl, caCert, &srvKey.PublicKey, caKey)
	srvCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvCertDER})
	srvKeyDER, _ := x509.MarshalECPrivateKey(srvKey)
	srvKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: srvKeyDER})
	srvTLS, _ := tls.X509KeyPair(srvCertPEM, srvKeyPEM)

	// generate client-user cert
	cliKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cliTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "user"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	cliCertDER, _ := x509.CreateCertificate(rand.Reader, cliTmpl, caCert, &cliKey.PublicKey, caKey)
	cliCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cliCertDER})
	cliKeyDER, _ := x509.MarshalECPrivateKey(cliKey)
	cliKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: cliKeyDER})

	// start gRPC server with mTLS
	serverTLSCfg := &tls.Config{
		Certificates: []tls.Certificate{srvTLS},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLSCfg)))
	pb.RegisterSnippetBoxServer(s, &testGRPCServer{})
	go func() { s.Serve(lis) }()
	t.Cleanup(func() { s.Stop() })

	// write cert files to temp dir matching the layout grpcConnForRole expects
	tmpDir := t.TempDir()
	tlsDir := filepath.Join(tmpDir, "cmd", "tls")
	os.MkdirAll(tlsDir, 0755)
	os.WriteFile(filepath.Join(tlsDir, "ca-cert.pem"), caCertPEM, 0644)
	os.WriteFile(filepath.Join(tlsDir, "client-user-cert.pem"), cliCertPEM, 0644)
	os.WriteFile(filepath.Join(tlsDir, "client-user-key.pem"), cliKeyPEM, 0644)

	// chdir so ./cmd/tls/ resolves
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	var buf bytes.Buffer
	code := run([]string{"home", "--grpc-host", lis.Addr().String()}, &buf, nil)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; output: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "Hello from Snippetbox") {
		t.Fatalf("unexpected body: %s", buf.String())
	}
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("google.golang.org/grpc/internal/transport.(*controlBuffer).get"),
		goleak.IgnoreTopFunction("google.golang.org/grpc/internal/grpcsync.(*CallbackSerializer).run"),
	)
}
