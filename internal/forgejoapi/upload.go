package forgejoapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

// FormFile is a file part of a multipart upload.
type FormFile struct {
	Field       string // form field name
	Filename    string
	ContentType string
	Content     io.Reader
	Size        int64 // exact number of bytes in Content
}

var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// Upload POSTs a multipart/form-data request to path with the given plain
// fields and optional file, decoding the JSON response into out. The file is
// streamed, and the request carries an exact Content-Length (some reverse
// proxies reject chunked uploads).
func (c *Client) Upload(ctx context.Context, path string, query url.Values, fields map[string]string, file *FormFile, out any) (*Response, error) {
	// Everything except the file content is small: render it up front so the
	// total length is known.
	var head bytes.Buffer
	mw := multipart.NewWriter(&head)
	for name, value := range fields {
		if err := mw.WriteField(name, value); err != nil {
			return nil, fmt.Errorf("forgejo: build upload: %w", err)
		}
	}
	var content io.Reader = http.NoBody
	var size int64
	if file != nil {
		if file.Size < 0 {
			return nil, fmt.Errorf("forgejo: upload %q: size must be known", file.Filename)
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
			quoteEscaper.Replace(file.Field), quoteEscaper.Replace(file.Filename)))
		h.Set("Content-Type", file.ContentType)
		if _, err := mw.CreatePart(h); err != nil {
			return nil, fmt.Errorf("forgejo: build upload: %w", err)
		}
		content, size = io.LimitReader(file.Content, file.Size), file.Size
	}
	headBytes := bytes.Clone(head.Bytes())
	head.Reset()
	if err := mw.Close(); err != nil { // writes the closing boundary into head
		return nil, fmt.Errorf("forgejo: build upload: %w", err)
	}
	tail := head.Bytes()

	body := io.MultiReader(bytes.NewReader(headBytes), content, bytes.NewReader(tail))
	req, err := c.newRequest(ctx, http.MethodPost, c.apiURL(path, query), body, mw.FormDataContentType())
	if err != nil {
		return nil, err
	}
	req.ContentLength = int64(len(headBytes)) + size + int64(len(tail))
	return c.doJSON(req, out)
}

// Download GETs rawURL, an absolute URL on the Forgejo instance (such as an
// attachment's browser_download_url), and returns the response body.
// Credentials are only sent to the instance's own host; other URLs are
// refused.
func (c *Client) Download(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("forgejo: download %q: %w", rawURL, err)
	}
	if !strings.EqualFold(u.Scheme, c.baseURL.Scheme) || !strings.EqualFold(u.Host, c.baseURL.Host) {
		return nil, fmt.Errorf("forgejo: download %q: not on %s; refusing to send credentials", rawURL, c.baseURL.Host)
	}
	req, err := c.newRequest(ctx, http.MethodGet, u.String(), nil, "")
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
