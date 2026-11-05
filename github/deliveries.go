package github

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

var _ gitoperationslib.HookDeliveries = (*Client)(nil)

func toHookDelivery(d *gh.HookDelivery) gitoperationslib.HookDelivery {
	out := gitoperationslib.HookDelivery{
		ID: d.GetID(), GUID: d.GetGUID(), Event: d.GetEvent(), Action: d.GetAction(), Status: d.GetStatus(), StatusCode: d.GetStatusCode(),
		Redelivery: d.GetRedelivery(), Duration: time.Duration(d.GetDuration() * float64(time.Second)), DeliveredAt: d.GetDeliveredAt().Time,
	}
	if d.Request != nil {
		out.RequestHeaders = d.Request.Headers
		if d.Request.RawPayload != nil {
			out.RequestPayload = *d.Request.RawPayload
		}
	}
	if d.Response != nil {
		out.ResponseHeaders = d.Response.Headers
		if d.Response.RawPayload != nil {
			// A JSON string holding the target's answer, or null.
			var body string
			if json.Unmarshal(*d.Response.RawPayload, &body) == nil {
				out.ResponseBody = body
			}
		}
	}
	return out
}

// ListHookDeliveries implements gitoperationslib.HookDeliveries.
func (c *Client) ListHookDeliveries(ctx context.Context, scope gitoperationslib.HookScope, hookID int64, opts gitoperationslib.ListHookDeliveriesOptions) (gitoperationslib.HookDeliveryPage, error) {
	if err := checkScope(scope); err != nil {
		return gitoperationslib.HookDeliveryPage{}, err
	}
	lo := &gh.ListCursorOptions{Cursor: opts.Cursor, PerPage: opts.PerPage}
	var (
		list []*gh.HookDelivery
		resp *gh.Response
		err  error
	)
	if scope.Repo == "" {
		list, resp, err = c.gh.Organizations.ListHookDeliveries(ctx, scope.Owner, hookID, lo)
	} else {
		list, resp, err = c.gh.Repositories.ListHookDeliveries(ctx, scope.Owner, scope.Repo, hookID, lo)
	}
	if err != nil {
		return gitoperationslib.HookDeliveryPage{}, convertError(err)
	}
	page := gitoperationslib.HookDeliveryPage{Items: make([]gitoperationslib.HookDelivery, len(list)), NextCursor: resp.Cursor}
	for i, d := range list {
		page.Items[i] = toHookDelivery(d)
	}
	return page, nil
}

// GetHookDelivery implements gitoperationslib.HookDeliveries.
func (c *Client) GetHookDelivery(ctx context.Context, scope gitoperationslib.HookScope, hookID, id int64) (*gitoperationslib.HookDelivery, error) {
	if err := checkScope(scope); err != nil {
		return nil, err
	}
	var (
		d   *gh.HookDelivery
		err error
	)
	if scope.Repo == "" {
		d, _, err = c.gh.Organizations.GetHookDelivery(ctx, scope.Owner, hookID, id)
	} else {
		d, _, err = c.gh.Repositories.GetHookDelivery(ctx, scope.Owner, scope.Repo, hookID, id)
	}
	if err != nil {
		return nil, convertError(err)
	}
	out := toHookDelivery(d)
	return &out, nil
}

// RedeliverHook implements gitoperationslib.HookDeliveries. GitHub answers HTTP
// 202, which go-github reports as an error.
func (c *Client) RedeliverHook(ctx context.Context, scope gitoperationslib.HookScope, hookID, id int64) error {
	if err := checkScope(scope); err != nil {
		return err
	}
	var err error
	if scope.Repo == "" {
		_, _, err = c.gh.Organizations.RedeliverHookDelivery(ctx, scope.Owner, hookID, id)
	} else {
		_, _, err = c.gh.Repositories.RedeliverHookDelivery(ctx, scope.Owner, scope.Repo, hookID, id)
	}
	var accepted *gh.AcceptedError
	if errors.As(err, &accepted) {
		return nil
	}
	return convertError(err)
}
