package main

import (
	"fmt"
	"io"
	"net/http"
)

func makeRequest(client *http.Client, method, url, token string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return client.Do(req)
}

// verbose helper extracted from main.go
func printResponse(resp *http.Response, verbose bool, stdout io.Writer) {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if verbose {
		fmt.Fprintln(stdout, "Status:", resp.Status)
		for k, v := range resp.Header {
			fmt.Fprintf(stdout, "%s: %s\n", k, v)
		}
		fmt.Fprintln(stdout, "---")
	}
	fmt.Fprintln(stdout, string(body))
}
