package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"io"
	"net/http"
)

func httpRequest(ctx context.Context, method string, url string, payload interface{}) (*http.Response, error) {
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	var b *bytes.Buffer
	if payload != nil {
		var bp []byte
		switch p := payload.(type) {
		case string:
			bp = []byte(p)
		case []byte:
			bp = p
		default:
			o, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			bp = o
		}

		b = bytes.NewBuffer(bp)
	}

	var err error
	var req *http.Request
	if b == nil {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, err
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, b)
		if err != nil {
			return nil, err
		}
	}

	req.Header.Set("Content-Type", "application/json")

	response, err := client.Do(req)
	if err != nil {

		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	rsp := &HttpResponse{
		Status:  response.StatusCode,
		Headers: response.Header,
		Body:    body,
	}
	return rsp, nil
}
