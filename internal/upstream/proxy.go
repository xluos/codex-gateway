package upstream

import (
	"io"
	"net/http"
)

type Result struct {
	UpstreamURL      string
	StatusCode       int
	RequestID        string
	ErrorBodySnippet string
}

func Proxy(w http.ResponseWriter, r *http.Request, client *Client, path string) (*Result, error) {
	req, err := client.NewRequest(r.Context(), r.Method, path, r.Body, r.Header)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return WriteResponse(w, resp, req.URL.Path)
}

func copyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func WriteResponse(w http.ResponseWriter, resp *http.Response, upstreamPath string) (*Result, error) {
	result := &Result{
		UpstreamURL: upstreamPath,
		StatusCode:  resp.StatusCode,
		RequestID:   resp.Header.Get("X-Request-ID"),
	}

	if resp.StatusCode >= http.StatusBadRequest {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		result.ErrorBodySnippet = truncateForLog(body)
		copyResponseHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, err = w.Write(body)
		return result, err
	}

	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		return nil, err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return result, nil
}

func truncateForLog(body []byte) string {
	const maxBytes = 2048
	if len(body) <= maxBytes {
		return string(body)
	}
	return string(body[:maxBytes]) + "...(truncated)"
}
