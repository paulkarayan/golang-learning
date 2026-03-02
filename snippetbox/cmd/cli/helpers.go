package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func makeRequest(client *http.Client, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return client.Do(req)
}

// verbose helper extracted from main.go
func printResponse(resp *http.Response, verbose bool, stdout io.Writer) {
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if verbose {
		fmt.Fprintln(stdout, "Status:", resp.Status) //nolint:errcheck
		for k, v := range resp.Header {
			fmt.Fprintf(stdout, "%s: %s\n", k, v) //nolint:errcheck
		}
		fmt.Fprintln(stdout, "---") //nolint:errcheck
	}
	fmt.Fprintln(stdout, string(body)) //nolint:errcheck
}

// we are going to look up the correct cert and key for the role
// cuz we're so nice
func clientForRole(role, caPath, certDir string) (*http.Client, error) {
	cert, err := tls.LoadX509KeyPair(
		fmt.Sprintf("%s/client-%s-cert.pem", certDir, role),
		fmt.Sprintf("%s/client-%s-key.pem", certDir, role),
	)
	if err != nil {
		return nil, err
	}
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      caCertPool,
				Certificates: []tls.Certificate{cert},
			},
		},
	}, nil
}

// same thing but for grpc
func grpcConnForRole(role, caPath, certDir, grpcHost string) (*grpc.ClientConn, error) {
	cert, err := tls.LoadX509KeyPair(
		fmt.Sprintf("%s/client-%s-cert.pem", certDir, role),
		fmt.Sprintf("%s/client-%s-key.pem", certDir, role),
	)
	if err != nil {
		return nil, err
	}
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		ServerName:   "localhost",
	}
	conn, err := grpc.NewClient(
		grpcHost,
		grpc.WithTransportCredentials(
			credentials.NewTLS(tlsCfg),
		),
	)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
