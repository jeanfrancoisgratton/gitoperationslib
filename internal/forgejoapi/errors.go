package forgejoapi

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/retry"
)

// maxErrorBody caps how much of an error response is read.
const maxErrorBody = 64 << 10

// decodeError turns a non-2xx response into an *APIError. Bodies that are not
// Forgejo's JSON error format (e.g. a reverse proxy's HTML page) leave Message
// empty, so the error falls back to the HTTP status text.
func decodeError(resp *http.Response) error {
	apiErr := &gitoperationslib.APIError{
		Backend:    gitoperationslib.BackendForgejo,
		StatusCode: resp.StatusCode,
	}
	apiErr.RetryAt, _ = retry.RetryAt(resp.Header, time.Now())
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

	var body struct {
		Message string            `json:"message"`
		URL     string            `json:"url"`
		Errors  []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(data, &body) != nil {
		return apiErr
	}
	apiErr.Message = body.Message
	apiErr.DocumentationURL = body.URL
	for _, raw := range body.Errors {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			apiErr.Details = append(apiErr.Details, s)
		} else {
			apiErr.Details = append(apiErr.Details, string(raw))
		}
	}
	return apiErr
}
