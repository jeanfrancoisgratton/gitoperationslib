package gitoperationslib

import (
	"context"
)

// Site gives access to a Forgejo instance's system and default webhooks,
// which need a site-admin token (HTTP 403 otherwise). Only the Forgejo backend
// implements it (CapSystemWebhooks).
//
// Forgejo's API addresses both kinds by ID at the same path, and nothing in its
// answers tells them apart. Both interfaces therefore check the kind by listing
// the system webhooks first, and answer an ID of the other kind with a 404
// *APIError, changing nothing.
type Site interface {
	SystemWebhooks() SystemWebhooks
	DefaultWebhooks() DefaultWebhooks
}

// SystemWebhooks manages system webhooks, which fire for events in every
// repository of the instance.
type SystemWebhooks interface {
	List(ctx context.Context, opts ListOptions) (Page[Webhook], error)
	Get(ctx context.Context, id int64) (*Webhook, error)
	// Create adds a system webhook, through a flag Forgejo's API does not
	// document. It checks the result: if the server made a default webhook
	// instead, Create deletes it and returns an error. Forgejo silently drops
	// event names it does not know; Create then returns the webhook together
	// with an error.
	Create(ctx context.Context, opts CreateWebhookOptions) (*Webhook, error)
	// Edit changes a webhook; dropped events are reported as in
	// Webhooks.Create.
	Edit(ctx context.Context, id int64, opts EditWebhookOptions) (*Webhook, error)
	Delete(ctx context.Context, id int64) error
}

// DefaultWebhooks manages default webhooks: templates copied into each
// repository created afterwards. Existing repositories are not affected, and
// the copies are independent repository webhooks: editing or deleting the
// default webhook leaves them as they are. There is no List: Forgejo's API
// cannot list them, so keep the ID that Create returns.
type DefaultWebhooks interface {
	// Create adds a default webhook. Forgejo silently drops event names it does
	// not know; Create then returns the webhook together with an error.
	Create(ctx context.Context, opts CreateWebhookOptions) (*Webhook, error)
	Get(ctx context.Context, id int64) (*Webhook, error)
	// Edit changes a webhook; dropped events are reported as in Create.
	Edit(ctx context.Context, id int64, opts EditWebhookOptions) (*Webhook, error)
	Delete(ctx context.Context, id int64) error
}
