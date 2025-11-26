package util

import (
	"crypto/tls"
	"net/http"
	"sync"
	"time"
)

var (
	client *http.Client
	once   sync.Once
)

func GetHttpClient() *http.Client {
	once.Do(func() {
		client = buildHttpClient()
	})
	return client
}

func buildHttpClient() *http.Client {

	transport := &http.Transport{
		MaxIdleConnsPerHost: 30, // Maximum idle connections per host for connection reuse to reduce overhead
		MaxConnsPerHost:     50, // Maximum total connections (active + idle) per host, controls concurrency, excess will queue
		MaxIdleConns:        50, // Global maximum idle connections

		// TLS configuration for Doris HTTP endpoints
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // Allow insecure connections for Doris HTTP endpoints
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   120 * time.Second, // Total request timeout
	}

	return client
}
